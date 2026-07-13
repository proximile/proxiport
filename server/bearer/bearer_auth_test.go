package bearer

import (
	"context"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"

	"github.com/proximile/proxiport/server/api/session"
)

const testSecret = "unit-test-secret-please-ignore"

type stubSessionUpdater struct{}

func (stubSessionUpdater) Save(_ context.Context, _ session.APISession) (int64, error) {
	return 1, nil
}

func mintValid(t *testing.T, lifetime time.Duration) string {
	t.Helper()
	tok, err := CreateAuthToken(context.Background(), stubSessionUpdater{}, testSecret, lifetime, "alice", nil, "ua", "1.2.3.4")
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}
	return tok
}

// CreateAuthToken must embed a real exp claim (absolute session cap), not just
// track expiry server-side.
func TestCreateAuthTokenSetsExpiry(t *testing.T) {
	tok := mintValid(t, time.Hour)
	tokCtx, err := ParseToken(tok, testSecret)
	if err != nil {
		t.Fatalf("ParseToken: %v", err)
	}
	if tokCtx.AppClaims.ExpiresAt == 0 {
		t.Fatal("expected an exp claim on the minted token")
	}
	if delta := tokCtx.AppClaims.ExpiresAt - time.Now().Unix(); delta < 3500 || delta > 3700 {
		t.Fatalf("exp not ~1h out: delta=%d", delta)
	}
}

func TestParseTokenAcceptsValid(t *testing.T) {
	if _, err := ParseToken(mintValid(t, time.Hour), testSecret); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
}

// alg=none must never be accepted, even though the header claims a valid type.
func TestParseTokenRejectsNoneAlg(t *testing.T) {
	claims := AppTokenClaims{Username: "alice", StandardClaims: jwt.StandardClaims{ExpiresAt: time.Now().Add(time.Hour).Unix()}}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseToken(tok, testSecret); err == nil {
		t.Fatal("alg=none token was accepted")
	}
}

// A different (but still HMAC) algorithm must be rejected because we pin HS256.
func TestParseTokenRejectsWrongAlg(t *testing.T) {
	claims := AppTokenClaims{Username: "alice", StandardClaims: jwt.StandardClaims{ExpiresAt: time.Now().Add(time.Hour).Unix()}}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS384, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseToken(tok, testSecret); err == nil {
		t.Fatal("HS384 token accepted despite HS256 pin")
	}
}

func TestParseTokenRejectsExpired(t *testing.T) {
	claims := AppTokenClaims{Username: "alice", StandardClaims: jwt.StandardClaims{ExpiresAt: time.Now().Add(-time.Hour).Unix()}}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testSecret))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseToken(tok, testSecret); err == nil {
		t.Fatal("expired token was accepted")
	}
}

func TestParseTokenRejectsWrongSignature(t *testing.T) {
	claims := AppTokenClaims{Username: "alice", StandardClaims: jwt.StandardClaims{ExpiresAt: time.Now().Add(time.Hour).Unix()}}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("a-different-secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseToken(tok, testSecret); err == nil {
		t.Fatal("token signed with the wrong secret was accepted")
	}
}
