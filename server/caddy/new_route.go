package caddy

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// The Caddy admin API takes JSON, and this body used to be produced by a
// text/template. text/template does not escape, so any value that reached it
// could close its JSON string and add sibling keys -- and two of these values
// originate on the agent, which is inside the threat model. A tunnel host of
//
//	1.2.3.4","upstreams":[{"dial":"169.254.169.254:80"}],"x":"
//
// rendered into "dial": "{{.TargetTunnelHost}}:{{.TargetTunnelPort}}" produced a
// second upstream pointing wherever the agent liked, PUT straight to the admin
// socket.
//
// Marshaling typed values removes the question: encoding/json escapes whatever
// it is given, so no input can change the document's shape. The validation
// below is the second layer -- these fields feed a route's identity and its
// upstream address, and a value that is not a hostname or a port has no
// business reaching Caddy even escaped.

// dnsLabel matches a single DNS label: what a subdomain must be.
var dnsLabel = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// Validate reports whether the route request is made of the kinds of values it
// claims to hold.
func (nrr *NewRouteRequest) Validate() error {
	if !isDNSName(nrr.DownstreamProxySubdomain) {
		return fmt.Errorf("invalid downstream proxy subdomain %q", nrr.DownstreamProxySubdomain)
	}
	if !isDNSName(nrr.DownstreamProxyBaseDomain) {
		return fmt.Errorf("invalid downstream proxy base domain %q", nrr.DownstreamProxyBaseDomain)
	}
	if !isDNSName(nrr.RouteID) {
		return fmt.Errorf("invalid route id %q", nrr.RouteID)
	}
	if net.ParseIP(nrr.TargetTunnelHost) == nil && !isDNSName(nrr.TargetTunnelHost) {
		return fmt.Errorf("invalid target tunnel host %q", nrr.TargetTunnelHost)
	}
	port, err := strconv.Atoi(nrr.TargetTunnelPort)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("invalid target tunnel port %q", nrr.TargetTunnelPort)
	}
	return nil
}

func isDNSName(in string) bool {
	if in == "" || len(in) > 253 {
		return false
	}
	for _, label := range strings.Split(in, ".") {
		if !dnsLabel.MatchString(label) {
			return false
		}
	}
	return true
}

type caddyRoute struct {
	ID       string `json:"@id"`
	Handle   []any  `json:"handle"`
	Match    []any  `json:"match"`
	Terminal bool   `json:"terminal"`
}

type caddySubroute struct {
	Handler string `json:"handler"`
	Routes  []any  `json:"routes"`
}

type caddyInnerRoute struct {
	Handle []any `json:"handle"`
}

type caddyReverseProxy struct {
	Handler   string             `json:"handler"`
	Headers   caddyProxyHeaders  `json:"headers"`
	Transport caddyHTTPTransport `json:"transport"`
	Upstreams []caddyUpstream    `json:"upstreams"`
}

type caddyProxyHeaders struct {
	Request caddyHeaderOps `json:"request"`
}

type caddyHeaderOps struct {
	Set map[string][]string `json:"set"`
}

type caddyHTTPTransport struct {
	Protocol string        `json:"protocol"`
	TLS      caddyTLSConfg `json:"tls"`
}

type caddyTLSConfg struct {
	// The upstream is the server's own tunnel listener on loopback or on the
	// configured bind address, presenting a certificate for whatever the tunnel
	// target is. Verification here would check the wrong name.
	InsecureSkipVerify bool `json:"insecure_skip_verify"`
}

type caddyUpstream struct {
	Dial string `json:"dial"`
}

type caddyStaticResponse struct {
	Body       string `json:"body"`
	Close      bool   `json:"close"`
	Handler    string `json:"handler"`
	StatusCode int    `json:"status_code"`
}

type caddyHostMatch struct {
	Host []string `json:"host"`
}

// MarshalNewRouteRequest builds the Caddy admin-API body for a new downstream
// route, refusing values that are not what the field says they are.
func MarshalNewRouteRequest(nrr *NewRouteRequest) ([]byte, error) {
	if err := nrr.Validate(); err != nil {
		return nil, fmt.Errorf("refusing to build a caddy route: %w", err)
	}

	route := caddyRoute{
		ID: nrr.RouteID,
		Handle: []any{
			caddySubroute{
				Handler: "subroute",
				Routes: []any{
					caddyInnerRoute{
						Handle: []any{
							caddyReverseProxy{
								Handler: "reverse_proxy",
								Headers: caddyProxyHeaders{
									Request: caddyHeaderOps{
										Set: map[string][]string{
											"X-Forwarded-Host": {"{http.request.host}"},
										},
									},
								},
								Transport: caddyHTTPTransport{
									Protocol: "http",
									TLS:      caddyTLSConfg{InsecureSkipVerify: true},
								},
								Upstreams: []caddyUpstream{
									{Dial: net.JoinHostPort(nrr.TargetTunnelHost, nrr.TargetTunnelPort)},
								},
							},
							caddyStaticResponse{
								Body:       "Access denied",
								Close:      true,
								Handler:    "static_response",
								StatusCode: 403,
							},
						},
					},
				},
			},
		},
		Match: []any{
			caddyHostMatch{
				Host: []string{nrr.DownstreamProxySubdomain + "." + nrr.DownstreamProxyBaseDomain},
			},
		},
		Terminal: true,
	}

	return json.Marshal(route)
}
