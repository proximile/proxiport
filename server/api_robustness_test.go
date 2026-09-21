package chserver

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/monitoring"
	"github.com/proximile/proxiport/share/models"
	"github.com/proximile/proxiport/share/types"
)

// TestTicketQuotaIsPerUserNotJustGlobal is M11. /ws-ticket sits behind
// authentication alone -- no permission gate, no rate limiter anywhere on the
// API -- and a ticket is the only credential a browser can use to open
// /ws/commands, /ws/scripts or /ws/uploads. With one shared ceiling and no
// per-user accounting, an account with zero function permissions could pin the
// store at that ceiling and every other operator's ticket request answered 503.
func TestTicketQuotaIsPerUserNotJustGlobal(t *testing.T) {
	store := newWSTicketStore()

	for i := 0; i < maxTicketsPerUser; i++ {
		_, err := store.issue("greedy")
		require.NoError(t, err, "ticket %d of the per-user allowance", i+1)
	}

	_, err := store.issue("greedy")
	require.ErrorIs(t, err, errTooManyUserTickets)

	// The point of the quota: everyone else is still served.
	for _, user := range []string{"alice", "bob", "carol"} {
		ticket, err := store.issue(user)
		require.NoError(t, err, "%s must still be able to open a websocket", user)
		require.NotEmpty(t, ticket)
	}

	// And the quota is a live count, not a lifetime one: redeeming frees a slot.
	ticket, err := store.issue("alice")
	require.NoError(t, err)
	username, ok := store.redeem(ticket)
	require.True(t, ok)
	require.Equal(t, "alice", username)
}

func TestTicketQuotaFreesSlotsAsTicketsAreRedeemed(t *testing.T) {
	store := newWSTicketStore()

	tickets := make([]string, 0, maxTicketsPerUser)
	for i := 0; i < maxTicketsPerUser; i++ {
		ticket, err := store.issue("operator")
		require.NoError(t, err)
		tickets = append(tickets, ticket)
	}
	_, err := store.issue("operator")
	require.ErrorIs(t, err, errTooManyUserTickets)

	_, ok := store.redeem(tickets[0])
	require.True(t, ok)

	_, err = store.issue("operator")
	assert.NoError(t, err, "a redeemed ticket must give its holder the slot back")
}

// TestDropNonJSONMeasurementBlobs is L3 at the source. A measurement's
// processes and mountpoints are composed by the agent, stored verbatim, and
// spliced back into responses as raw JSON -- so a blob that is not JSON makes
// encoding/json fail on the whole response, and every monitoring request for
// that client answers 500 for as long as the row is in the requested window.
func TestDropNonJSONMeasurementBlobs(t *testing.T) {
	testCases := []struct {
		name            string
		processes       string
		mountpoints     string
		wantProcesses   string
		wantMountpoints string
		wantDiscarded   []string
	}{
		{
			name:            "well-formed blobs are kept",
			processes:       `[{"pid":1,"name":"init"}]`,
			mountpoints:     `{"free_b./":1}`,
			wantProcesses:   `[{"pid":1,"name":"init"}]`,
			wantMountpoints: `{"free_b./":1}`,
		},
		{
			name:          "a bare word is not JSON",
			processes:     "not json",
			wantDiscarded: []string{"processes"},
		},
		{
			name:          "trailing garbage after a valid value is not JSON",
			processes:     `1,"x":2`,
			wantDiscarded: []string{"processes"},
		},
		{
			name:            "the repo's own fixtures are caught too",
			processes:       `{[{"pid":30210, "parent_pid": 4711, "name": "chrome"}]}`,
			mountpoints:     `{"free_b./":34182758400}`,
			wantMountpoints: `{"free_b./":34182758400}`,
			wantDiscarded:   []string{"processes"},
		},
		{
			name:          "both can go at once",
			processes:     "nope",
			mountpoints:   "also nope",
			wantDiscarded: []string{"processes", "mountpoints"},
		},
		{
			name: "empty stays empty and is not reported",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			measurement := models.Measurement{Processes: tc.processes, Mountpoints: tc.mountpoints}
			discarded := dropNonJSONMeasurementBlobs(&measurement)

			assert.Equal(t, tc.wantProcesses, measurement.Processes)
			assert.Equal(t, tc.wantMountpoints, measurement.Mountpoints)
			assert.Equal(t, tc.wantDiscarded, discarded)
		})
	}
}

// TestMonitoringResponseSurvivesAPoisonedRow is the same finding from the read
// side: rows written before the ingest check exists are already on disk, and a
// response carrying one must still be a response.
func TestMonitoringResponseSurvivesAPoisonedRow(t *testing.T) {
	payload := []*monitoring.ClientProcessesPayload{
		{Processes: types.JSONString(`[{"pid":1,"name":"init"}]`)},
		{Processes: types.JSONString("not json")},
	}

	encoded, err := json.Marshal(payload)
	require.NoError(t, err, "one unrepresentable row must not fail the whole response")

	var decoded []map[string]interface{}
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Len(t, decoded, 2)
	assert.NotNil(t, decoded[0]["processes"], "the good row must survive intact")
	assert.Nil(t, decoded[1]["processes"], "the unrepresentable row becomes null")
}
