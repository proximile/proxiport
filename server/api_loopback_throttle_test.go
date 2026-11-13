package chserver

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/api/authorization"
	"github.com/proximile/proxiport/server/api/message"
	"github.com/proximile/proxiport/server/bearer"
	chshare "github.com/proximile/proxiport/share"
	"github.com/proximile/proxiport/share/security"
)

// Callers that reach the API through a relay on the server's own host (an
// onion service mapped to 127.0.0.1, a same-host reverse proxy that is not a
// trusted proxy) all have a loopback address. Banning that address locked
// every one of them out, and ten anonymous requests were enough.

const loopbackTestMax = 3

var loopbackPeers = []string{"127.0.0.1:40000", "[::1]:40000", "127.9.8.7:40000"}

func newLoopbackTestListener(t *testing.T) *APIListener {
	t.Helper()
	al := newAuthMiddlewareTestListener(t, authorization.APITokenReadWrite, loopbackTestMax)
	al.bannedLoopbackUsers = security.NewMaxBadAttemptsBanList(loopbackTestMax, time.Minute, testLog)
	return al
}

func authRequest(peer, user, password string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	req.RemoteAddr = peer
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	return req
}

func TestLoopbackAnonymousRequestsLockNobodyOut(t *testing.T) {
	for _, peer := range loopbackPeers {
		t.Run(peer, func(t *testing.T) {
			al := newLoopbackTestListener(t)
			for i := 0; i < 4*loopbackTestMax; i++ {
				assert.Equal(t, http.StatusUnauthorized, serveThroughAuth(al, authRequest(peer, "", "")))
			}
			assert.False(t, al.bannedIPs.IsBanned(chshare.RemoteIP(authRequest(peer, "", ""))), "the loopback address must not be banned")
			assert.Equal(t, http.StatusOK, serveThroughAuth(al, authRequest(peer, "user1", "pwd")),
				"a valid login through the same relay still works")
		})
	}
}

func TestLoopbackRejectedBearerTokensLockNobodyOut(t *testing.T) {
	al := newLoopbackTestListener(t)
	peer := loopbackPeers[0]
	for i := 0; i < 4*loopbackTestMax; i++ {
		req := authRequest(peer, "", "")
		req.Header.Set("Authorization", fmt.Sprintf("Bearer not-a-session-%d", i))
		assert.Equal(t, http.StatusUnauthorized, serveThroughAuth(al, req))
	}
	assert.False(t, al.bannedIPs.IsBanned("127.0.0.1"))
	assert.Equal(t, http.StatusOK, serveThroughAuth(al, authRequest(peer, "user1", "pwd")))
}

// Guessing a password through the relay must stay as bounded as it is from an
// address of its own: the user name is banned for loopback callers, and only
// that name, and only there.
func TestLoopbackWrongPasswordsBanThatUserNameOnly(t *testing.T) {
	al := newLoopbackTestListener(t)
	peer := loopbackPeers[0]

	// A session of that user, signed in before the guessing starts.
	al.config.API.JWTSecret = "a-test-signing-secret-of-enough-length" //nolint:gosec // G101: a fixture
	session, err := bearer.CreateAuthToken(context.Background(), al.apiSessions, al.config.API.JWTSecret,
		time.Minute, "user1", bearer.ScopesAllExcluding2FaCheck, "test", "127.0.0.1")
	require.NoError(t, err)
	withSession := func() *http.Request {
		req := authRequest(peer, "", "")
		req.Header.Set("Authorization", "Bearer "+session)
		return req
	}
	require.Equal(t, http.StatusOK, serveThroughAuth(al, withSession()))

	for i := 0; i < loopbackTestMax; i++ {
		assert.Equal(t, http.StatusUnauthorized, serveThroughAuth(al, authRequest(peer, "user1", fmt.Sprintf("wrong-%d", i))))
	}

	assert.Equal(t, http.StatusTooManyRequests, serveThroughAuth(al, authRequest(peer, "user1", "pwd")),
		"after the limit the correct password is refused too")
	assert.Equal(t, http.StatusTooManyRequests, serveThroughAuth(al, authRequest("[::1]:40001", "user1", "pwd")),
		"the ban holds for every loopback address")

	assert.Equal(t, http.StatusOK, serveThroughAuth(al, withSession()),
		"a session of that user that is already signed in is not ended")
	assert.Equal(t, http.StatusUnauthorized, serveThroughAuth(al, authRequest(peer, "", "")),
		"an anonymous caller behind the relay is not affected")
	assert.Equal(t, http.StatusUnauthorized, serveThroughAuth(al, authRequest(peer, "user2", "wrong")),
		"another user name behind the relay is not affected")
	assert.False(t, al.bannedIPs.IsBanned("127.0.0.1"), "the address itself is never banned")
	assert.Equal(t, http.StatusOK, serveThroughAuth(al, authRequest("192.0.2.77:40000", "user1", "pwd")),
		"the same user from an address of its own is not affected")
}

