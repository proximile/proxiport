package chserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/proximile/proxiport/server/api"
)

const (
	// ErrCodePairingDisabled is returned when no pairing service is configured
	// (server.pairing_url is empty), so there is nothing to forward a deposit to.
	ErrCodePairingDisabled = "ERR_CODE_PAIRING_DISABLED"
	// ErrCodePairingUpstream is returned when the pairing service is reachable
	// but rejects the deposit or answers with something unexpected.
	ErrCodePairingUpstream = "ERR_CODE_PAIRING_UPSTREAM"

	pairingProxyTimeout     = 15 * time.Second
	maxPairingResponseBytes = 64 << 10 // a pairing code plus two short installer strings
)

// pairingProxyClient performs the server-to-server deposit. It is kept separate
// from http.DefaultClient so its timeout is fixed and not mutable elsewhere.
var pairingProxyClient = &http.Client{Timeout: pairingProxyTimeout}

// pairingDepositRequest is the deposit the SPA sends us. It mirrors the pairing
// service's deposit body; the fields are relayed verbatim.
type pairingDepositRequest struct {
	ConnectURL  string `json:"connect_url"`
	Fingerprint string `json:"fingerprint"`
	ClientID    string `json:"client_id"`
	Password    string `json:"password"`
}

// pairingDepositResponse is the pairing service's answer: a short-lived code
// plus ready-to-paste installer one-liners.
type pairingDepositResponse struct {
	PairingCode string `json:"pairing_code"`
	Expires     string `json:"expires"`
	Installers  struct {
		Linux   string `json:"linux"`
		Windows string `json:"windows"`
	} `json:"installers"`
}

// handlePostPairing brokers a pairing deposit on the browser's behalf.
//
// The pairing service is a shared, first-party service on its own origin, so a
// browser served from this deployment's origin cannot POST to it directly
// without the pairing service opening CORS to every possible deployment
// hostname. Instead the SPA POSTs the deposit here (same origin, already
// authenticated as an admin) and we forward it to server.pairing_url
// server-to-server, where the browser same-origin policy does not apply, then
// return the pairing code and installer commands to the browser.
func (al *APIListener) handlePostPairing(w http.ResponseWriter, req *http.Request) {
	pairingURL := strings.TrimRight(al.config.Server.PairingURL, "/")
	if pairingURL == "" {
		al.jsonErrorResponseWithDetail(w, http.StatusConflict, ErrCodePairingDisabled,
			"Pairing service is not configured.",
			"Set server.pairing_url to enable one-line agent installers.")
		return
	}

	var depReq pairingDepositRequest
	if err := parseRequestBody(req.Body, &depReq); err != nil {
		al.jsonError(w, err)
		return
	}

	// #nosec G117 -- the deposit deliberately carries the freshly-created agent
	// credential to the pairing service; relaying it is the purpose of a deposit.
	payload, err := json.Marshal(depReq)
	if err != nil {
		al.jsonErrorResponse(w, http.StatusInternalServerError, err)
		return
	}

	// Tie the outbound call to the browser request so it is canceled if the
	// operator navigates away; pairingProxyClient.Timeout bounds its duration.
	// #nosec G107 -- pairingURL is server.pairing_url from the operator's config,
	// validated as an http/https URL at load; it is not attacker-controlled input.
	upstreamReq, err := http.NewRequestWithContext(req.Context(), http.MethodPost, pairingURL+"/", bytes.NewReader(payload))
	if err != nil {
		al.jsonErrorResponse(w, http.StatusInternalServerError, err)
		return
	}
	upstreamReq.Header.Set("Content-Type", "application/json")
	// When the pairing service is configured to require a shared secret on
	// deposits (its deposit_auth_token), present it. Default is empty on both
	// sides, so this is a no-op unless the operator opts in.
	if token := al.config.Server.PairingAuthToken; token != "" {
		upstreamReq.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := pairingProxyClient.Do(upstreamReq)
	if err != nil {
		al.Errorf("pairing deposit to %q failed: %s", pairingURL, err)
		al.jsonErrorResponseWithError(w, http.StatusBadGateway,
			"Could not reach the pairing service.", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPairingResponseBytes))
	if err != nil {
		al.jsonErrorResponseWithError(w, http.StatusBadGateway,
			"Could not read the pairing service response.", err)
		return
	}

	if resp.StatusCode != http.StatusOK {
		// Log the upstream body for the operator, but don't relay it verbatim:
		// it may echo validation text about the submitted credential.
		al.Errorf("pairing service %q returned %d: %s", pairingURL, resp.StatusCode, strings.TrimSpace(string(body)))
		al.jsonErrorResponseWithDetail(w, http.StatusBadGateway, ErrCodePairingUpstream,
			"The pairing service rejected the request.",
			fmt.Sprintf("Upstream returned HTTP %d.", resp.StatusCode))
		return
	}

	var depResp pairingDepositResponse
	if err := json.Unmarshal(body, &depResp); err != nil {
		al.jsonErrorResponseWithError(w, http.StatusBadGateway,
			"The pairing service returned an unexpected response.", err)
		return
	}

	al.writeJSONResponse(w, http.StatusOK, api.NewSuccessPayload(depResp))
}
