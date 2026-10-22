package middleware

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRewrite404(t *testing.T) {
	testCases := []struct {
		Path string
	}{
		{
			Path: "/not_found_path",
		}, {
			Path: "/",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Path, func(t *testing.T) {
			mockHandler := func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/" {
					w.Header().Add("test", "redirect")
					http.NotFound(w, r)
					return
				}
				w.Header().Add("test", "success")
				_, err := w.Write([]byte("success"))
				require.NoError(t, err)
			}
			h := Rewrite404(http.HandlerFunc(mockHandler), "/")

			req, err := http.NewRequest("GET", tc.Path, nil)
			require.NoError(t, err)
			rw := httptest.NewRecorder()

			h.ServeHTTP(rw, req)

			result := rw.Result()
			resultBody, err := io.ReadAll(result.Body)
			require.NoError(t, err)
			expectedHeader := []string{"success"}

			assert.Equal(t, 200, result.StatusCode)
			assert.Equal(t, expectedHeader, result.Header.Values("test"))
			assert.Equal(t, "success", string(resultBody))
		})
	}
}

func TestRewrite404ForVueJs(t *testing.T) {
	testCases := []struct {
		Path           string
		ExpectedStatus int
	}{
		{
			Path:           "/not_found_path",
			ExpectedStatus: 404,
		}, {
			Path:           "/dashboard",
			ExpectedStatus: 200,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.Path, func(t *testing.T) {
			mockHandler := func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/" {
					http.NotFound(w, r)
					return
				}
				_, err := w.Write([]byte("hello world"))
				require.NoError(t, err)
			}
			vueHistoryPaths := []string{"auth", "dashboard"}
			h := Rewrite404ForVueJs(http.HandlerFunc(mockHandler), vueHistoryPaths)

			req, err := http.NewRequest("GET", tc.Path, nil)
			require.NoError(t, err)
			rw := httptest.NewRecorder()
			h.ServeHTTP(rw, req)
			result := rw.Result()
			assert.Equal(t, tc.ExpectedStatus, result.StatusCode)
		})
	}
}

func TestRewrite404ForVueJsKeepsContentEncodingOnUnknownPath(t *testing.T) {
	// Mimics http.FileServer, whose error path removes Content-Encoding.
	fileServer := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			w.Header().Del("Content-Encoding")
			http.NotFound(w, r)
			return
		}
		_, err := w.Write([]byte("index"))
		require.NoError(t, err)
	}
	h := Rewrite404ForVueJs(http.HandlerFunc(fileServer), []string{"inventory"})

	for path, wantStatus := range map[string]int{
		"/no-such-page":  http.StatusNotFound,
		"/inventory/abc": http.StatusOK,
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rw := httptest.NewRecorder()
			// A compressing middleware sets this before the handler runs.
			rw.Header().Set("Content-Encoding", "gzip")

			h.ServeHTTP(rw, req)

			assert.Equal(t, wantStatus, rw.Code)
			assert.Equal(t, "gzip", rw.Header().Get("Content-Encoding"))
		})
	}
}