// Refused attempts during the ban are not failures of their own: they must not
// extend the ban.
func TestLoopbackRefusedAttemptsDoNotExtendTheBan(t *testing.T) {
	al := newLoopbackTestListener(t)
	al.bannedLoopbackUsers = security.NewMaxBadAttemptsBanList(loopbackTestMax, 300*time.Millisecond, testLog)
	peer := loopbackPeers[0]

	for i := 0; i < loopbackTestMax; i++ {
		serveThroughAuth(al, authRequest(peer, "user1", "wrong"))
	}
	deadline := time.Now().Add(250 * time.Millisecond)
	for time.Now().Before(deadline) {
		require.Equal(t, http.StatusTooManyRequests, serveThroughAuth(al, authRequest(peer, "user1", "wrong")))
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(150 * time.Millisecond)
	assert.Equal(t, http.StatusOK, serveThroughAuth(al, authRequest(peer, "user1", "pwd")),
		"the ban ends ban_time after it began, however often the caller knocked meanwhile")
}

func TestLoopbackCompletedLoginClearsThatUsersCounter(t *testing.T) {
	al := newLoopbackTestListener(t)
	req := authRequest(loopbackPeers[0], "", "")

	for i := 0; i < loopbackTestMax-1; i++ {
		al.handleBannedIPs(req, false, "user1")
	}
	al.recordAuthSuccess(req, "user1")
	for i := 0; i < loopbackTestMax-1; i++ {
		al.handleBannedIPs(req, false, "user1")
	}
	assert.False(t, al.loopbackUserBanned("127.0.0.1", "user1"))

	al.handleBannedIPs(req, false, "user1")
	assert.True(t, al.loopbackUserBanned("127.0.0.1", "user1"))
	assert.False(t, al.loopbackUserBanned("192.0.2.1", "user1"), "the ban is for loopback callers")
	assert.False(t, al.loopbackUserBanned("127.0.0.1", ""), "no name, no ban")
}

// Control: an address of its own is still banned as a whole, by anonymous
// requests too. The change must not weaken that.
func TestOwnAddressIsStillBannedAsAWhole(t *testing.T) {
	al := newLoopbackTestListener(t)
	peer := "192.0.2.50:40000"
	for i := 0; i < loopbackTestMax; i++ {
		serveThroughAuth(al, authRequest(peer, "", ""))
	}
	assert.True(t, al.bannedIPs.IsBanned("192.0.2.50"))
	assert.False(t, al.bannedIPs.IsBanned("127.0.0.1"))
}

// With the relay listed in trusted_proxies the caller has an address of its
// own again, and is counted by it.
func TestLoopbackTrustedProxyCallerIsCountedByItsForwardedAddress(t *testing.T) {
	require.NoError(t, chshare.SetTrustedProxies([]string{"127.0.0.1"}))
	t.Cleanup(func() { _ = chshare.SetTrustedProxies(nil) })

	al := newLoopbackTestListener(t)
	for i := 0; i < loopbackTestMax; i++ {
		req := authRequest(loopbackPeers[0], "", "")
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		serveThroughAuth(al, req)
	}
	assert.True(t, al.bannedIPs.IsBanned("203.0.113.9"))
}

// The login route, through the router and its ban middleware.
func TestLoopbackLoginRoute(t *testing.T) {
	al := newLoopbackTestListener(t)
	al.initRouter()

	login := func(peer, user, password string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/login", nil)
		req.RemoteAddr = peer
		if user != "" {
			req.SetBasicAuth(user, password)
		}
		w := httptest.NewRecorder()
		al.router.ServeHTTP(w, req)
		return w.Code
	}
	peer := loopbackPeers[0]

	for i := 0; i < 4*loopbackTestMax; i++ {
		require.Equal(t, http.StatusUnauthorized, login(peer, "", ""))
	}
	require.Equal(t, http.StatusOK, login(peer, "user1", "pwd"), "an anonymous flood locks nobody out")

	for i := 0; i < loopbackTestMax; i++ {
		require.Equal(t, http.StatusUnauthorized, login(peer, "user1", fmt.Sprintf("wrong-%d", i)))
	}
	assert.Equal(t, http.StatusTooManyRequests, login(peer, "user1", "pwd"), "guessing is bounded")
	assert.Equal(t, http.StatusUnauthorized, login(peer, "", ""), "others are not locked out")

	// Control through the same router: an address of its own is locked as a whole.
	other := "192.0.2.60:40000"
	for i := 0; i < loopbackTestMax; i++ {
		login(other, "", "")
	}
	assert.Equal(t, http.StatusLocked, login(other, "user1", "pwd"))
}

// Guessing the second factor with a known password, through the relay.
func TestLoopbackWrongSecondFactorsBanThatUserName(t *testing.T) {
	al := newLoopbackTestListener(t)
	al.config.API.TotPEnabled = true
	al.config.API.JWTSecret = "a-test-signing-secret-of-enough-length" //nolint:gosec // G101: a fixture
	al.twoFASrv = NewTwoFAService(100, time.Second, al.userService, &message.ServiceMock{})

	user, err := al.userService.GetByUsername("user1")
	require.NoError(t, err)
	totP, err := GenerateTotPSecretKey(&TotPInput{Issuer: "test", AccountName: "user1"})
	require.NoError(t, err)
	StoreTotPCodeInUser(user, totP)

	handler := al.wrapWithAuthMiddleware(true)(al.handlePostVerify2FAToken())
	peer := loopbackPeers[0]

	guess := func() int {
		// The attacker has the password: each guess starts from a fresh
		// password step, so the per-session failure limit does not stop them.
		al.twoFASrv.SetTotPLoginSession("user1", time.Minute)
		pending, err := bearer.CreateAuthToken(context.Background(), al.apiSessions, al.config.API.JWTSecret,
			time.Minute, "user1", bearer.Scopes2FaCheckOnly, "test", "127.0.0.1")
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/verify-2fa",
			bytes.NewBufferString(`{"username":"user1","token":"000000"}`))
		req.RemoteAddr = peer
		req.Header.Set("Authorization", "Bearer "+pending)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w.Code
	}

	for i := 0; i < loopbackTestMax; i++ {
		require.Equal(t, http.StatusUnauthorized, guess(), "wrong code %d", i)
	}
	assert.Equal(t, http.StatusTooManyRequests, guess(), "the next guess is refused without being looked at")
	assert.True(t, al.loopbackUserBanned("127.0.0.1", "user1"))
	assert.False(t, al.bannedIPs.IsBanned("127.0.0.1"))
	assert.Equal(t, http.StatusUnauthorized, serveThroughAuth(al, authRequest(peer, "", "")),
		"others behind the relay are not locked out")
}
