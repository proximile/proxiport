package bearer

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"

	"github.com/proximile/proxiport/server/api/session"
	"github.com/proximile/proxiport/server/routes"
	"github.com/proximile/proxiport/share/logger"
)

const (
	DefaultMaxTokenLifetime = 90 * 24 * time.Hour
	DefaultTokenLifetime    = 10 * time.Minute
)

type AppTokenClaims struct {
	Username  string  `json:"username,omitempty"`
	SessionID int64   `json:"sessionID,omitempty"`
	Scopes    []Scope `json:"scopes,omitempty"`
	jwt.StandardClaims
}

type Scope struct {
	URI     string `json:"uri,omitempty"`
	Method  string `json:"method,omitempty"`
	Exclude bool   `json:"exclude,omitempty"`
}

var ScopesAllExcluding2FaCheck = []Scope{
	{
		URI:    "*",
		Method: "*",
	},
	{
		URI:     routes.AllRoutesPrefix + routes.Verify2FaRoute,
		Method:  "*",
		Exclude: true,
	},
}

var ScopesTotPCreateOnly = []Scope{
	{
		URI:    routes.AllRoutesPrefix + routes.TotPRoutes,
		Method: http.MethodPost,
	},
}

var Scopes2FaCheckOnly = []Scope{
	{
		URI:    routes.AllRoutesPrefix + routes.Verify2FaRoute,
		Method: http.MethodPost,
	},
}

type TokenContext struct {
	AppClaims *AppTokenClaims
	RawToken  string
	JwtToken  *jwt.Token
}

type APISessionUpdater interface {
	Save(ctx context.Context, session session.APISession) (sessionID int64, err error)
}

type APISessionGetter interface {
	Get(ctx context.Context, sessionID int64) (found bool, sessionInfo session.APISession, err error)
}

func CreateAuthToken(
	ctx context.Context,
	sessionUpdater APISessionUpdater,
	JWTSecret string,
	lifetime time.Duration,
	username string,
	scopes []Scope,
	userAgent string,
	remoteAddress string,
) (string, error) {
	if username == "" {
		return "", errors.New("username cannot be empty")
	}

	expiresAt := time.Now().Add(lifetime)

	// The token's id is stored with its session and checked on every use,
	// which ties the token to this one session: see SessionMatchesToken.
	tokenID, err := newTokenID()
	if err != nil {
		return "", err
	}

	newSession := session.APISession{
		ExpiresAt:    expiresAt,
		Username:     username,
		LastAccessAt: time.Now(),
		UserAgent:    userAgent,
		IPAddress:    remoteAddress,
		TokenID:      tokenID,
	}
	sessionID, err := sessionUpdater.Save(ctx, newSession)
	if err != nil {
		return "", err
	}

	claims := AppTokenClaims{
		Username:  username,
		SessionID: sessionID,
		StandardClaims: jwt.StandardClaims{
			Id: tokenID,
			// A real expiry in the token itself, so it is self-limiting even if
			// the server-side session check is ever bypassed. Equals the minted
			// lifetime (already capped at DefaultMaxTokenLifetime), giving an
			// absolute session cap rather than an indefinitely slideable one.
			// (No iat claim: jwt validates it, and a trailing verifier clock in a
			// multi-node setup would reject freshly minted tokens.)
			ExpiresAt: expiresAt.Unix(),
		},
		Scopes: scopes,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenStr, err := token.SignedString([]byte(JWTSecret))
	if err != nil {
		return "", err
	}

	return tokenStr, nil
}

// newTokenID returns 128 random bits in hex.
func newTokenID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate a token id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// SessionMatchesToken reports whether a stored session is the one a token
// was issued for: same user, same token id.
//
// The session id in a token is not enough to find its session. A session row
// is deleted on logout, on a password change and when an administrator ends
// it, and a later login can then be stored under an id the token still
// names; a token that was revoked would be accepted again, as its own user,
// against whoever's session that now is. The user name closes that across
// users. The token id, which only the server and the token's holder know,
// closes it for a later session of the same user.
func SessionMatchesToken(sessionInfo session.APISession, claims *AppTokenClaims) bool {
	if claims == nil || claims.Username == "" || sessionInfo.Username != claims.Username {
		return false
	}
	if sessionInfo.TokenID == "" || claims.Id == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(sessionInfo.TokenID), []byte(claims.Id)) == 1
}

