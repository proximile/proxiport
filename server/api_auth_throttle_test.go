package chserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/proximile/proxiport/share/security"
)

// The auth middleware called handleBannedIPs with authorized=true on every
// request that carried a valid bearer, and that cleared the per-IP failure
// counter. A /verify-2fa brute force carries a valid 2FA-pending bearer on every
// guess, so the limiter was reset before each attempt and never engaged: TOTP
// was brute-forceable by anyone holding the password.
func TestHandleBannedIPsDoesNotClearCounterOnAuthorizedRequests(t *testing.T) {
	al := &APIListener{
		bannedIPs: security.NewMaxBadAttemptsBanList(3, time.Minute, nil),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/verify-2fa", nil)
	req.RemoteAddr = "192.0.2.10:5555"

	for i := 0; i < 3; i++ {
		// The shape of the attack: one authorized request (the bearer check
		// passes) followed by the failed second factor.
		al.handleBannedIPs(req, true)
		al.handleBannedIPs(req, false)
	}

	assert.True(t, al.bannedIPs.IsBanned("192.0.2.10"),
		"an authorized request must not reset the per-IP failure counter")
}

// A completed authentication still clears it.
func TestRecordAuthSuccessClearsCounter(t *testing.T) {
	al := &APIListener{
		bannedIPs: security.NewMaxBadAttemptsBanList(3, time.Minute, nil),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/login", nil)
	req.RemoteAddr = "192.0.2.11:5555"

	al.handleBannedIPs(req, false)
	al.handleBannedIPs(req, false)
	al.recordAuthSuccess(req)
	al.handleBannedIPs(req, false)
	al.handleBannedIPs(req, false)

	assert.False(t, al.bannedIPs.IsBanned("192.0.2.11"))
}

// A clients-auth token used to match any path merely containing the string.
func TestRouteMatchesIsNotSubstringMatching(t *testing.T) {
	for _, tc := range []struct {
		path  string
		route string
		want  bool
	}{
		{"/api/v1/clients-auth", "/clients-auth", true},
		{"/api/v1/clients-auth/abc", "/clients-auth", true},
		{"/clients-auth", "/clients-auth", true},
		{"/api/v1/users/clients-auth", "/clients-auth", false},
		{"/api/v1/user-groups/clients-auth", "/clients-auth", false},
		{"/api/v1/clients/clients-auth/tunnels", "/clients-auth", false},
		{"/api/v1/ws/commands", "/ws", true},
		{"/api/v1/ws-ticket", "/ws", false},
	} {
		assert.Equal(t, tc.want, routeMatches(tc.path, tc.route), "%s vs %s", tc.path, tc.route)
	}
}

// A read-scoped token must not reach secret material or a way out of its scope.
func TestReadScopeDenied(t *testing.T) {
	for _, tc := range []struct {
		path string
		want bool
	}{
		{"/api/v1/me/totp-secret", true},
		{"/api/v1/ws/commands", true},
		{"/api/v1/ws-ticket", true},
		{"/api/v1/vault/12", true},
		{"/api/v1/vault", false},
		{"/api/v1/clients", false},
		{"/api/v1/me", false},
	} {
		assert.Equal(t, tc.want, readScopeDenied(tc.path), tc.path)
	}
}

// With the +-1 skew a code stays valid for ~90s, so the same one must not be
// accepted twice.
func TestAcceptTotPStepRejectsReplay(t *testing.T) {
	srv := &TwoFAService{lastTotPStep: make(map[string]int64)}

	assert.True(t, srv.acceptTotPStep("alice", 100))
	assert.False(t, srv.acceptTotPStep("alice", 100), "same step replayed")
	assert.False(t, srv.acceptTotPStep("alice", 99), "earlier step replayed")
	assert.True(t, srv.acceptTotPStep("alice", 101))
	assert.True(t, srv.acceptTotPStep("bob", 100), "other users are independent")
}
