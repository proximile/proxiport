package chserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/chconfig"
	"github.com/proximile/proxiport/server/clientsauth"
	"github.com/proximile/proxiport/server/routes"
	"github.com/proximile/proxiport/share/query"
)

func clientAuthListener(provider clientsauth.Provider) APIListener {
	return APIListener{
		Logger: testLog,
		Server: &Server{
			config: &chconfig.Config{
				API: chconfig.APIConfig{MaxRequestBytes: 1024 * 1024},
			},
			clientAuthProvider: provider,
		},
	}
}

// Reading a credential must not change it.
//
// The read handlers used to redact by assigning Password = "" to the object the
// provider returned. SingleProvider -- what a packaged install runs, because
// the shipped config is a single `auth = "id:password"` line -- returned its one
// live *ClientAuth. So the assignment did not redact a response: it blanked the
// credential the server authenticates agents against, for every agent, until
// the daemon restarted. The SPA's Client Access page issues this exact request
// on load, so merely opening it was enough.
//
// This test goes through the handler on purpose. A test that builds a copy and
// asserts the copy marshals without a password proves only that Go assignment
// works; it cannot see the aliasing, which is the whole bug.
func TestGetClientsAuthDoesNotDestroyTheCredential(t *testing.T) {
	provider := clientsauth.NewSingleProvider("clientAuth1", "s3cret-agent-password")
	al := clientAuthListener(provider)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clients-auth?page[limit]=100", nil)
	w := httptest.NewRecorder()
	http.HandlerFunc(al.handleGetClientsAuth).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	assert.NotContains(t, w.Body.String(), "s3cret-agent-password", "the response must not carry the credential")

	stored, err := provider.Get("clientAuth1")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, "s3cret-agent-password", stored.Password,
		"reading the listing must leave the credential intact")
}

func TestGetClientAuthDoesNotDestroyTheCredential(t *testing.T) {
	provider := clientsauth.NewSingleProvider("clientAuth1", "s3cret-agent-password")
	al := clientAuthListener(provider)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/clients-auth/clientAuth1", nil)
	req = mux.SetURLVars(req, map[string]string{routes.ParamClientAuthID: "clientAuth1"})
	w := httptest.NewRecorder()
	http.HandlerFunc(al.handleGetClientAuth).ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	assert.NotContains(t, w.Body.String(), "s3cret-agent-password")

	stored, err := provider.Get("clientAuth1")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, "s3cret-agent-password", stored.Password)
}

// The root cause, asserted at the provider rather than at the handler: a caller
// that writes to what it received must not be writing into the provider.
func TestSingleProviderHandsOutCopies(t *testing.T) {
	provider := clientsauth.NewSingleProvider("clientAuth1", "s3cret-agent-password")

	got, err := provider.Get("clientAuth1")
	require.NoError(t, err)
	got.Password = ""

	stored, err := provider.Get("clientAuth1")
	require.NoError(t, err)
	assert.Equal(t, "s3cret-agent-password", stored.Password)

	listed, _, err := provider.GetFiltered(&query.ListOptions{
		Pagination: query.NewPagination(10, 0),
	})
	require.NoError(t, err)
	require.Len(t, listed, 1)
	listed[0].Password = ""

	stored, err = provider.Get("clientAuth1")
	require.NoError(t, err)
	assert.Equal(t, "s3cret-agent-password", stored.Password)
}
