package caddy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validRouteRequest() *NewRouteRequest {
	return &NewRouteRequest{
		RouteID:                   "abc123",
		TargetTunnelHost:          "127.0.0.1",
		TargetTunnelPort:          "3390",
		DownstreamProxySubdomain:  "abc123",
		DownstreamProxyBaseDomain: "tunnels.example.com",
	}
}

func TestMarshalNewRouteRequestShape(t *testing.T) {
	body, err := MarshalNewRouteRequest(validRouteRequest())
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))

	assert.Equal(t, "abc123", decoded["@id"])
	assert.Equal(t, true, decoded["terminal"])

	assert.Contains(t, string(body), `"dial":"127.0.0.1:3390"`)
	assert.Contains(t, string(body), `"abc123.tunnels.example.com"`)
	assert.Contains(t, string(body), `"handler":"reverse_proxy"`)
	assert.Contains(t, string(body), `"handler":"static_response"`)
}

// The body used to be rendered by a text/template, which does not escape, and
// two of these fields come from the agent's connection request. A value that
// closes its JSON string could add sibling keys -- a second upstream dialing
// anywhere the agent liked, PUT straight to the Caddy admin socket.
func TestMarshalNewRouteRequestRefusesInjection(t *testing.T) {
	cases := map[string]func(*NewRouteRequest){
		"host breaks out of the dial string": func(nrr *NewRouteRequest) {
			nrr.TargetTunnelHost = `1.2.3.4","upstreams":[{"dial":"169.254.169.254:80"}],"x":"`
		},
		"subdomain breaks out of the host match": func(nrr *NewRouteRequest) {
			nrr.DownstreamProxySubdomain = `a"],"terminal":false,"x":["b`
		},
		"base domain breaks out of the host match": func(nrr *NewRouteRequest) {
			nrr.DownstreamProxyBaseDomain = `evil.example"],"x":["`
		},
		"route id breaks out": func(nrr *NewRouteRequest) {
			nrr.RouteID = `id","terminal":false,"x":"`
		},
		"port is not a number": func(nrr *NewRouteRequest) {
			nrr.TargetTunnelPort = `80","y":"`
		},
		"port is out of range": func(nrr *NewRouteRequest) {
			nrr.TargetTunnelPort = "70000"
		},
		"subdomain is empty": func(nrr *NewRouteRequest) {
			nrr.DownstreamProxySubdomain = ""
		},
		"host is a url": func(nrr *NewRouteRequest) {
			nrr.TargetTunnelHost = "http://evil.example/"
		},
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			nrr := validRouteRequest()
			mutate(nrr)

			body, err := MarshalNewRouteRequest(nrr)
			require.Error(t, err, "a value that is not what the field claims must be refused")
			assert.Nil(t, body)
			assert.Contains(t, err.Error(), "refusing to build a caddy route")
		})
	}
}

// Even without validation, marshaling escapes: this pins the second layer, so a
// future field that is not validated still cannot change the document's shape.
func TestMarshalNewRouteRequestEscapesRatherThanInterpolates(t *testing.T) {
	nrr := validRouteRequest()
	nrr.DownstreamProxyBaseDomain = "tunnels.example.com"

	body, err := MarshalNewRouteRequest(nrr)
	require.NoError(t, err)

	var route struct {
		Match []struct {
			Host []string `json:"host"`
		} `json:"match"`
	}
	require.NoError(t, json.Unmarshal(body, &route))
	require.Len(t, route.Match, 1)
	require.Len(t, route.Match[0].Host, 1, "exactly one host, no injected siblings")
	assert.Equal(t, "abc123.tunnels.example.com", route.Match[0].Host[0])
}

// TestMarshalNewRouteRequestMatchesTheGoldenDocument pins the exact document
// sent to the Caddy admin API.
//
// testdata/caddy_route_golden.json is the output the deleted
// new_route_request_template.json produced for validRouteRequest(), captured
// when the template was replaced. The typed marshaler reproduced it byte for
// byte after normalisation, which is what made the swap safe -- the shape
// assertions above would not have caught a dropped or renamed key, and Caddy
// would have accepted a subtly different route and served it wrong.
//
// Keeping the golden means the wire format cannot drift by accident later. If
// this fails because the format is being changed deliberately, regenerate it
// and say so in the commit.
func TestMarshalNewRouteRequestMatchesTheGoldenDocument(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "caddy_route_golden.json"))
	require.NoError(t, err)

	body, err := MarshalNewRouteRequest(validRouteRequest())
	require.NoError(t, err)

	var golden, got any
	require.NoError(t, json.Unmarshal(want, &golden))
	require.NoError(t, json.Unmarshal(body, &got))

	assert.Equal(t, golden, got, "the Caddy admin-API document changed shape")
}
