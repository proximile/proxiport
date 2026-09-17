package clienttunnel

import (
	_ "embed" //to embed novnc wrapper templates
	"net"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"github.com/proximile/proxiport/server/api/middleware"
)

//go:embed novnc/index.html
var indexHTML string

//go:embed novnc/error404.html
var error404HTML string

// TunnelProxyConnectorVNC is a kind of 'websockify' vnc to tcp proxy to be used by a novnc instance to connect to a vnc tunnel
type TunnelProxyConnectorVNC struct {
	tunnelProxy *InternalTunnelProxy
}

func NewTunnelConnectorVNC(tp *InternalTunnelProxy) *TunnelProxyConnectorVNC {
	return &TunnelProxyConnectorVNC{tunnelProxy: tp}
}

// InitRouter called when tunnel proxy is started
func (tc *TunnelProxyConnectorVNC) InitRouter(router *mux.Router) *mux.Router {
	router.Use(tc.tunnelProxy.noCache)

	router.HandleFunc("/vnc", tc.serveVNC)

	router.HandleFunc("/", tc.serveIndex)
	router.HandleFunc("/error404", tc.serveError404)

	// Serve the noVNC javascript app from the local filesystem -- but only
	// when the operator actually configured where it lives.
	//
	// novnc_root is commented out in the shipped example config, so it
	// defaults to "". http.Dir("") does not serve nothing: it is treated as
	// ".", resolved against the server process's working directory. No shipped
	// unit sets WorkingDirectory=, so systemd's default applies and that
	// directory is "/" -- which turned this route into an unauthenticated
	// read of the entire control-plane filesystem, jwt_secret and every
	// database included.
	//
	// Refusing to mount the handler costs an operator who set novnc_root
	// nothing, and an operator who did not was never going to get a working
	// noVNC console from an empty path anyway.
	if tc.tunnelProxy.Config.NovncRoot == "" {
		tc.tunnelProxy.Logger.Errorf("novnc_root is not configured: the noVNC console is disabled. " +
			"Set [server] novnc_root to the directory holding the noVNC app to enable it.")
		// Plain 404 rather than the styled error page: that page renders with
		// a 200, which is the wrong answer for a route serving nothing.
		router.PathPrefix("/").Handler(http.NotFoundHandler())
		return router
	}

	tc.tunnelProxy.Logger.Infof("serving novnc javascript app from: %s", tc.tunnelProxy.Config.NovncRoot)
	router.PathPrefix("/").Handler(middleware.Handle404(http.FileServer(http.Dir(tc.tunnelProxy.Config.NovncRoot)), http.HandlerFunc(tc.serveError404)))

	return router
}

func (tc *TunnelProxyConnectorVNC) serveIndex(w http.ResponseWriter, r *http.Request) {
	novncParamsMap := map[string]string{
		"resize": "scale",
	}

	templateData := map[string]interface{}{
		"host":            tc.tunnelProxy.Host,
		"port":            tc.tunnelProxy.Port,
		"addr":            tc.tunnelProxy.Addr(),
		"basicUI":         false,
		"noURLPassword":   true,
		"defaultViewOnly": false,
		"params":          novncParamsMap,
	}

	tc.tunnelProxy.serveTemplate(w, r, indexHTML, templateData)
}

func (tc *TunnelProxyConnectorVNC) serveError404(w http.ResponseWriter, r *http.Request) {
	tc.tunnelProxy.serveTemplate(w, r, error404HTML, map[string]interface{}{})
}

func (tc *TunnelProxyConnectorVNC) serveVNC(w http.ResponseWriter, r *http.Request) {
	tc.tunnelProxy.Logger.Infof("TunnelProxyConnectorVNC: connect to tunnel: %s", tc.tunnelProxy.TunnelAddr())

	wsConn, err := Upgrader.Upgrade(w, r, nil)
	if err != nil {
		tc.tunnelProxy.Logger.Errorf("failed to upgrade websocket request: %v", err)
	}

	if err == nil {
		tcpAddr, err := net.ResolveTCPAddr("tcp", tc.tunnelProxy.TunnelAddr())
		if err != nil {
			tc.tunnelProxy.Logger.Errorf("failed to resolve tcp destination: %v", err)
		}

		if err == nil {
			p := new(WebsocketTCPProxy)
			p.Initialize(wsConn, tcpAddr, tc.tunnelProxy.Logger)

			if err = p.Dial(); err != nil {
				tc.tunnelProxy.Logger.Errorf("failed to dial tcp addr: %v", err)
			}

			if err == nil {
				go p.Start()
				return
			}
		}
	}
	_ = wsConn.WriteMessage(websocket.CloseMessage, []byte("could not start websocket tcp proxy"))
}
