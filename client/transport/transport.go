// Package transport turns the operator's ordered egress list into concrete
// dialers for the agent's single outbound connection to the server.
//
// The agent dials out and carries its whole SSH session — every tunnel, every
// command — over one WebSocket. Whatever carries that one TCP connection
// therefore carries everything, so choosing an egress path is a single
// decision made once per connection attempt.
//
// A chain is an ordered list of transports. It is the operator's anonymity
// policy expressed directly: the agent dials without a proxy if and only if
// the chain contains a Direct transport, so a chain that omits it can never
// reach the server over the open internet, and nothing in this package ever
// synthesizes one.
package transport

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gorilla/websocket"
	"golang.org/x/net/proxy"
)

// DirectSpec is the reserved word that means "dial with no proxy". It is the
// only bare word a chain accepts; every other element must be a proxy URL.
const DirectSpec = "direct"

// Kind is how a transport reaches the server.
type Kind int

const (
	// KindDirect dials the server straight from the local network stack.
	KindDirect Kind = iota
	// KindSOCKS5 dials through a SOCKS5 proxy, which resolves the server name
	// itself. This is what makes a .onion address work.
	KindSOCKS5
	// KindConnect dials through an HTTP CONNECT proxy.
	KindConnect
)

// Transport is one egress path. It is a value, not a resource: building one
// opens nothing, so a chain can be built during config validation and rebuilt
// per connection attempt without leaking anything.
type Transport struct {
	Kind Kind
	// Spec is the operator's literal text, used in errors so a message can be
	// matched against the config file by eye.
	Spec string
	// URL is nil for KindDirect.
	URL *url.URL
}

// IsDirect reports whether this transport reaches the server over the open
// internet with no proxy in front of it.
func (t Transport) IsDirect() bool { return t.Kind == KindDirect }

// Label renders the transport for logs with any proxy password masked.
//
// url.URL.String() embeds userinfo verbatim, and the shipped example config
// documents proxies with credentials in the URL, so String() must never reach
// a log line on this path. Every log statement naming a transport uses Label.
func (t Transport) Label() string {
	if t.URL == nil {
		return DirectSpec
	}
	u := *t.URL
	if u.User != nil {
		if _, hasPassword := u.User.Password(); hasPassword {
			u.User = url.UserPassword(u.User.Username(), "xxxxx")
		}
	}
	return u.String()
}

// Apply configures a websocket dialer to reach the server over this transport.
//
// It always resets both proxy mechanisms first. gorilla layers d.Proxy above
// NetDialContext, so a CONNECT proxy left over from a previous candidate would
// silently stack on top of this one's SOCKS dialer instead of replacing it.
func (t Transport) Apply(d *websocket.Dialer, netDialer *net.Dialer) error {
	d.Proxy = nil
	d.NetDialContext = netDialer.DialContext

	switch t.Kind {
	case KindDirect:
		return nil

	case KindSOCKS5:
		var auth *proxy.Auth
		if t.URL.User != nil {
			password, _ := t.URL.User.Password()
			auth = &proxy.Auth{User: t.URL.User.Username(), Password: password}
		}
		socksDialer, err := proxy.SOCKS5("tcp", t.URL.Host, auth, netDialer)
		if err != nil {
			return fmt.Errorf("transport %s: %w", t.Label(), err)
		}
		contextDialer, ok := socksDialer.(proxy.ContextDialer)
		if !ok {
			return fmt.Errorf("transport %s: socks dialer does not support contexts", t.Label())
		}
		d.NetDialContext = contextDialer.DialContext
		return nil

	case KindConnect:
		proxyURL := t.URL
		d.Proxy = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
		return nil
	}

	return fmt.Errorf("transport %s: unknown kind %d", t.Spec, t.Kind)
}

// ParseSpec turns one chain element into a Transport.
//
// An unrecognized element is always an error and never a skip: silently
// dropping an element would shorten the chain the operator wrote, and a typo
// must never be able to change the policy.
func ParseSpec(spec string) (Transport, error) {
	trimmed := strings.TrimSpace(spec)
	if trimmed == "" {
		return Transport{}, fmt.Errorf("empty transport")
	}
	if trimmed == DirectSpec {
		return Transport{Kind: KindDirect, Spec: trimmed}, nil
	}
	if !strings.Contains(trimmed, "://") {
		return Transport{}, fmt.Errorf(
			"unknown transport %q: expected %q or a proxy URL (socks5h://, socks://, socks5://, http://)",
			trimmed, DirectSpec)
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return Transport{}, fmt.Errorf("invalid transport URL %q: %w", trimmed, err)
	}
	if parsed.Host == "" {
		return Transport{}, fmt.Errorf("transport %q has no host", trimmed)
	}

	switch parsed.Scheme {
	// golang.org/x/net/proxy always sends the destination as a hostname
	// (SOCKS5 ATYP=3) rather than resolving it locally, so all three spellings
	// behave as socks5h. socks5:// is accepted because it is what operators
	// reach for, and rejecting it produced a confusing failure at dial time.
	case "socks", "socks5", "socks5h":
		return Transport{Kind: KindSOCKS5, Spec: trimmed, URL: parsed}, nil
	case "http":
		return Transport{Kind: KindConnect, Spec: trimmed, URL: parsed}, nil
	case "https":
		return Transport{}, fmt.Errorf("transport %q: https proxies are not supported", trimmed)
	default:
		return Transport{}, fmt.Errorf(
			"transport %q: unsupported scheme %q (want socks5h://, socks://, socks5:// or http://)",
			trimmed, parsed.Scheme)
	}
}

// Chain is the ordered list of transports the agent tries.
type Chain []Transport

// BuildChain parses and validates a whole chain.
//
// Direct is required to be last: a direct dial that succeeds ends the sweep, so
// anything after it is unreachable and would misdescribe the policy to the next
// person who reads the config file.
func BuildChain(specs []string) (Chain, error) {
	if len(specs) == 0 {
		return nil, fmt.Errorf("empty transport list")
	}

	chain := make(Chain, 0, len(specs))
	for i, spec := range specs {
		t, err := ParseSpec(spec)
		if err != nil {
			return nil, fmt.Errorf("transports[%d]: %w", i, err)
		}
		if t.IsDirect() && i != len(specs)-1 {
			return nil, fmt.Errorf(
				"transports[%d]: %q must be the last entry — entries after it (transports[%d] = %q) can never be reached",
				i, DirectSpec, i+1, specs[i+1])
		}
		chain = append(chain, t)
	}
	return chain, nil
}

// HasDirect reports whether the chain permits a clearnet dial at all.
func (c Chain) HasDirect() bool {
	for _, t := range c {
		if t.IsDirect() {
			return true
		}
	}
	return false
}

// Labels renders the whole chain for a log line, passwords masked.
func (c Chain) Labels() []string {
	out := make([]string, 0, len(c))
	for _, t := range c {
		out = append(out, t.Label())
	}
	return out
}
