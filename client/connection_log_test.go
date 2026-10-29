package chclient

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/client/transport"
)

func TestSweepFailureIsOneLineAndUnwraps(t *testing.T) {
	err := sweepFailure{
		errors.New("via socks5h://127.0.0.1:1 to ws://host01:80: connection refused"),
		context.Canceled,
	}

	assert.NotContains(t, err.Error(), "\n", "every reason must stay in the one log record")
	assert.Equal(t, "via socks5h://127.0.0.1:1 to ws://host01:80: connection refused; context canceled", err.Error())
	assert.ErrorIs(t, err, context.Canceled)
}

func TestEndpointLabel(t *testing.T) {
	direct, err := transport.ParseSpec("direct")
	require.NoError(t, err)
	proxied, err := transport.ParseSpec("socks5h://user:secret-pass@127.0.0.1:1080")
	require.NoError(t, err)

	assert.Equal(t, "ws://host01:80", endpointLabel(candidate{Transport: direct, Server: "ws://host01:80"}))

	label := endpointLabel(candidate{Transport: proxied, Server: "ws://host01:80"})
	assert.Equal(t, "ws://host01:80 via socks5h://user:xxxxx@127.0.0.1:1080", label)
	assert.NotContains(t, label, "secret-pass")
}

func TestBadHandshakeHintNamesTheRefusal(t *testing.T) {
	cases := []struct {
		name      string
		resp      *http.Response
		want      string
		notWanted string
	}{
		{
			name: "banned address",
			resp: &http.Response{StatusCode: http.StatusLocked},
			want: "(HTTP 423) - This address is temporarily banned after repeated failed logins",
		},
		{
			name: "busy server",
			resp: &http.Response{StatusCode: http.StatusServiceUnavailable},
			want: "(HTTP 503) - The server is busy",
		},
		{
			name:      "not the agent listener",
			resp:      &http.Response{StatusCode: http.StatusNotFound},
			want:      "(HTTP 404) - The address answered but did not accept the agent connection",
			notWanted: "credential",
		},
		{
			name:      "no response",
			want:      "websocket: bad handshake - The address answered but did not accept the agent connection",
			notWanted: "credential",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// allowDirectProbe=false: the hint must not dial anything itself.
			got := ConnectionErrorHints("ws://host01:80", testLog, websocket.ErrBadHandshake, tc.resp, false).Error()
			assert.Contains(t, got, tc.want)
			if tc.notWanted != "" {
				assert.NotContains(t, strings.ToLower(got), tc.notWanted)
			}
		})
	}
}
