package chserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/proximile/proxiport/server/api/authorization"
	"github.com/proximile/proxiport/server/api/users"
	"github.com/proximile/proxiport/server/chconfig"
	chshare "github.com/proximile/proxiport/share"
	"github.com/proximile/proxiport/share/security"
)

func newAuthMiddlewareTestListener(t *testing.T, scope authorization.APITokenScope, maxBadAttempts int) *APIListener {
	t.Helper()
	//nolint:gosec // G101: a bcrypt hash of a fixture password, not a credential
	user := &users.User{
		Username: "user1",
		Password: "$2y$05$ep2DdPDeLDDhwRrED9q/vuVEzRpZtB5WHCFT7YbcmH9r9oNmlsZOm",
	}
	return &APIListener{
		Logger:      testLog,
		apiSessions: newEmptyAPISessionCache(t),
		bannedUsers: security.NewBanList(0),
		bannedIPs:   security.NewMaxBadAttemptsBanList(maxBadAttempts, time.Minute, testLog),
		Server: &Server{
			config: &chconfig.Config{
				API: chconfig.APIConfig{
					MaxRequestBytes: 1024 * 1024,
				},
			},
		},
		tokenManager: authorization.NewManager(
			CommonAPITokenTestDb(t, "user1", "theprefi", "the name", scope, "mynicefi-xedl-enth-long-livedpasswor")),
		userService: users.NewAPIService(users.NewStaticProvider([]*users.User{user}), false, 0, -1),
	}
}

func serveThroughAuth(al *APIListener, req *http.Request) int {
	handler := al.wrapWithAuthMiddleware(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w.Code
}

func TestAuthMiddlewareReadTokenOnWriteIsForbidden(t *testing.T) {
	al := newAuthMiddlewareTestListener(t, authorization.APITokenRead, 10)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/me/tokens", nil)
	req.SetBasicAuth("user1", "theprefi_mynicefi-xedl-enth-long-livedpasswor")
	assert.Equal(t, http.StatusForbidden, serveThroughAuth(al, req))

	req = httptest.NewRequest(http.MethodGet, "/api/v1/clients", nil)
	req.SetBasicAuth("user1", "theprefi_mynicefi-xedl-enth-long-livedpasswor")
	assert.Equal(t, http.StatusOK, serveThroughAuth(al, req))
}

func TestAuthMiddlewareSameDeadBearerCountsOnce(t *testing.T) {
	al := newAuthMiddlewareTestListener(t, authorization.APITokenReadWrite, 3)

	var ip string
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		req.Header.Set("Authorization", "Bearer not-a-valid-session-token")
		ip = chshare.RemoteIP(req)
		assert.Equal(t, http.StatusUnauthorized, serveThroughAuth(al, req))
	}
	assert.False(t, al.bannedIPs.IsBanned(ip), "one stale session token retried must not ban the address")
}

func TestAuthMiddlewareDistinctBearersStillBan(t *testing.T) {
	al := newAuthMiddlewareTestListener(t, authorization.APITokenReadWrite, 3)

	var ip string
	for _, token := range []string{"token-a", "token-b", "token-c"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		ip = chshare.RemoteIP(req)
		serveThroughAuth(al, req)
	}
	assert.True(t, al.bannedIPs.IsBanned(ip))
}

func TestAuthMiddlewareRepeatedWrongPasswordStillBans(t *testing.T) {
	al := newAuthMiddlewareTestListener(t, authorization.APITokenReadWrite, 3)

	var ip string
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		req.SetBasicAuth("user1", "wrong")
		ip = chshare.RemoteIP(req)
		assert.Equal(t, http.StatusUnauthorized, serveThroughAuth(al, req))
	}
	assert.True(t, al.bannedIPs.IsBanned(ip))
}
