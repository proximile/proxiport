// TODO(v0.2): Replace the disabledProvider with a real OIDC client
// (likely backed by github.com/coreos/go-oidc/v3 and
// golang.org/x/oauth2). The interface below is the contract a real
// provider has to satisfy; see docs/oidc-design.md in the workspace
// for the full milestone plan and threat model.
//
// Open work items before a real implementation lands:
//   - Persist nonce/state/PKCE verifier across the redirect round-trip
//     (likely in a short-lived server-side cache keyed by state).
//   - Map UserInfo.Groups onto the existing user_groups table so an
//     OIDC-authenticated principal inherits ProxiPort permissions.
//   - Decide whether OIDC replaces or augments the local user table.

package oidc

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors returned by the Provider surface.
var (
	// ErrDisabled is returned by the default disabledProvider when no
	// OIDC configuration has been supplied. Callers should treat this
	// as "OIDC is off" rather than as a hard error.
	ErrDisabled = errors.New("oidc: provider is disabled")

	// ErrInvalidState is returned when the state value returned by the
	// IdP does not match the one issued by AuthCodeURL.
	ErrInvalidState = errors.New("oidc: state mismatch")

	// ErrInvalidNonce is returned when the nonce embedded in the ID
	// token does not match the one issued by AuthCodeURL.
	ErrInvalidNonce = errors.New("oidc: nonce mismatch")

	// ErrNoIDToken is returned when the IdP did not include an
	// id_token in its token-endpoint response.
	ErrNoIDToken = errors.New("oidc: response did not include an id_token")
)

// Provider is the abstract OIDC login surface used by the HTTP layer.
// Real implementations talk to an Identity Provider; the
// disabledProvider stub returns ErrDisabled for everything.
type Provider interface {
	// Name returns a stable, human-readable provider identifier
	// (e.g. "keycloak", "auth0", "google"). Used in log lines and
	// UI labels.
	Name() string

	// AuthCodeURL builds the URL the browser should be redirected to
	// in order to start an authorization-code flow. The caller is
	// expected to have generated `state` and `nonce` already and to
	// have stashed them somewhere that survives the redirect.
	AuthCodeURL(state, nonce string) (string, error)

	// Exchange trades an authorization code (and the matching PKCE
	// verifier) for an OAuth/OIDC token bundle. Implementations MUST
	// validate the id_token signature and the nonce claim before
	// returning.
	Exchange(ctx context.Context, code, codeVerifier string) (*Token, error)

	// UserInfo fetches the userinfo claims for a valid access token.
	// Implementations SHOULD prefer claims already present in the
	// id_token and only call the userinfo endpoint when needed.
	UserInfo(ctx context.Context, accessToken string) (*UserInfo, error)
}

// Token is the result of a successful OIDC token exchange.
type Token struct {
	// AccessToken is the OAuth 2.0 access token.
	AccessToken string
	// RefreshToken is the OAuth 2.0 refresh token, when issued.
	RefreshToken string
	// IDToken is the raw, compact-serialized OIDC ID token.
	IDToken string
	// TokenType is typically "Bearer".
	TokenType string
	// Expiry is the access-token expiration time, in the local clock.
	Expiry time.Time
}

// UserInfo is the subset of OIDC claims ProxiPort cares about. The
// Raw map preserves the full claim set so callers can pluck custom
// claims without us hard-coding them here.
type UserInfo struct {
	// Subject is the stable, opaque user identifier issued by the
	// IdP (the `sub` claim).
	Subject string
	// Email is the user's email, when scope=email was granted.
	Email string
	// EmailVerified mirrors the `email_verified` claim.
	EmailVerified bool
	// Name is the user's display name, when scope=profile was granted.
	Name string
	// PreferredUsername is the `preferred_username` claim when present.
	PreferredUsername string
	// Groups are the groups the IdP asserted for the user. The claim
	// path used to populate this field is governed by
	// Config.GroupsClaimPath.
	Groups []string
	// Raw is the full set of decoded claims, for callers that need
	// to read custom claims.
	Raw map[string]any
}

// New returns the default Provider. While the package is in
// scaffolding state, this always returns a disabledProvider; once a
// real implementation lands, this constructor will take a *Config and
// dispatch on it.
func New() Provider {
	return disabledProvider{}
}
