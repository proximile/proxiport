package chserver

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/proximile/proxiport/server/api"
	errors2 "github.com/proximile/proxiport/server/api/errors"
	"github.com/proximile/proxiport/server/api/users"
	"github.com/proximile/proxiport/server/bearer"
	chshare "github.com/proximile/proxiport/share"
	"github.com/proximile/proxiport/share/logger"
)

type twoFAResponse struct {
	SendTo         string `json:"send_to"`
	DeliveryMethod string `json:"delivery_method"`
	TotPKeyStatus  string `json:"totp_key_status"`
}

type loginResponse struct {
	Token *string        `json:"token"`  // null if 2fa is on
	TwoFA *twoFAResponse `json:"two_fa"` // null if 2fa is off
}

func (al *APIListener) handleGetLogin(w http.ResponseWriter, req *http.Request) {
	if al.config.API.AuthHeader != "" && req.Header.Get(al.config.API.AuthHeader) != "" {
		al.handleLogin(req.Header.Get(al.config.API.UserHeader), "", "", true /* skipPasswordValidation */, w, req)
		return
	}

	basicUser, basicPwd, basicAuthProvided := req.BasicAuth()
	if basicAuthProvided {
		al.handleLogin(basicUser, basicPwd, "", false, w, req)
		return
	}

	// TODO: lift this bad-request banning check out of every API endpoint and into middleware.
	// ban IP if it sends a lot of bad requests
	if !al.handleBannedIPs(req, false) {
		return
	}
	al.jsonErrorResponseWithTitle(w, http.StatusUnauthorized, "auth is required")
}

