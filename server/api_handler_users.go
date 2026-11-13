package chserver

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/mux"

	"github.com/proximile/proxiport/server/api"
	"github.com/proximile/proxiport/server/api/authorization"
	"github.com/proximile/proxiport/server/api/users"
	"github.com/proximile/proxiport/server/auditlog"
	"github.com/proximile/proxiport/server/routes"
)

var (
	ErrMissingUserIDParam    = errors.New("missing user id param")
	ErrMissingSessionIDParam = errors.New("missing session id param")
)

type UserPayload struct {
	Username                     string                       `json:"username"`
	PasswordExpired              bool                         `json:"password_expired"`
	Groups                       []string                     `json:"groups"`
	TwoFASendTo                  string                       `json:"two_fa_send_to"`
	EffectiveUserPermissions     map[string]bool              `json:"effective_user_permissions"`
	EffectiveExtendedPermissions EffectiveExtendedPermissions `json:"effective_extended_permissions"`
	GroupPermissionsEnabled      bool                         `json:"group_permissions_enabled"`
}

type EffectiveExtendedPermissions struct {
	TunnelsRestricted  []users.PermissionParams `json:"tunnels_restricted"`
	CommandsRestricted []users.PermissionParams `json:"commands_restricted"`
}

func (al *APIListener) handleGetUsers(w http.ResponseWriter, req *http.Request) {
	usrs, err := al.userService.GetAll()
	if err != nil {
		al.jsonError(w, err)
		return
	}

	usersToSend := make([]UserPayload, 0, len(usrs))
	for _, user := range usrs {
		payload := UserPayload{
			Username:    user.Username,
			Groups:      user.Groups,
			TwoFASendTo: user.TwoFASendTo,
		}
		if user.PasswordExpired != nil {
			payload.PasswordExpired = *user.PasswordExpired
		}
		usersToSend = append(usersToSend, payload)
	}

	response := api.NewSuccessPayload(usersToSend)
	al.writeJSONResponse(w, http.StatusOK, response)
}

func (al *APIListener) handleChangeUser(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)

	// if the username is part of the route params then we're updating the user, otherwise creating a new user
	userID, userIDExists := vars[routes.ParamUserID]
	if !userIDExists {
		userID = ""

		err := al.checkUserCount()
		if err != nil {
			al.jsonError(w, err)
			return
		}
	}

	var user users.User
	err := parseRequestBody(req.Body, &user)
	if err != nil {
		al.jsonError(w, err)
		return
	}

	if err := al.userService.Change(&user, userID); err != nil {
		al.jsonError(w, err)
		return
	}

	// A password set by an admin, or a password marked expired, must retire that
	// user's sessions: otherwise an attacker's stolen bearer survives the very
	// rotation performed to lock them out. The user's API tokens go with them
	// for the same reason: anyone who held a session could have minted one.
	passwordChanged := userIDExists && user.Password != ""
	if passwordChanged || (userIDExists && user.PasswordExpired != nil && *user.PasswordExpired) {
		ctx := req.Context()
		err := al.apiSessions.DeleteAllByUser(ctx, userID)
		if err != nil {
			titleMsg := fmt.Sprintf("password changed, unable to delete all sessions for user \"%s\"", userID)
			al.jsonErrorResponseWithDetail(w, http.StatusInternalServerError, "Unable to delete all User's sessions", titleMsg, err.Error())
			return
		}

		// The same request may have renamed the user, and the tokens with them.
		tokenOwner := userID
		if user.Username != "" {
			tokenOwner = user.Username
		}
		if err := al.tokenManager.DeleteAllByUser(ctx, tokenOwner); err != nil {
			titleMsg := fmt.Sprintf("password changed, unable to delete the API tokens of user \"%s\"", tokenOwner)
			al.jsonErrorResponseWithDetail(w, http.StatusInternalServerError, "Unable to delete the User's API tokens", titleMsg, err.Error())
			return
		}
		al.auditLog.Entry(auditlog.ApplicationAuthUserTokens, auditlog.ActionDelete).
			WithHTTPRequest(req).
			WithID(tokenOwner).
			Save()
	}

	if userIDExists {
		al.auditLog.Entry(auditlog.ApplicationAuthUser, auditlog.ActionUpdate).
			WithHTTPRequest(req).
			WithID(userID).
			Save()

		al.Debugf("User [%s] updated.", userID)
		w.WriteHeader(http.StatusNoContent)
	} else {
		al.auditLog.Entry(auditlog.ApplicationAuthUser, auditlog.ActionCreate).
			WithHTTPRequest(req).
			Save()

		al.Debugf("User [%s] created.", user.Username)
		w.WriteHeader(http.StatusCreated)
	}
}

