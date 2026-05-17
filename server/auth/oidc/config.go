// TODO(v0.2): Config currently has no Validate logic and no loader.
// When wiring lands, the loader should:
//   - read the [auth-oidc] block from proxiportd.conf,
//   - bind it via the existing mapstructure loader path,
//   - reject configs that are partially populated (e.g. ClientID set
//     but Issuer empty),
//   - and refuse to start if RedirectURI is not an HTTPS URL outside
//     of a local-dev override.
// See docs/oidc-design.md for the agreed config shape.

package oidc

// Config holds operator-supplied OIDC provider settings. While the
// package is scaffolding, no loader populates this struct; it exists
// to pin down the field set a future loader will use.
type Config struct {
	// Issuer is the OIDC issuer URL. The provider's OpenID
	// configuration document is expected at
	// `${Issuer}/.well-known/openid-configuration`.
	Issuer string `mapstructure:"issuer"`

	// ClientID is the OAuth 2.0 client identifier issued to
	// ProxiPort by the IdP.
	ClientID string `mapstructure:"client_id"`

	// ClientSecret is the OAuth 2.0 client secret. For public
	// (PKCE-only) clients, leave empty.
	ClientSecret string `mapstructure:"client_secret"`

	// RedirectURI is the absolute URL the IdP redirects the browser
	// back to after the user authenticates. It must be registered
	// with the IdP and SHOULD be HTTPS.
	RedirectURI string `mapstructure:"redirect_uri"`

	// Scopes is the list of OAuth 2.0 / OIDC scopes to request.
	// "openid" is required; "profile", "email", and "groups" are
	// common additions.
	Scopes []string `mapstructure:"scopes"`

	// GroupsClaimPath is a dotted path into the ID-token claims that
	// resolves to an array of group names (e.g.
	// "resource_access.proxiport.roles"). When empty, the top-level
	// "groups" claim is used.
	GroupsClaimPath string `mapstructure:"groups_claim_path"`

	// AllowedGroups, when non-empty, restricts login to principals
	// whose Groups intersect this list. An empty AllowedGroups means
	// "any successfully authenticated user is allowed".
	AllowedGroups []string `mapstructure:"allowed_groups"`

	// UsernameClaim is the claim used to derive a stable ProxiPort
	// username. Defaults to "preferred_username" when empty, falling
	// back to "sub".
	UsernameClaim string `mapstructure:"username_claim"`
}

// Validate is a no-op while the package is scaffolding. A real
// implementation will reject incomplete configs; see docs/oidc-design.md.
func (c *Config) Validate() error {
	// TODO(v0.2): enforce required fields and URL shape.
	return nil
}
