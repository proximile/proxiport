package chserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/chconfig"
	"github.com/proximile/proxiport/server/clients"
	"github.com/proximile/proxiport/server/clients/clientdata"
)

func TestPlausibleAgentStartedAt(t *testing.T) {
	dispatchedAt := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	now := dispatchedAt.Add(200 * time.Millisecond)

	testCases := []struct {
		name       string
		reportedAt time.Time
		wantUsed   bool
	}{
		{"inside the round trip", dispatchedAt.Add(100 * time.Millisecond), true},
		{"a little behind the server", dispatchedAt.Add(-30 * time.Second), true},
		{"a little ahead of the server", now.Add(30 * time.Second), true},
		{"at the edge of the allowance", now.Add(maxAgentStartedAtSkew), true},
		{"far ahead of the server", now.AddDate(1000, 0, 0), false},
		{"far behind the server", dispatchedAt.AddDate(-6, 0, 0), false},
		{"not reported at all", time.Time{}, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got, used := plausibleAgentStartedAt(dispatchedAt, now, tc.reportedAt)

			assert.Equal(t, tc.wantUsed, used)
			if tc.wantUsed {
				assert.Equal(t, tc.reportedAt, got)
			} else {
				assert.Equal(t, dispatchedAt, got, "the server's own dispatch time is the fallback")
			}
		})
	}
}

// An agent answers run-cmd with a start time on its own clock, and the server
// stored it verbatim. Downstream that value is subtracted from the server's
// clock, so a future-dated one is not merely wrong, it is wrong in a direction
// that reads as "this job is still running" forever.
func TestACommandJobDoesNotTakeAnImplausibleStartTimeFromItsAgent(t *testing.T) {
	connMock := makeConnMock(t, 7, time.Date(2999, 1, 1, 0, 0, 0, 0, time.UTC))
	c1 := clients.New(t).ID("client-1").Connection(connMock).Logger(testLog).Build()

	al := APIListener{
		insecureForTests: true,
		Logger:           testLog,
		Server: &Server{
			clientService: clients.NewClientService(nil, nil,
				clients.NewClientRepository([]*clientdata.Client{c1}, &hour, testLog), testLog, nil),
			config: &chconfig.Config{
				Server: chconfig.ServerConfig{RunRemoteCmdTimeoutSec: 60},
				API:    chconfig.APIConfig{MaxRequestBytes: 1024 * 1024},
			},
		},
	}
	al.initRouter()

	jp := NewJobProviderMock()
	al.jobProvider = jp

	before := time.Now()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/clients/client-1/commands",
		strings.NewReader(`{"command": "/bin/date", "timeout_sec": 30}`))
	w := httptest.NewRecorder()
	al.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	stored := jp.InputCreateJob
	require.NotNil(t, stored)
	assert.False(t, stored.StartedAt.After(time.Now()),
		"a job may not be recorded as starting in the future; got %s", stored.StartedAt)
	assert.False(t, stored.StartedAt.Before(before),
		"the server's own dispatch time should have been kept; got %s", stored.StartedAt)
}
