// TODO(v0.2): Remove disabledProvider from the default constructor
// once a real provider exists. It will remain here as the explicit
// off-state for tests and for operators who want to assert "OIDC is
// definitely not on".

package oidc

import "context"

// disabledProvider is the zero-config Provider. Every method returns
// ErrDisabled. It exists so callers can always rely on a non-nil
// Provider without nil-checking.
type disabledProvider struct{}

// Name returns the constant identifier "disabled".
func (disabledProvider) Name() string { return "disabled" }

// AuthCodeURL always returns ErrDisabled.
func (disabledProvider) AuthCodeURL(state, nonce string) (string, error) {
	return "", ErrDisabled
}

// Exchange always returns ErrDisabled.
func (disabledProvider) Exchange(ctx context.Context, code, codeVerifier string) (*Token, error) {
	return nil, ErrDisabled
}

// UserInfo always returns ErrDisabled.
func (disabledProvider) UserInfo(ctx context.Context, accessToken string) (*UserInfo, error) {
	return nil, ErrDisabled
}
