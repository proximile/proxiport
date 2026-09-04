package chserver

import (
	"context"
	"crypto/subtle"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/proximile/proxiport/server/api/users"

	errors2 "github.com/proximile/proxiport/server/api/errors"
	"github.com/proximile/proxiport/server/api/message"
	"github.com/proximile/proxiport/share/security"
)

type TwoFAService struct {
	TokenTTL    time.Duration
	MsgSrv      message.Service
	UserSrv     UserService
	SendTimeout time.Duration

	tokensByUser map[string]*expirableToken
	// lastTotPStep is the most recent TotP time step accepted for a user, so the
	// same code cannot be replayed inside the +-1 skew window.
	lastTotPStep map[string]int64
	mu           sync.RWMutex
}

func NewTwoFAService(tokenTTLSeconds int, sendTimeout time.Duration, userSrv UserService, msgSrv message.Service) TwoFAService {
	return TwoFAService{
		TokenTTL:     time.Duration(tokenTTLSeconds) * time.Second,
		UserSrv:      userSrv,
		MsgSrv:       msgSrv,
		SendTimeout:  sendTimeout,
		tokensByUser: make(map[string]*expirableToken),
		lastTotPStep: make(map[string]int64),
	}
}

type expirableToken struct {
	token    string
	expiry   time.Time
	failures int
	// pendingPassword is a password change requested during the password step of
	// a 2FA login. It is applied only once the second factor is verified.
	pendingPassword string
}

const twoFATokenLength = 6

// maxTwoFAFailures is how many wrong second factors a single pending login
// session tolerates before it is discarded and the user must re-authenticate
// with their password. Without it a pending session lived for its whole TTL and
// accepted unlimited guesses.
const maxTwoFAFailures = 5

// minTwoFAResendInterval is how much of a token's remaining lifetime must have
// elapsed before a new one is sent. While a code is comfortably live, a repeated
// login returns the same delivery target without sending again.
const minTwoFAResendInterval = 30 * time.Second

// acceptTotPStep records a TotP time step as used and reports whether it was
// still unused. Codes from the current or an earlier step are refused, so an
// observed code cannot be replayed while it is still inside the skew window.
func (srv *TwoFAService) acceptTotPStep(username string, step int64) bool {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	if srv.lastTotPStep == nil {
		srv.lastTotPStep = make(map[string]int64)
	}
	if last, ok := srv.lastTotPStep[username]; ok && step <= last {
		return false
	}
	srv.lastTotPStep[username] = step
	return true
}

// registerFailure counts a wrong second factor against the pending login
// session and discards the session once the limit is reached. It is a no-op if
// the session was already replaced by a concurrent login.
func (srv *TwoFAService) registerFailure(username string, t *expirableToken) {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	cur, ok := srv.tokensByUser[username]
	if !ok || cur != t {
		return
	}
	cur.failures++
	if cur.failures >= maxTwoFAFailures {
		delete(srv.tokensByUser, username)
	}
}

func (srv *TwoFAService) SendToken(ctx context.Context, username string, userAgent string, remoteAddress string) (sendTo string, err error) {
	ctx, cancel := context.WithTimeout(ctx, srv.SendTimeout)
	defer cancel()

	if username == "" {
		return "", errors2.APIError{
			Message:    "username cannot be empty",
			HTTPStatus: http.StatusBadRequest,
		}
	}

	user, err := srv.UserSrv.GetByUsername(username)
	if err != nil {
		return "", err
	}
	if user == nil {
		return "", errors2.APIError{
			Message:    fmt.Sprintf("user with username %s not found", username),
			HTTPStatus: http.StatusNotFound,
		}
	}

	if user.TwoFASendTo == "" {
		return "", errors2.APIError{
			Message:    "no two_fa_send_to set for this user",
			HTTPStatus: http.StatusBadRequest,
		}
	}

	// Every /login with a correct password used to mint and send a fresh code.
	// That is an SMS/email bombing primitive, and each send also invalidated the
	// code the real user was in the middle of typing. Reuse the live one instead.
	srv.mu.RLock()
	existing := srv.tokensByUser[username]
	srv.mu.RUnlock()
	if existing != nil && existing.token != "" && time.Now().Before(existing.expiry.Add(-minTwoFAResendInterval)) {
		return user.TwoFASendTo, nil
	}

	token, err := security.NewRandomToken(twoFATokenLength)
	if err != nil {
		return "", fmt.Errorf("failed to generate 2fa token: %wv", err)
	}

	data := message.Data{
		SendTo:        user.TwoFASendTo,
		Token:         token,
		TTL:           srv.TokenTTL,
		Title:         "🔐 ProxiPort Two-Factor Token",
		RemoteAddress: remoteAddress,
		UserAgent:     userAgent,
	}
	if err := srv.MsgSrv.Send(ctx, data); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return "", fmt.Errorf("failed to send 2fa verification code: %w", err)
	}

	srv.mu.Lock()
	srv.tokensByUser[username] = &expirableToken{
		token:  token,
		expiry: time.Now().Add(srv.TokenTTL),
	}
	srv.mu.Unlock()

	return user.TwoFASendTo, nil
}