func (al *APIListener) handleLogin(username, pwd string, newpwd string, skipPasswordValidation bool, w http.ResponseWriter, req *http.Request) {
	// Throttle failed logins per (source IP, username) rather than by username
	// alone, so a third party cannot lock a victim's account out just by spraying
	// bad passwords at their username. Per-IP abuse is still caught separately by
	// the bannedIPs machinery via handleBannedIPs.
	banKey := loginBanKey(chshare.RemoteIP(req), username)
	if al.bannedUsers.IsBanned(banKey) {
		al.jsonErrorResponseWithTitle(w, http.StatusTooManyRequests, ErrTooManyRequests.Error())
		return
	}

	if username == "" {
		al.jsonErrorResponseWithTitle(w, http.StatusUnauthorized, "username is required")
		return
	}

	authorized, user, err := al.validateCredentials(username, pwd, skipPasswordValidation)
	if err != nil {
		al.jsonError(w, err)
		return
	}

	if !al.handleBannedIPs(req, authorized) {
		return
	}

	if !authorized {
		al.bannedUsers.Add(banKey)
		al.jsonErrorResponseWithTitle(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	// A successful password login proves the operator has retrieved the
	// installer's cleartext credential files, so remove them from disk. Skipped
	// for token/2FA-check flows (skipPasswordValidation), which do not evidence
	// a fresh password entry.
	if !skipPasswordValidation {
		al.shredConsumedInstallerCreds()
	}

	lifetime, err := parseTokenLifetime(req)
	if err != nil {
		al.jsonErrorResponse(w, http.StatusBadRequest, err)
		return
	}

	// A second factor is configured, so the password alone has not authenticated
	// anyone yet. Committing the new password here let someone holding only the
	// password rotate it — locking the real owner out — without ever completing
	// a login. Carry it on the pending 2FA session instead and apply it in
	// /verify-2fa. The expired-password flow still works: the change lands as
	// soon as the second factor is verified.
	deferPasswordChange := newpwd != "" && (al.config.API.IsTwoFAOn() || al.config.API.TotPEnabled)

	// Only set the new password after the old password has been verified.
	if newpwd != "" && !deferPasswordChange {
		if err := al.userService.Change(
			&users.User{
				Password:        newpwd,
				PasswordExpired: users.PasswordExpired(false)},
			username); err != nil {
			al.jsonError(w, err)
			return
		}
		user.PasswordExpired = users.PasswordExpired(false) // from here on

		// Rotating a password retires every other session; otherwise a stolen
		// bearer outlives the rotation meant to revoke it.
		if err := al.apiSessions.DeleteAllByUser(req.Context(), username); err != nil {
			al.jsonErrorResponse(w, http.StatusInternalServerError, err)
			return
		}
	}

	if !deferPasswordChange && user.PasswordExpired != nil && *user.PasswordExpired {
		al.jsonErrorResponseWithTitle(w, http.StatusUnauthorized, ErrThatPasswordHasExpired.Error())
		return
	}

	if al.config.API.IsTwoFAOn() {
		sendTo, err := al.twoFASrv.SendToken(req.Context(), username, req.UserAgent(), chshare.RemoteIP(req))
		if err != nil {
			al.jsonError(w, err)
			return
		}

		// 2fa token
		tokenStr, err := bearer.CreateAuthToken(
			req.Context(),
			al.apiSessions,
			al.config.API.JWTSecret,
			lifetime,
			username,
			bearer.Scopes2FaCheckOnly,
			req.UserAgent(),
			chshare.RemoteIP(req),
		)
		if err != nil {
			al.jsonErrorResponse(w, http.StatusInternalServerError, err)
			return
		}

		if deferPasswordChange {
			al.twoFASrv.SetPendingPasswordChange(username, newpwd)
		}

		al.writeJSONResponse(w, http.StatusOK, api.NewSuccessPayload(loginResponse{
			Token: &tokenStr,
			TwoFA: &twoFAResponse{
				SendTo:         sendTo,
				DeliveryMethod: al.twoFASrv.MsgSrv.DeliveryMethod(),
			},
		}))
		return
	}

	if al.config.API.TotPEnabled {
		al.twoFASrv.SetTotPLoginSession(username, al.config.API.TotPLoginSessionTimeout)
		if deferPasswordChange {
			al.twoFASrv.SetPendingPasswordChange(username, newpwd)
		}

		loginResp := loginResponse{
			TwoFA: &twoFAResponse{
				DeliveryMethod: "totp_authenticator_app",
			},
		}

		totP, err := GetUsersTotPCode(user)
		if err != nil {
			al.Logf(logger.LogLevelError, "failed to get TotP secret: %v", err)
			al.jsonErrorResponse(w, http.StatusInternalServerError, err)
			return
		}

		scopes := bearer.Scopes2FaCheckOnly
		if totP == nil {
			// we allow access to totp-secret creation only if no totp secret was created before
			scopes = append(scopes, bearer.ScopesTotPCreateOnly...)
			loginResp.TwoFA.TotPKeyStatus = TotPKeyPending.String()
		} else {
			loginResp.TwoFA.TotPKeyStatus = TotPKeyExists.String()
		}

		// TotP token
		tokenStr, err := bearer.CreateAuthToken(
			req.Context(),
			al.apiSessions,
			al.config.API.JWTSecret,
			lifetime,
			username,
			scopes,
			req.UserAgent(),
			chshare.RemoteIP(req),
		)
		if err != nil {
			al.jsonErrorResponse(w, http.StatusInternalServerError, err)
			return
		}

		loginResp.Token = &tokenStr
		al.writeJSONResponse(w, http.StatusOK, api.NewSuccessPayload(loginResp))
		return
	}

	// login token, normal. 2FA is off, so the password alone completes the
	// authentication: clear the per-IP failure counter here.
	al.recordAuthSuccess(req)

	tokenStr, err := bearer.CreateAuthToken(
		req.Context(),
		al.apiSessions,
		al.config.API.JWTSecret,
		lifetime,
		username,
		bearer.ScopesAllExcluding2FaCheck,
		req.UserAgent(),
		chshare.RemoteIP(req),
	)
	if err != nil {
		al.jsonErrorResponse(w, http.StatusInternalServerError, err)
		return
	}

	response := api.NewSuccessPayload(loginResponse{
		Token: &tokenStr,
	})
	al.writeJSONResponse(w, http.StatusOK, response)
}

func (al *APIListener) sendJWTToken(username string, w http.ResponseWriter, req *http.Request) {
	lifetime, err := parseTokenLifetime(req)
	if err != nil {
		al.jsonErrorResponse(w, http.StatusBadRequest, err)
		return
	}

	// login token, after 2fa. The second factor has been verified, so this is
	// the completed authentication: clear the per-IP failure counter. Doing this
	// from the auth middleware instead is what let a /verify-2fa brute force
	// reset the limiter on every guess.
	al.recordAuthSuccess(req)

	tokenStr, err := bearer.CreateAuthToken(
		req.Context(),
		al.apiSessions,
		al.config.API.JWTSecret,
		lifetime,
		username,
		bearer.ScopesAllExcluding2FaCheck,
		req.UserAgent(),
		chshare.RemoteIP(req),
	)
	if err != nil {
		al.jsonErrorResponse(w, http.StatusInternalServerError, err)
		return
	}

	response := api.NewSuccessPayload(loginResponse{
		Token: &tokenStr,
	})
	al.writeJSONResponse(w, http.StatusOK, response)
}

func (al *APIListener) handlePostLogin(w http.ResponseWriter, req *http.Request) {
	// updating the Password via newPassword field is allowed with a POST or PATCH request
	username, pwd, newPassword, err := parseLoginRequestBody(req)

	if err != nil {
		// ban IP if it sends a lot of bad requests
		if !al.handleBannedIPs(req, false) {
			return
		}
		al.jsonError(w, err)
		return
	}

	al.handleLogin(username, pwd, newPassword, false, w, req)
}

func parseLoginRequestBody(req *http.Request) (string, string, string, error) {
	reqContentType := req.Header.Get("Content-Type")
	if reqContentType == "application/x-www-form-urlencoded" {
		err := req.ParseForm()
		if err != nil {
			return "", "", "", errors2.APIError{
				Err:        fmt.Errorf("failed to parse form: %v", err),
				HTTPStatus: http.StatusBadRequest,
			}
		}
		return req.PostForm.Get("username"), req.PostForm.Get("password"), req.PostForm.Get("new_password"), nil
	}
	if reqContentType == "application/json" {
		type loginReq struct {
			Username    string `json:"username"`
			Password    string `json:"password"`
			NewPassword string `json:"new_password"`
		}
		var params loginReq
		err := parseRequestBody(req.Body, &params)
		if err != nil {
			return "", "", "", err
		}
		return params.Username, params.Password, params.NewPassword, nil
	}
	return "", "", "", errors2.APIError{
		Message:    fmt.Sprintf("unsupported content type: %s", reqContentType),
		HTTPStatus: http.StatusBadRequest,
	}
}

func parseTokenLifetime(req *http.Request) (time.Duration, error) {
	lifetimeStr := req.URL.Query().Get("token-lifetime")
	if lifetimeStr == "" {
		lifetimeStr = "0"
	}
	lifetime, err := strconv.ParseInt(lifetimeStr, 10, 0)
	if err != nil {
		return 0, fmt.Errorf("invalid token-lifetime : %s", err)
	}
	result := time.Duration(lifetime) * time.Second
	if result > bearer.DefaultMaxTokenLifetime {
		return 0, fmt.Errorf("requested token lifetime exceeds max allowed %d", bearer.DefaultMaxTokenLifetime/time.Second)
	}
	if result <= 0 {
		result = bearer.DefaultTokenLifetime
	}
	return result, nil
}
