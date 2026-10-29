package chclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/proximile/proxiport/share/logger"
)

// ConnectionErrorHints turns a raw dial failure into an operator-facing hint.
//
// resp is the HTTP response to the WebSocket upgrade, when there was one. A
// refused upgrade carries the reason in its status code, so the hint names
// that reason instead of listing every possible one.
//
// allowDirectProbe reports whether the candidate that just failed reached the
// server over the open internet. When it did not, the transparent-proxy probe
// is skipped: the probe deliberately dials the server straight from the local
// stack, which on a proxied candidate would be an unproxied request to the
// server address — the exact disclosure the transport chain exists to prevent.
// Its finding would also be meaningless, since it would describe a path the
// agent is not using.
func ConnectionErrorHints(server string, logger *logger.Logger, err error, resp *http.Response, allowDirectProbe bool) error {
	switch err.Error() {
	case "Access violation":
		return fmt.Errorf("%s - Check your proxy allows the CONNECT method to the proxiport server port", err)
	case "Proxy Authentication Required":
		return fmt.Errorf("%s - Check the proxy username and password in proxiport.conf or add if missing", err)
	case "websocket: bad handshake":
		if hint, known := badHandshakeStatusHint(resp); known {
			return fmt.Errorf("%s (HTTP %d) - %s", err, resp.StatusCode, hint)
		}
		status := ""
		if resp != nil {
			status = fmt.Sprintf(" (HTTP %d)", resp.StatusCode)
		}
		if !allowDirectProbe {
			return fmt.Errorf("%s%s - The address answered but did not accept the agent connection. Check that 'server' names the server's agent listener, not its API or web UI address", err, status)
		}
		proxy, allHeaders, checkErr := DetectTransparentProxy(server)
		if checkErr != nil {
			logger.Errorf("error detecting proxy: %s", checkErr)
		}
		if proxy != "" {
			logger.Errorf(proxy)
		}
		if allHeaders != "" {
			logger.Debugf("headers collected while detecting proxy: %s", allHeaders)
		}
		return fmt.Errorf("%s%s - The address answered but did not accept the agent connection. Check that 'server' names the server's agent listener, not its API or web UI address, and check for transparent proxies", err, status)
	default:
		return err
	}
}

// badHandshakeStatusHint explains the status codes the server's agent listener
// uses to refuse an upgrade on purpose. Any other status, or none, means the
// request most likely reached something other than that listener.
func badHandshakeStatusHint(resp *http.Response) (string, bool) {
	if resp == nil {
		return "", false
	}
	switch resp.StatusCode {
	case http.StatusLocked:
		return "This address is temporarily banned after repeated failed logins. Check the credential in 'auth', then wait for the ban to expire", true
	case http.StatusServiceUnavailable:
		return "The server is busy with other connection attempts; the agent retries", true
	default:
		return "", false
	}
}

func DetectTransparentProxy(serverURL string) (proxy string, allHeaders string, error error) {
	var client = &http.Client{
		Timeout: time.Second * 10,
	}
	res, err := client.Head(strings.Replace(serverURL, "ws", "http", 1))
	if err != nil {
		return "", "", fmt.Errorf("error on http HEAD request: %s", err)
	}
	proxy = res.Header.Get("Via")
	reqHeadersBytes, err := json.Marshal(res.Header)
	if err != nil {
		return "", "", fmt.Errorf("error on getting headers from http HEAD request: %s", err)
	}
	allHeaders = string(reqHeadersBytes)

	if proxy != "" {
		return fmt.Sprintf("A transparent proxy '%s' seems to interfere your connection", proxy), allHeaders, nil
	}
	return "", allHeaders, nil
}
