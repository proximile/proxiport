// Package oidc provides an OpenID Connect / OAuth 2.0 login provider for
// ProxiPort's HTTP API.
//
// The package is scaffolding only. The default Provider returned by
// New() is a disabled stub that returns ErrDisabled for every call;
// see provider_disabled.go. A real implementation is planned for
// ProxiPort v0.2 and is tracked in docs/oidc-design.md.
//
// The exported surface is deliberately narrow so a future
// implementation can be dropped in without rippling through callers:
//
//	type Provider interface {
//	    Name() string
//	    AuthCodeURL(state, nonce string) (string, error)
//	    Exchange(ctx context.Context, code, codeVerifier string) (*Token, error)
//	    UserInfo(ctx context.Context, accessToken string) (*UserInfo, error)
//	}
//
// Nothing in this package is wired into the HTTP router. Wiring will
// land in a v0.2 commit alongside config-loader changes.
package oidc
