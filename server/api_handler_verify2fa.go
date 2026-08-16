package chserver

import (
	"net/http"

	errors2 "github.com/proximile/proxiport/server/api/errors"
	"github.com/proximile/proxiport/server/bearer"
)

func (al *APIListener) handlePostVerify2FAToken() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		username, err := al.parseAndValidate2FATokenRequest(req)
		if err != nil {
			if !al.handleBannedIPs(req, false) {
				return
			}
			al.Errorf(err.Error())
			al.jsonError(w, err)
			return
		}

		al.sendJWTToken(username, w, req)
	})
}

func (al *APIListener) parseAndValidate2FATokenRequest(req *http.Request) (username string, err error) {
	if !al.config.API.IsTwoFAOn() && !al.config.API.TotPEnabled {
		return "", errors2.APIError{
			HTTPStatus: http.StatusConflict,
			Message:    "2fa is disabled",
		}
	}

	var reqBody struct {
		Username string `json:"username"`
		Token    string `json:"token"`
	}
	err = parseRequestBody(req.Body, &reqBody)
	if err != nil {
		return "", err
	}

	if al.bannedUsers.IsBanned(reqBody.Username) {
		return reqBody.Username, errors2.APIError{
			HTTPStatus: http.StatusTooManyRequests,
			Err:        ErrTooManyRequests,
		}
	}

	if reqBody.Username == "" {
		return "", errors2.APIError{
			HTTPStatus: http.StatusUnauthorized,
			Message:    "username is required",
		}
	}

	if reqBody.Token == "" {
		return reqBody.Username, errors2.APIError{
			HTTPStatus: http.StatusUnauthorized,
			Message:    "token is required",
		}
	}

	// Both the TotP and the email/SMS flows carry the 2FA-pending bearer token
	// minted by the password step. Bind the session that is about to be issued to
	// the identity inside that token, never to the client-supplied username, so a
	// verified caller cannot mint a session for someone else.
	bearerToken, bearerAuthProvided := bearer.GetBearerToken(req)
	if !bearerAuthProvided {
		return reqBody.Username, errors2.APIError{
			HTTPStatus: http.StatusBadRequest,
			Message:    "token is required",
		}
	}

	isAuthorized, token, err := al.checkBearerToken(req.Context(), bearerToken, req.URL.Path, req.Method)
	if err != nil {
		return reqBody.Username, err
	}

	if !isAuthorized {
		return reqBody.Username, errors2.APIError{
			HTTPStatus: http.StatusForbidden,
			Message:    "access denied",
		}
	}

	username = token.AppClaims.Username
	if reqBody.Username != username {
		return username, errors2.APIError{
			HTTPStatus: http.StatusForbidden,
			Message:    "username does not match the pending login session",
		}
	}

	if al.config.API.TotPEnabled {
		user, err := al.userService.GetByUsername(username)
		if err != nil {
			return "", err
		}
		return username, al.twoFASrv.ValidateTotPCode(user, reqBody.Token)
	}

	return username, al.twoFASrv.ValidateToken(username, reqBody.Token)
}
