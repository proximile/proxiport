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
// allowDirectProbe reports whether the candidate that just failed reached the
// server over the open internet. When it did not, the transparent-proxy probe
// is skipped: the probe deliberately dials the server straight from the local
// stack, which on a proxied candidate would be an unproxied request to the
// server address — the exact disclosure the transport chain exists to prevent.
// Its finding would also be meaningless, since it would describe a path the
// agent is not using.
func ConnectionErrorHints(server string, logger *logger.Logger, err error, allowDirectProbe bool) error {
	switch err.Error() {
	case "Access violation":
		return fmt.Errorf("%s - Check your proxy allows the CONNECT method to the proxiport server port", err)
	case "Proxy Authentication Required":
		return fmt.Errorf("%s - Check the proxy username and password in proxiport.conf or add if missing", err)
	case "websocket: bad handshake":
		if !allowDirectProbe {
			return fmt.Errorf("%s - Server maybe busy, or the transport in use cannot reach it. Check your client credentials", err)
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
		return fmt.Errorf("%s - Server maybe busy. Also check your client credentials AND check for tranparent proxies", err)
	default:
		return err
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
