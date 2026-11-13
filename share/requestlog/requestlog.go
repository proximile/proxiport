// Package requestlog writes one line per HTTP request the server handled:
//
//	2026/01/02 15:04:05.000 GET /api/v1/status 200 3ms 1.2KB (203.0.113.7)
//
// Only the path is logged, never the query string.
package requestlog

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/jpillora/sizestr"
	"golang.org/x/term"
)

func init() {
	// Sizes in the server's log lines are written in upper case ("1.2KB").
	sizestr.UpperCase()
}

const (
	timeFormat = "2006/01/02 15:04:05.000"

	reset  = "\x1b[0m"
	green  = "\x1b[32m"
	cyan   = "\x1b[36m"
	yellow = "\x1b[33m"
)

// Options configure Wrap.
type Options struct {
	// Writer receives the log lines.
	Writer io.Writer
	// Filter, when set, is asked after each request whether to log it.
	Filter func(r *http.Request, code int, duration time.Duration, size int64) bool
	// Color marks the status code with ANSI colors. See IsTerminal.
	Color bool
}

// IsTerminal reports whether w is a terminal, where colored output is wanted.
// It is false for a log file, a pipe, and any writer that is not a file.
func IsTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && f != nil && term.IsTerminal(int(f.Fd()))
}

// Wrap returns a handler that serves with next and then logs the request.
func Wrap(next http.Handler, opts Options) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m := &responseWriter{ResponseWriter: w}
		started := time.Now()
		next.ServeHTTP(m, r)
		m.log(opts, r, started)
	})
}

// responseWriter records the status code and the body size.
type responseWriter struct {
	http.ResponseWriter
	code int
	size int64
}

func (m *responseWriter) Write(p []byte) (int, error) {
	n, err := m.ResponseWriter.Write(p)
	m.size += int64(n)
	return n, err
}

func (m *responseWriter) WriteHeader(code int) {
	if m.code == 0 {
		m.code = code
	}
	m.ResponseWriter.WriteHeader(code)
}

// Hijack lets a WebSocket upgrade take over the connection.
func (m *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := m.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("hijacking not supported")
	}
	return hj.Hijack()
}

func (m *responseWriter) Flush() {
	if f, ok := m.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap gives http.ResponseController access to the underlying writer.
func (m *responseWriter) Unwrap() http.ResponseWriter { return m.ResponseWriter }

var fraction = regexp.MustCompile(`\.\d+`)

func (m *responseWriter) log(opts Options, r *http.Request, started time.Time) {
	if opts.Writer == nil {
		return
	}
	duration := time.Since(started)
	code := m.code
	if code == 0 {
		code = http.StatusOK
	}
	if opts.Filter != nil && !opts.Filter(r, code, duration, m.size) {
		return
	}

	line := started.Format(timeFormat) + " " + r.Method + " " + r.URL.Path + " "
	if opts.Color {
		line += codeColor(code) + fmt.Sprint(code) + reset
	} else {
		line += fmt.Sprint(code)
	}
	// "1.234567ms" is logged as "1ms".
	line += " " + fraction.ReplaceAllString(duration.String(), "")
	if m.size > 0 {
		line += " " + sizestr.ToString(m.size)
	}
	// A loopback peer says nothing about who called.
	if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil && ip != "127.0.0.1" && ip != "::1" {
		line += " (" + ip + ")"
	}
	// One Fprint per line, so concurrent requests do not interleave.
	_, _ = fmt.Fprint(opts.Writer, line+"\n")
}

func codeColor(code int) string {
	switch code / 100 {
	case 2:
		return green
	case 3:
		return cyan
	case 4, 5:
		return yellow
	}
	return reset
}