// SetPendingPasswordChange stores a password the user asked to set while
// logging in, to be applied only after the second factor is verified. Applying
// it at the password step let anyone holding just the password rotate it and
// lock the real owner out without ever completing a login.
func (srv *TwoFAService) SetPendingPasswordChange(username, newPassword string) {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if t, ok := srv.tokensByUser[username]; ok {
		t.pendingPassword = newPassword
	}
}

func (srv *TwoFAService) SetTotPLoginSession(username string, loginSessionTTL time.Duration) {
	srv.mu.Lock()
	srv.tokensByUser[username] = &expirableToken{
		expiry: time.Now().Add(loginSessionTTL),
	}
	srv.mu.Unlock()
}

func (srv *TwoFAService) ValidateTotPCode(user *users.User, code string) (pendingPassword string, err error) {
	srv.mu.RLock()
	t := srv.tokensByUser[user.Username]
	srv.mu.RUnlock()

	if t == nil {
		return "", errors2.APIError{
			Message:    "login request not found for provided username",
			HTTPStatus: http.StatusUnauthorized,
		}
	}

	if time.Now().After(t.expiry) {
		return "", errors2.APIError{
			Message:    "login request expired",
			HTTPStatus: http.StatusUnauthorized,
		}
	}

	totP, err := GetUsersTotPCode(user)
	if err != nil {
		return "", errors2.APIError{
			Err:        err,
			HTTPStatus: http.StatusInternalServerError,
		}
	}
	if totP == nil || totP.Secret == "" {
		return "", errors2.APIError{
			Message:    "time based one time secret key should be generated for this user",
			HTTPStatus: http.StatusConflict,
		}
	}

	step, valid := CheckTotPCodeStep(code, totP)
	if !valid {
		srv.registerFailure(user.Username, t)
		return "", errors2.APIError{
			Message:    "invalid code",
			HTTPStatus: http.StatusUnauthorized,
		}
	}

	if !srv.acceptTotPStep(user.Username, step) {
		srv.registerFailure(user.Username, t)
		return "", errors2.APIError{
			Message:    "code already used, wait for the next one",
			HTTPStatus: http.StatusUnauthorized,
		}
	}

	// Consume the login session so the code can't be reused. The map is mutated
	// only under a write lock, and only if the entry we validated is still
	// current (a concurrent login may have replaced it).
	srv.mu.Lock()
	if cur := srv.tokensByUser[user.Username]; cur == t {
		pendingPassword = cur.pendingPassword
		delete(srv.tokensByUser, user.Username)
	}
	srv.mu.Unlock()

	return pendingPassword, nil
}

func (srv *TwoFAService) ValidateToken(username, token string) (pendingPassword string, err error) {
	srv.mu.RLock()
	t := srv.tokensByUser[username]
	srv.mu.RUnlock()

	if t == nil {
		return "", errors2.APIError{
			Message:    "2fa token not found for provided username",
			HTTPStatus: http.StatusUnauthorized,
		}
	}

	if time.Now().After(t.expiry) {
		return "", errors2.APIError{
			Message:    "2fa token expired",
			HTTPStatus: http.StatusUnauthorized,
		}
	}

	if subtle.ConstantTimeCompare([]byte(t.token), []byte(token)) != 1 {
		srv.registerFailure(username, t)
		return "", errors2.APIError{
			Message:    "invalid token",
			HTTPStatus: http.StatusUnauthorized,
		}
	}

	// Single-use: consume the OTP on success so it can't be replayed within its
	// TTL. Mutate the map only under a write lock, and only if the entry we
	// validated is still current.
	srv.mu.Lock()
	if cur := srv.tokensByUser[username]; cur == t {
		pendingPassword = cur.pendingPassword
		delete(srv.tokensByUser, username)
	}
	srv.mu.Unlock()

	return pendingPassword, nil
}