func (al *APIListener) checkUserCount() (err error) {
	maxUsers := al.getMaxUsers()

	if maxUsers > 0 {
		users, err := al.userService.GetAll()
		if err != nil {
			return err
		}

		if len(users) >= maxUsers {
			return errors.New("failed to create user. max user limit reached. please upgrade your license for additional users")
		}
	}

	return nil
}

func (al *APIListener) getMaxUsers() (maxUsers int) {
	return 0
}

func (al *APIListener) handleDeleteUser(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	userID, userIDExists := vars[routes.ParamUserID]
	if !userIDExists {
		al.jsonErrorResponseWithTitle(w, http.StatusBadRequest, "Empty user id provided")
		return
	}

	if err := al.userService.Delete(userID); err != nil {
		al.jsonError(w, err)
		return
	}

	al.auditLog.Entry("user", auditlog.ActionDelete).
		WithHTTPRequest(req).
		WithID(userID).
		Save()

	w.WriteHeader(http.StatusNoContent)
	al.Debugf("User [%s] deleted.", userID)
}

func (al *APIListener) handleDeleteUsersTotP(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	userID, userIDProvided := vars[routes.ParamUserID]
	if !userIDProvided {
		al.jsonErrorResponseWithTitle(w, http.StatusBadRequest, "Empty user id provided")
		return
	}

	user, err := al.userService.GetByUsername(userID)
	if err != nil {
		al.jsonError(w, err)
		return
	}
	if user == nil {
		al.jsonErrorResponseWithTitle(w, http.StatusNotFound, "user not found")
		return
	}

	al.auditLog.Entry(auditlog.ApplicationAuthUserTotP, auditlog.ActionDelete).
		WithHTTPRequest(req).
		WithID(userID).
		Save()

	al.handleManageTotP(w, req, user, "delete")
}

func (al *APIListener) handleGetUserAPISessions(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	userID, userIDExists := vars[routes.ParamUserID]
	if !userIDExists {
		al.jsonError(w, ErrMissingUserIDParam)
		return
	}

	ctx := req.Context()

	userSessions, err := al.apiSessions.GetAllByUser(ctx, userID)
	if err != nil {
		titleMsg := fmt.Sprintf("unable to get sessions for user \"%s\"", userID)
		al.jsonErrorResponseWithDetail(w, http.StatusInternalServerError, "", titleMsg, err.Error())
		return
	}

	response := api.NewSuccessPayload(userSessions)
	al.writeJSONResponse(w, http.StatusOK, response)
}

func (al *APIListener) handleDeleteUserAPISession(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	userID, userIDExists := vars[routes.ParamUserID]
	if !userIDExists {
		al.jsonError(w, ErrMissingUserIDParam)
		return
	}

	sessionID, sessionIDExists := vars[routes.ParamSessionID]
	if !sessionIDExists {
		al.jsonError(w, ErrMissingUserIDParam)
		return
	}

	ctx := req.Context()

	sessID, err := strconv.ParseInt(sessionID, 10, 64)
	if err != nil {
		titleMsg := fmt.Sprintf("unable to parse session ID \"%s\" for user \"%s\"", sessionID, userID)
		al.jsonErrorResponseWithDetail(w, http.StatusInternalServerError, "", titleMsg, err.Error())
		return
	}

	err = al.apiSessions.DeleteByID(ctx, userID, sessID)
	if err != nil {
		titleMsg := fmt.Sprintf("unable to delete session \"%s\" for user \"%s\"", sessionID, userID)
		al.jsonErrorResponseWithDetail(w, http.StatusInternalServerError, "", titleMsg, err.Error())
		return
	}

	al.auditLog.Entry(auditlog.ApplicationAuthAPISession, auditlog.ActionDelete).
		WithHTTPRequest(req).
		WithID(userID).
		Save()

	w.WriteHeader(http.StatusNoContent)
}

