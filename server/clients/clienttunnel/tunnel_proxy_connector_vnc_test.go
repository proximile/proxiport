package clienttunnel

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/logger"
)

// novnc_root is commented out in the shipped example config, so it defaults to
// "". http.Dir("") is not an empty file server: Go treats it as ".", resolved
// against the process working directory, and no shipped systemd unit sets
// WorkingDirectory= -- so the default of "/" made this route an
// unauthenticated read of the whole control-plane filesystem.
//
// This asserts the behavior, not the implementation: a request that would
// have walked out to an absolute path must not return the file.
func TestVNCConnectorDoesNotServeTheFilesystemWhenNovncRootIsUnset(t *testing.T) {
	// http.Dir("") resolves against the process working directory, so the test
	// has to control it to demonstrate the read. t.Chdir restores it after.
	// On the real server that directory is "/", because no shipped systemd
	// unit sets WorkingDirectory=.
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "jwt_secret"),
		[]byte("super-secret-signing-key"), 0o600))
	t.Chdir(root)

	tp := &InternalTunnelProxy{
		Config: &InternalTunnelProxyConfig{NovncRoot: ""},
		Logger: logger.NewLogger("vnc-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelError),
	}
	router := (&TunnelProxyConnectorVNC{tunnelProxy: tp}).InitRouter(mux.NewRouter())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/jwt_secret", nil)
	router.ServeHTTP(rec, req)

	assert.NotContains(t, rec.Body.String(), "super-secret-signing-key",
		"an unset novnc_root must not serve files from the server's filesystem")
	assert.NotEqual(t, http.StatusOK, rec.Code,
		"an unset novnc_root must not return 200 for an arbitrary absolute path")
}
