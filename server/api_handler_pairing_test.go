package chserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/chconfig"
)

func newPairingListener(t *testing.T, pairingURL string) *APIListener {
	t.Helper()
	cfg := &chconfig.Config{}
	cfg.Server.PairingURL = pairingURL
	al := &APIListener{
		Server: &Server{config: cfg},
	}
	al.Logger = testLog
	return al
}

// The server must forward the deposit verbatim to the pairing service and wrap
// the pairing service's answer in the standard {data:...} envelope.
func TestHandlePostPairing_ForwardsAndWrapsResponse(t *testing.T) {
	var gotBody pairingDepositRequest
	var gotCT string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"pairing_code":"AbC1234","expires":"2026-08-15T23:00:00Z","installers":{"linux":"curl ... | sh","windows":"iwr ..."}}`)
	}))
	defer upstream.Close()

	al := newPairingListener(t, upstream.URL)

	reqBody := `{"connect_url":"https://port.example.com:443","fingerprint":"aa:bb","client_id":"agent-1","password":"s3cret-passphrase"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pairing", strings.NewReader(reqBody))
	w := httptest.NewRecorder()

	al.handlePostPairing(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Contains(t, gotCT, "application/json")

	// deposit forwarded verbatim
	assert.Equal(t, "agent-1", gotBody.ClientID)
	assert.Equal(t, "s3cret-passphrase", gotBody.Password)
	assert.Equal(t, "https://port.example.com:443", gotBody.ConnectURL)
	assert.Equal(t, "aa:bb", gotBody.Fingerprint)

	// response is wrapped in the standard {data:...} envelope
	var env struct {
		Data pairingDepositResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &env))
	assert.Equal(t, "AbC1234", env.Data.PairingCode)
	assert.Equal(t, "curl ... | sh", env.Data.Installers.Linux)
	assert.Equal(t, "iwr ...", env.Data.Installers.Windows)
}

// With no pairing service configured there is nothing to broker.
func TestHandlePostPairing_DisabledWhenNoURL(t *testing.T) {
	al := newPairingListener(t, "")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pairing", strings.NewReader(`{}`))
	w := httptest.NewRecorder()

	al.handlePostPairing(w, req)

	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), ErrCodePairingDisabled)
}

// An upstream rejection is surfaced as 502 with a stable error code, and the
// raw upstream body is not relayed to the browser.
func TestHandlePostPairing_UpstreamErrorMapsToBadGateway(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "input validation failed: secret-ish detail", http.StatusBadRequest)
	}))
	defer upstream.Close()

	al := newPairingListener(t, upstream.URL)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pairing",
		strings.NewReader(`{"client_id":"a","password":"b","connect_url":"c","fingerprint":"d"}`))
	w := httptest.NewRecorder()

	al.handlePostPairing(w, req)

	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), ErrCodePairingUpstream)
	assert.NotContains(t, w.Body.String(), "secret-ish detail")
}

// If the pairing service can't be reached, the operator gets a clear 502.
func TestHandlePostPairing_UnreachableMapsToBadGateway(t *testing.T) {
	// A server we immediately close yields a real URL shape whose port is dead.
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()

	al := newPairingListener(t, deadURL)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pairing",
		strings.NewReader(`{"client_id":"a","password":"b","connect_url":"c","fingerprint":"d"}`))
	w := httptest.NewRecorder()

	al.handlePostPairing(w, req)

	require.Equal(t, http.StatusBadGateway, w.Code, w.Body.String())
}