func (al *APIListener) handleDeleteAllUserAPISessions(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	userID, userIDExists := vars[routes.ParamUserID]
	if !userIDExists {
		al.jsonError(w, ErrMissingUserIDParam)
		return
	}

	ctx := req.Context()

	err := al.apiSessions.DeleteAllByUser(ctx, userID)
	if err != nil {
		titleMsg := fmt.Sprintf("unable to delete all sessions for user \"%s\"", userID)
		al.jsonErrorResponseWithDetail(w, http.StatusInternalServerError, "", titleMsg, err.Error())
		return
	}

	al.auditLog.Entry(auditlog.ApplicationAuthAPISessions, auditlog.ActionDelete).
		WithHTTPRequest(req).
		WithID(userID).
		Save()

	w.WriteHeader(http.StatusNoContent)
}

// handleGetUserAPITokens lists the API tokens stored for a user. It does not
// require the user to exist, so an admin can also find tokens left under the
// name of a user who is gone.
func (al *APIListener) handleGetUserAPITokens(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	userID, userIDExists := vars[routes.ParamUserID]
	if !userIDExists {
		al.jsonError(w, ErrMissingUserIDParam)
		return
	}

	tokens, err := al.tokenManager.GetAll(req.Context(), userID)
	if err != nil {
		titleMsg := fmt.Sprintf("unable to get the API tokens of user \"%s\"", userID)
		al.jsonErrorResponseWithDetail(w, http.StatusInternalServerError, "", titleMsg, err.Error())
		return
	}

	al.writeJSONResponse(w, http.StatusOK, api.NewSuccessPayload(newAPITokenPayloads(tokens)))
}

// handleDeleteUserAPIToken deletes one API token of a user.
func (al *APIListener) handleDeleteUserAPIToken(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	userID, userIDExists := vars[routes.ParamUserID]
	if !userIDExists {
		al.jsonError(w, ErrMissingUserIDParam)
		return
	}

	prefix := vars[routes.ParamTokenPrefix]
	if len(prefix) != authorization.APITokenPrefixLength {
		al.jsonErrorResponseWithTitle(w, http.StatusBadRequest, "missing or invalid token prefix.")
		return
	}

	ctx := req.Context()

	token, err := al.tokenManager.Get(ctx, userID, prefix)
	if err != nil {
		titleMsg := fmt.Sprintf("unable to get API token \"%s\" of user \"%s\"", prefix, userID)
		al.jsonErrorResponseWithDetail(w, http.StatusInternalServerError, "", titleMsg, err.Error())
		return
	}
	if token == nil {
		al.jsonErrorResponseWithTitle(w, http.StatusNotFound, fmt.Sprintf("user %q has no API token with prefix %q", userID, prefix))
		return
	}

	if err := al.tokenManager.Delete(ctx, userID, prefix); err != nil {
		al.jsonError(w, err)
		return
	}

	al.auditLog.Entry(auditlog.ApplicationAuthUserToken, auditlog.ActionDelete).
		WithHTTPRequest(req).
		WithID(userID).
		Save()

	al.Debugf("APIToken [%s] of user [%s] is deleted.", prefix, userID)
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteAllUserAPITokens deletes every API token of a user.
func (al *APIListener) handleDeleteAllUserAPITokens(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	userID, userIDExists := vars[routes.ParamUserID]
	if !userIDExists {
		al.jsonError(w, ErrMissingUserIDParam)
		return
	}

	if err := al.tokenManager.DeleteAllByUser(req.Context(), userID); err != nil {
		titleMsg := fmt.Sprintf("unable to delete the API tokens of user \"%s\"", userID)
		al.jsonErrorResponseWithDetail(w, http.StatusInternalServerError, "", titleMsg, err.Error())
		return
	}

	al.auditLog.Entry(auditlog.ApplicationAuthUserTokens, auditlog.ActionDelete).
		WithHTTPRequest(req).
		WithID(userID).
		Save()

	al.Debugf("All API tokens of user [%s] are deleted.", userID)
	w.WriteHeader(http.StatusNoContent)
}
