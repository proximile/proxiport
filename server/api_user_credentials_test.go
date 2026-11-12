package chserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/api/authorization"
	"github.com/proximile/proxiport/server/api/session"
	"github.com/proximile/proxiport/server/api/users"
	"github.com/proximile/proxiport/server/chconfig"
	"github.com/proximile/proxiport/share/security"
)

const (
	credTestUser   = "user1"
	credTestPrefix = "theprefi"
	credTestSecret = "mynicefi-xedl-enth-long-livedpasswor" //nolint:gosec // G101: a fixture token, not a credential
	credTestToken  = credTestPrefix + "_" + credTestSecret
)

// newUserCredentialsTestListener builds a listener whose users live in a file,
// so they can be added, renamed and deleted, wired to a real token database
// and session cache the way NewAPIListener wires them. user1 starts with one
// API token and one session.
func newUserCredentialsTestListener(t *testing.T) (*APIListener, *users.APIService) {
	t.Helper()

	authFile := filepath.Join(t.TempDir(), "users.json")
	require.NoError(t, os.WriteFile(authFile, []byte(`[{"username":"`+credTestUser+`","password":"`+mustHashPassword(t, "first-password")+`","groups":[]}]`), 0o600))
	provider, err := users.NewFileAdapter(testLog, users.NewFileManager(testLog, authFile))
	require.NoError(t, err)

	tokenManager := authorization.NewManager(
		CommonAPITokenTestDb(t, credTestUser, credTestPrefix, "the name", authorization.APITokenReadWrite, credTestSecret))
	sessions := newEmptyAPISessionCache(t)
	_, err = sessions.Save(context.Background(), session.APISession{
		Username:     credTestUser,
		ExpiresAt:    time.Now().Add(time.Hour),
		LastAccessAt: time.Now(),
	})
	require.NoError(t, err)

	userService := users.NewAPIService(provider, false, 0, -1)
	userService.AddCredentialStore(tokenManager)
	userService.AddCredentialStore(sessions)

	al := &APIListener{
		Logger:      testLog,
		apiSessions: sessions,
		bannedUsers: security.NewBanList(0),
		bannedIPs:   security.NewMaxBadAttemptsBanList(100, time.Minute, testLog),
		Server: &Server{
			config: &chconfig.Config{
				API: chconfig.APIConfig{
					MaxRequestBytes: 1024 * 1024,
				},
			},
		},
		tokenManager: tokenManager,
		userService:  userService,
	}
	return al, userService
}

func statusWithToken(al *APIListener, username string) int {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/clients", nil)
	req.SetBasicAuth(username, credTestToken)
	return serveThroughAuth(al, req)
}

// sessionCount is the number of sessions stored for the test user.
func sessionCount(t *testing.T, al *APIListener) int {
	t.Helper()
	found, err := al.apiSessions.GetAllByUser(context.Background(), credTestUser)
	require.NoError(t, err)
	return len(found)
}

// An API token issued to a user who is then deleted must not start working
// again when someone else is given the same username.
func TestDeletedUsersTokenDoesNotAuthenticateTheNextUserOfThatName(t *testing.T) {
	al, userService := newUserCredentialsTestListener(t)

	require.Equal(t, http.StatusOK, statusWithToken(al, credTestUser), "the token must work while its user exists")
	require.Equal(t, 1, sessionCount(t, al))

	require.NoError(t, userService.Delete(credTestUser))
	assert.Equal(t, http.StatusUnauthorized, statusWithToken(al, credTestUser))
	assert.Equal(t, 0, sessionCount(t, al), "the deleted user's sessions must be gone")

	require.NoError(t, userService.Change(&users.User{Username: credTestUser, Password: "second-password"}, ""))
	assert.Equal(t, http.StatusUnauthorized, statusWithToken(al, credTestUser),
		"the deleted user's token authenticated the new user of the same name")

	tokens, err := al.tokenManager.GetAll(context.Background(), credTestUser)
	require.NoError(t, err)
	assert.Empty(t, tokens)
}

// A token left behind before users and tokens were kept in step must not
// survive the creation of a user with its name either.
func TestLeftoverTokenIsClearedWhenAUserIsCreated(t *testing.T) {
	al, userService := newUserCredentialsTestListener(t)

	// Take the user away behind the service's back, as older versions did.
	require.NoError(t, userService.Provider.Delete(credTestUser))
	tokens, err := al.tokenManager.GetAll(context.Background(), credTestUser)
	require.NoError(t, err)
	require.NotEmpty(t, tokens, "a leftover token is the precondition of this test")

	require.NoError(t, userService.Change(&users.User{Username: credTestUser, Password: "second-password"}, ""))

	assert.Equal(t, http.StatusUnauthorized, statusWithToken(al, credTestUser))
	assert.Equal(t, 0, sessionCount(t, al))
}

// A renamed user keeps their tokens under the new name. Nothing stays under
// the old name for a later user to inherit.
func TestRenamedUserKeepsTokensAndTheOldNameKeepsNothing(t *testing.T) {
	al, userService := newUserCredentialsTestListener(t)

	require.NoError(t, userService.Change(&users.User{Username: "user9"}, credTestUser))

	assert.Equal(t, http.StatusOK, statusWithToken(al, "user9"), "the token must follow the renamed user")
	assert.Equal(t, 0, sessionCount(t, al), "sessions under the old name must end")

	require.NoError(t, userService.Change(&users.User{Username: credTestUser, Password: "second-password"}, ""))
	assert.Equal(t, http.StatusUnauthorized, statusWithToken(al, credTestUser),
		"the renamed user's token authenticated a new user of the old name")
	assert.Equal(t, http.StatusOK, statusWithToken(al, "user9"))
}