func IncreaseSessionLifetime(
	ctx context.Context,
	sessionUpdater APISessionUpdater,
	s session.APISession) error {
	newExpirationDate := time.Now().Add(DefaultTokenLifetime)
	if s.ExpiresAt.Before(newExpirationDate) {
		s.ExpiresAt = newExpirationDate
	}
	_, err := sessionUpdater.Save(ctx, s)
	if err != nil {
		return err
	}
	return nil
}

func currentURIMatchesTokenScopes(currentURI, currentMethod string, tokenScopes []Scope) bool {
	// make it compatible with the old tokens which don't have scopes field in jwt
	if len(tokenScopes) == 0 {
		return true
	}
	currentURI = "/" + strings.Trim(currentURI, "/")

	hasAtLeastOneMatch := false
	hasExcludeMatch := false
	for _, tokenScope := range tokenScopes {
		uriMatched := tokenScope.URI == "*" || currentURI == tokenScope.URI
		methodMatched := tokenScope.Method == "*" || currentMethod == tokenScope.Method

		if uriMatched && methodMatched {
			if tokenScope.Exclude {
				hasExcludeMatch = true
			} else {
				hasAtLeastOneMatch = true
			}
		}
	}

	return hasAtLeastOneMatch && !hasExcludeMatch
}

func ParseToken(tokenStr string, JWTSecret string) (tokCtx *TokenContext, err error) {
	appClaims := &AppTokenClaims{}
	bearerToken, err := jwt.ParseWithClaims(tokenStr, appClaims, func(token *jwt.Token) (i interface{}, err error) {
		// Pin the algorithm to the one we mint with. Without this the parser
		// accepts whatever `alg` the token header declares and hands it our
		// secret bytes, which is the classic JWT algorithm-confusion footgun
		// (e.g. an attacker-chosen alg, or "none"). Belt-and-suspenders with the
		// WithValidMethods option below.
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %q", token.Header["alg"])
		}
		return []byte(JWTSecret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		// do not return error since it should respond with 401 instead of 500, just log it
		return nil, err
	}

	return &TokenContext{
		AppClaims: appClaims,
		RawToken:  tokenStr,
		JwtToken:  bearerToken,
	}, nil
}

func ValidateBearerToken(
	ctx context.Context,
	tokCtx *TokenContext,
	uri, method string,
	apiSessionGetter APISessionGetter,
	l *logger.Logger) (valid bool, sessionInfo session.APISession, err error) {
	if !currentURIMatchesTokenScopes(uri, method, tokCtx.AppClaims.Scopes) {
		l.Errorf(
			"Token scopes %+v don't match with the current url %s[%s], so this token is not intended to be used for this page",
			tokCtx.AppClaims.Scopes,
			method,
			uri,
		)
		return false, session.APISession{}, nil
	}

	if !tokCtx.JwtToken.Valid || tokCtx.AppClaims.Username == "" {
		l.Errorf(
			"Token is invalid or user name is empty",
			tokCtx.AppClaims.Username,
		)
		return false, session.APISession{}, nil
	}

	found, sessionInfo, err := apiSessionGetter.Get(ctx, tokCtx.AppClaims.SessionID)
	if err != nil || !found {
		l.Errorf(
			"Login session not found for %s",
			tokCtx.AppClaims.Username,
		)
		return false, session.APISession{}, err
	}

	if !SessionMatchesToken(sessionInfo, tokCtx.AppClaims) {
		l.Errorf(
			"Token of %s does not belong to login session %d",
			tokCtx.AppClaims.Username,
			tokCtx.AppClaims.SessionID,
		)
		return false, session.APISession{}, nil
	}

	isValidByExpirationTime := sessionInfo.ExpiresAt.After(time.Now())
	if !isValidByExpirationTime {
		l.Errorf(
			"api session time %v is expired",
			sessionInfo.ExpiresAt,
		)
		return false, sessionInfo, err
	}
	return true, sessionInfo, nil
}

func GetBearerToken(req *http.Request) (string, bool) {
	auth := req.Header.Get("Authorization")
	const prefix = "Bearer "
	// Case insensitive prefix match.
	if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
		return "", false
	}
	return auth[len(prefix):], true
}
