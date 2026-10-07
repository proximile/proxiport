package transport

import (
	"net"
	"net/http"
	"net/url"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSpec(t *testing.T) {
	testCases := []struct {
		Name          string
		Spec          string
		ExpectedKind  Kind
		ExpectedError string
	}{
		{Name: "direct", Spec: "direct", ExpectedKind: KindDirect},
		{Name: "socks5h", Spec: "socks5h://127.0.0.1:9050", ExpectedKind: KindSOCKS5},
		{Name: "socks", Spec: "socks://127.0.0.1:9050", ExpectedKind: KindSOCKS5},
		// socks5:// used to be rejected at dial time with "unsupported socks
		// proxy type", which is the spelling operators actually reach for.
		{Name: "socks5 alias accepted", Spec: "socks5://127.0.0.1:9050", ExpectedKind: KindSOCKS5},
		{Name: "http connect", Spec: "http://127.0.0.1:8888", ExpectedKind: KindConnect},
		{Name: "with credentials", Spec: "socks5h://user:pass@gluetun:1080", ExpectedKind: KindSOCKS5},
		{
			Name:          "https rejected",
			Spec:          "https://proxy.example.com:443",
			ExpectedError: `transport "https://proxy.example.com:443": https proxies are not supported`,
		},
		{
			Name:          "unknown scheme rejected",
			Spec:          "wireguard://vpn0",
			ExpectedError: `transport "wireguard://vpn0": unsupported scheme "wireguard" (want socks5h://, socks://, socks5:// or http://)`,
		},
		{
			Name:          "bare word is not silently skipped",
			Spec:          "tor",
			ExpectedError: `unknown transport "tor": expected "direct" or a proxy URL (socks5h://, socks://, socks5://, http://)`,
		},
		{
			Name:          "typo of direct is an error, not a skip",
			Spec:          "dircet",
			ExpectedError: `unknown transport "dircet": expected "direct" or a proxy URL (socks5h://, socks://, socks5://, http://)`,
		},
		{
			Name:          "no host",
			Spec:          "socks5h://",
			ExpectedError: `transport "socks5h://" has no host`,
		},
		{Name: "empty", Spec: "   ", ExpectedError: "empty transport"},
	}

	for _, tc := range testCases {
		t.Run(tc.Name, func(t *testing.T) {
			got, err := ParseSpec(tc.Spec)
			if tc.ExpectedError != "" {
				require.Error(t, err)
				assert.Equal(t, tc.ExpectedError, err.Error())
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.ExpectedKind, got.Kind)
		})
	}
}

func TestBuildChain(t *testing.T) {
	t.Run("orders as written", func(t *testing.T) {
		chain, err := BuildChain([]string{"socks5h://127.0.0.1:9050", "http://127.0.0.1:8888", "direct"})
		require.NoError(t, err)
		require.Len(t, chain, 3)
		assert.Equal(t, KindSOCKS5, chain[0].Kind)
		assert.Equal(t, KindConnect, chain[1].Kind)
		assert.True(t, chain[2].IsDirect())
		assert.True(t, chain.HasDirect())
	})

	t.Run("omitting direct means the chain cannot reach the clear internet", func(t *testing.T) {
		chain, err := BuildChain([]string{"socks5h://127.0.0.1:9050", "socks5h://gluetun:1080"})
		require.NoError(t, err)
		assert.False(t, chain.HasDirect(), "a chain without the word must never permit a direct dial")
	})

	t.Run("direct must be last", func(t *testing.T) {
		_, err := BuildChain([]string{"direct", "socks5h://127.0.0.1:9050"})
		require.Error(t, err)
		assert.Equal(t,
			`transports[0]: "direct" must be the last entry — entries after it (transports[1] = "socks5h://127.0.0.1:9050") can never be reached`,
			err.Error())
	})

	t.Run("one bad entry fails the whole chain", func(t *testing.T) {
		// Never a skip: silently dropping an entry would shorten the policy the
		// operator wrote.
		_, err := BuildChain([]string{"socks5h://127.0.0.1:9050", "sock5h://typo:9050", "direct"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "transports[1]")
	})

	t.Run("empty list rejected", func(t *testing.T) {
		_, err := BuildChain(nil)
		require.Error(t, err)
	})
}

// TestLabelMasksPassword guards the rule that no proxy credential reaches a log
// line. url.URL.String() embeds userinfo verbatim, and the shipped example
// config documents proxies with credentials in the URL.
func TestLabelMasksPassword(t *testing.T) {
	tr, err := ParseSpec("socks5h://alice:hunter2@gluetun:1080")
	require.NoError(t, err)

	label := tr.Label()
	assert.NotContains(t, label, "hunter2", "the proxy password must never appear in a label")
	assert.Contains(t, label, "alice", "the username is kept so the operator can tell which credential was used")
	assert.Contains(t, tr.URL.String(), "hunter2", "sanity: url.String() really does leak it, which is why Label exists")

	assert.Equal(t, DirectSpec, Transport{Kind: KindDirect}.Label())
}

func TestApply(t *testing.T) {
	netDialer := &net.Dialer{}

	t.Run("direct clears both proxy mechanisms", func(t *testing.T) {
		d := &websocket.Dialer{}
		d.Proxy = func(*http.Request) (*url.URL, error) { return nil, nil }
		require.NoError(t, Transport{Kind: KindDirect}.Apply(d, netDialer))
		assert.Nil(t, d.Proxy, "a stale CONNECT proxy must not survive onto a direct candidate")
		assert.NotNil(t, d.NetDialContext)
	})

	t.Run("socks sets the dialer and leaves Proxy nil", func(t *testing.T) {
		tr, err := ParseSpec("socks5h://127.0.0.1:9050")
		require.NoError(t, err)
		d := &websocket.Dialer{}
		d.Proxy = func(*http.Request) (*url.URL, error) { return nil, nil }
		require.NoError(t, tr.Apply(d, netDialer))
		// gorilla layers d.Proxy above NetDialContext, so leaving it set would
		// stack a CONNECT proxy on top of the SOCKS dialer instead of replacing
		// it.
		assert.Nil(t, d.Proxy, "d.Proxy must be cleared on the SOCKS path")
		assert.NotNil(t, d.NetDialContext)
	})

	t.Run("connect sets Proxy to the configured URL", func(t *testing.T) {
		tr, err := ParseSpec("http://127.0.0.1:8888")
		require.NoError(t, err)
		d := &websocket.Dialer{}
		require.NoError(t, tr.Apply(d, netDialer))
		require.NotNil(t, d.Proxy)
		got, err := d.Proxy(nil)
		require.NoError(t, err)
		assert.Equal(t, "http://127.0.0.1:8888", got.String())
	})
}
