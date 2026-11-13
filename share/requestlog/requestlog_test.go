package requestlog

import (
	"bufio"
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func serve(t *testing.T, opts Options, remoteAddr string, h http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/status?access_token=secret", nil)
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	Wrap(h, opts).ServeHTTP(rec, req)
	return rec
}

func TestLogsOneLinePerRequest(t *testing.T) {
	var out bytes.Buffer
	rec := serve(t, Options{Writer: &out}, "203.0.113.7:40000", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(bytes.Repeat([]byte("x"), 2000))
	})

	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Equal(t, 2000, rec.Body.Len())
	assert.Regexp(t, regexp.MustCompile(`^\d{4}/\d\d/\d\d \d\d:\d\d:\d\d\.\d{3} GET /api/v1/status 201 \d+(ns|µs|ms|s) 2KB \(203\.0\.113\.7\)\n$`), out.String())
}

func TestTheQueryStringIsNotLogged(t *testing.T) {
	var out bytes.Buffer
	serve(t, Options{Writer: &out}, "203.0.113.7:40000", func(http.ResponseWriter, *http.Request) {})
	assert.NotContains(t, out.String(), "secret")
	assert.NotContains(t, out.String(), "?")
}

func TestStatusDefaultsTo200AndAnEmptyBodyHasNoSize(t *testing.T) {
	var out bytes.Buffer
	serve(t, Options{Writer: &out}, "203.0.113.7:40000", func(http.ResponseWriter, *http.Request) {})
	assert.Regexp(t, regexp.MustCompile(` GET /api/v1/status 200 \d+(ns|µs|ms|s) \(203\.0\.113\.7\)\n$`), out.String())
}

func TestALoopbackPeerIsNotNamed(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:40000", "[::1]:40000"} {
		var out bytes.Buffer
		serve(t, Options{Writer: &out}, addr, func(http.ResponseWriter, *http.Request) {})
		assert.NotContains(t, out.String(), "(", addr)
	}
}

func TestFilterDecidesWhatIsLogged(t *testing.T) {
	var out bytes.Buffer
	var seenCode int
	var seenSize int64
	opts := Options{Writer: &out, Filter: func(_ *http.Request, code int, _ time.Duration, size int64) bool {
		seenCode, seenSize = code, size
		return false
	}}
	serve(t, opts, "203.0.113.7:40000", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("abc"))
	})
	assert.Empty(t, out.String())
	assert.Equal(t, http.StatusTeapot, seenCode)
	assert.Equal(t, int64(3), seenSize)
}

func TestColorMarksTheStatusCodeOnly(t *testing.T) {
	var plain, colored bytes.Buffer
	h := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }
	serve(t, Options{Writer: &plain}, "203.0.113.7:40000", h)
	serve(t, Options{Writer: &colored, Color: true}, "203.0.113.7:40000", h)
	assert.NotContains(t, plain.String(), "\x1b[")
	assert.Contains(t, colored.String(), yellow+"404"+reset)
}

func TestNoWriterLogsNothingAndStillServes(t *testing.T) {
	rec := serve(t, Options{}, "203.0.113.7:40000", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	})
	assert.Equal(t, http.StatusAccepted, rec.Code)
}

func TestAFileOrBufferIsNotATerminal(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "log")
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	assert.False(t, IsTerminal(f))
	assert.False(t, IsTerminal(&bytes.Buffer{}))
	assert.False(t, IsTerminal(nil))
}

// A WebSocket upgrade hijacks the connection through the logging writer.
func TestAConnectionCanBeHijacked(t *testing.T) {
	var out bytes.Buffer
	srv := httptest.NewServer(Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijacker", http.StatusInternalServerError)
			return
		}
		conn, rw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = rw.WriteString("HTTP/1.1 101 Switching Protocols\r\n\r\nhello")
		_ = rw.Flush()
	}), Options{Writer: &out}))
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()
	_, err = conn.Write([]byte("GET / HTTP/1.1\r\nHost: test\r\n\r\n"))
	require.NoError(t, err)
	status, err := bufio.NewReader(conn).ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "HTTP/1.1 101 Switching Protocols\r\n", status)
}

// The handler flushes through the logging writer (streamed responses).
func TestFlushReachesTheUnderlyingWriter(t *testing.T) {
	rec := serve(t, Options{Writer: &bytes.Buffer{}}, "203.0.113.7:40000", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("part"))
		w.(http.Flusher).Flush()
	})
	assert.True(t, rec.Flushed)
}
