package chserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/api/authorization"
	"github.com/proximile/proxiport/server/api/users"
	"github.com/proximile/proxiport/server/chconfig"
	"github.com/proximile/proxiport/share/security"
)

// newUserTokensTestListener serves the real router for two users kept in a
// file: admin, an administrator, and user1, who is not one and holds the API
// tokens of the shared fixture.
func newUserTokensTestListener(t *testing.T) *APIListener {
	t.Helper()

	authFile := filepath.Join(t.TempDir(), "users.json")
	content := `[
  {"username":"admin","password":"` + mustHashPassword(t, "admin-password") + `","groups":["` + users.Administrators + `"]},
  {"username":"user1","password":"` + mustHashPassword(t, "user1-password") + `","groups":["users"]}
]`
	require.NoError(t, os.WriteFile(authFile, []byte(content), 0o600))
	provider, err := users.NewFileAdapter(testLog, users.NewFileManager(testLog, authFile))
	require.NoError(t, err)

	tokenManager := authorization.NewManager(
		CommonAPITokenTestDb(t, credTestUser, credTestPrefix, "the name", authorization.APITokenReadWrite, credTestSecret))
	sessions := newEmptyAPISessionCache(t)

	userService := users.NewAPIService(provider, false, 0, -1)
	userService.AddCredentialStore(tokenManager)
	userService.AddCredentialStore(sessions)

	al := &APIListener{
		Logger:       testLog,
		Server:       &Server{config: &chconfig.Config{API: chconfig.APIConfig{MaxRequestBytes: 1024 * 1024}}},
		bannedUsers:  security.NewBanList(0),
		apiSessions:  sessions,
		tokenManager: tokenManager,
		userService:  userService,
	}
	al.initRouter()
	return al
}

func serveAs(al *APIListener, username, password, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "/api/v1"+path, strings.NewReader(body))
	req.SetBasicAuth(username, password)
	w := httptest.NewRecorder()
	al.router.ServeHTTP(w, req)
	return w
}

func asAdmin(al *APIListener, method, path, body string) *httptest.ResponseRecorder {
	return serveAs(al, "admin", "admin-password", method, path, body)
}

// tokensOf returns the prefixes of the tokens stored for user1.
func tokensOf(t *testing.T, al *APIListener) []string {
	t.Helper()
	tokens, err := al.tokenManager.GetAll(context.Background(), credTestUser)
	require.NoError(t, err)
	prefixes := make([]string, 0, len(tokens))
	for _, token := range tokens {
		prefixes = append(prefixes, token.Prefix)
	}
	return prefixes
}

func TestAdminListsAnotherUsersAPITokens(t *testing.T) {
	al := newUserTokensTestListener(t)
	stored := tokensOf(t, al)
	require.NotEmpty(t, stored)

	w := asAdmin(al, http.MethodGet, "/users/user1/tokens", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var got struct {
		Data []map[string]interface{} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got.Data, len(stored))
	for _, token := range got.Data {
		assert.Contains(t, stored, token["prefix"])
		assert.NotContains(t, token, "token", "the listing must not carry the token or its hash")
	}
	assert.NotContains(t, w.Body.String(), "$2", "the listing must not carry a bcrypt hash")

	// A name nobody holds has no tokens, which is an empty list and not an error.
	w = asAdmin(al, http.MethodGet, "/users/nobody/tokens", "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.JSONEq(t, `{"data":[]}`, w.Body.String())
}

func TestOnlyAdminsReachAnotherUsersAPITokens(t *testing.T) {
	al := newUserTokensTestListener(t)
	before := tokensOf(t, al)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/users/user1/tokens"},
		{http.MethodDelete, "/users/user1/tokens"},
		{http.MethodDelete, "/users/user1/tokens/" + credTestPrefix},
	} {
		w := serveAs(al, "user1", "user1-password", tc.method, tc.path, "")
		assert.Equal(t, http.StatusForbidden, w.Code, "%s %s", tc.method, tc.path)
	}
	assert.ElementsMatch(t, before, tokensOf(t, al), "a refused request must not delete anything")
}

func TestAdminDeletesOneAPITokenOfAnotherUser(t *testing.T) {
	al := newUserTokensTestListener(t)
	before := tokensOf(t, al)
	require.Contains(t, before, credTestPrefix)
	require.Equal(t, http.StatusOK, serveAs(al, credTestUser, credTestToken, http.MethodGet, "/me", "").Code,
		"the token must work before it is deleted")

	w := asAdmin(al, http.MethodDelete, "/users/user1/tokens/"+credTestPrefix, "")
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	after := tokensOf(t, al)
	assert.NotContains(t, after, credTestPrefix)
	assert.Len(t, after, len(before)-1, "the user's other tokens must be untouched")
	assert.Equal(t, http.StatusUnauthorized, serveAs(al, credTestUser, credTestToken, http.MethodGet, "/me", "").Code,
		"the deleted token still authenticated")

	assert.Equal(t, http.StatusNotFound, asAdmin(al, http.MethodDelete, "/users/user1/tokens/"+credTestPrefix, "").Code)
	assert.Equal(t, http.StatusBadRequest, asAdmin(al, http.MethodDelete, "/users/user1/tokens/short", "").Code)
}

func TestAdminDeletesAllAPITokensOfAnotherUser(t *testing.T) {
	al := newUserTokensTestListener(t)
	require.NotEmpty(t, tokensOf(t, al))

	w := asAdmin(al, http.MethodDelete, "/users/user1/tokens", "")
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	assert.Empty(t, tokensOf(t, al))
	assert.Equal(t, http.StatusUnauthorized, serveAs(al, credTestUser, credTestToken, http.MethodGet, "/me", "").Code)
}

// A password an admin sets, or marks expired, is how a user is locked out.
// The user's API tokens must not outlive it.
func TestAdminPasswordResetDeletesTheUsersAPITokens(t *testing.T) {
	for name, body := range map[string]string{
		"new password":     `{"password":"another-password"}`,
		"password expired": `{"password_expired":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			al := newUserTokensTestListener(t)
			require.NotEmpty(t, tokensOf(t, al))

			w := asAdmin(al, http.MethodPut, "/users/user1", body)
			require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

			assert.Empty(t, tokensOf(t, al))
		})
	}
}

func TestOtherUserChangesKeepTheUsersAPITokens(t *testing.T) {
	al := newUserTokensTestListener(t)
	before := tokensOf(t, al)

	w := asAdmin(al, http.MethodPut, "/users/user1", `{"groups":["users","operators"]}`)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	assert.ElementsMatch(t, before, tokensOf(t, al))
}
