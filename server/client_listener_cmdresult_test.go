package chserver

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/models"
	"github.com/proximile/proxiport/share/ws"
)

func newCmdResultListener(t *testing.T, provider JobProvider) *ClientListener {
	t.Helper()
	return &ClientListener{
		logger: testLog,
		server: &Server{
			jobProvider:     provider,
			uiJobWebSockets: ws.NewWebSocketCache(),
		},
	}
}

// A command result is a message from the agent, and the agent supplies the job
// id in it. The row used to be written with INSERT OR REPLACE keyed on that id,
// so a compromised agent could name any job at all -- another client's row,
// another operator's live job -- and overwrite it.
//
// GetByJID filters on the connection's authenticated client id as well as the
// job id, so a result naming a job this client was not given finds nothing.
func TestSaveCmdResultRejectsAJobTheClientWasNotGiven(t *testing.T) {
	provider := NewJobProviderMock()
	provider.ReturnJob = nil // no row for (this client, this jid)
	listener := newCmdResultListener(t, provider)

	forged, err := json.Marshal(&models.Job{
		JID:      "another-operators-job",
		ClientID: "victim-client",
		Command:  "/usr/bin/whoami",
		Status:   models.JobStatusSuccessful,
		Result:   &models.JobResult{StdOut: "root"},
	})
	require.NoError(t, err)

	job, err := listener.saveCmdResult(forged, "hostile-client")

	require.Error(t, err)
	assert.Nil(t, job)
	assert.Contains(t, err.Error(), "which it was not given")
	assert.Nil(t, provider.InputSaveJob, "nothing may be written for a job the client was not given")

	// The lookup must be scoped to the connection, not to what the agent claimed.
	assert.Equal(t, "hostile-client", provider.InputCID)
	assert.Equal(t, "another-operators-job", provider.InputJID)
}

// For a job the client really was given, the agent is the source of the outcome
// and nothing else. Identity and authorization come from the row the server
// wrote when it dispatched the job.
func TestSaveCmdResultKeepsServerRecordedFields(t *testing.T) {
	multiJobID := "multi-1"
	scheduleID := "schedule-1"
	startedAt := time.Date(2026, 9, 8, 10, 0, 0, 0, time.UTC)

	provider := NewJobProviderMock()
	provider.ReturnJob = &models.Job{
		JID:         "job-1",
		ClientID:    "client-1",
		ClientName:  "agent one",
		Command:     "/usr/bin/uptime",
		Interpreter: "",
		Cwd:         "/tmp",
		CreatedBy:   "operator",
		TimeoutSec:  60,
		MultiJobID:  &multiJobID,
		ScheduleID:  &scheduleID,
		StartedAt:   startedAt,
		IsSudo:      false,
		IsScript:    false,
		Status:      models.JobStatusRunning,
	}
	listener := newCmdResultListener(t, provider)

	finishedAt := startedAt.Add(time.Second)
	otherMulti := "someone-elses-multi-job"
	reported, err := json.Marshal(&models.Job{
		JID:    "job-1",
		Status: models.JobStatusSuccessful,
		Result: &models.JobResult{StdOut: "10:00:01 up 3 days"},

		// All of this is the agent trying to rewrite the record.
		ClientID:    "victim-client",
		ClientName:  "someone else",
		Command:     "/usr/bin/curl http://evil.example",
		Interpreter: "/bin/sh",
		Cwd:         "/root",
		CreatedBy:   "admin",
		IsSudo:      true,
		IsScript:    true,
		TimeoutSec:  1,
		MultiJobID:  &otherMulti,
		StartedAt:   time.Unix(0, 0).UTC(),
		FinishedAt:  &finishedAt,
	})
	require.NoError(t, err)

	job, err := listener.saveCmdResult(reported, "client-1")
	require.NoError(t, err)
	require.NotNil(t, job)

	// From the agent: the outcome.
	assert.Equal(t, models.JobStatusSuccessful, job.Status)
	assert.Equal(t, "10:00:01 up 3 days", job.Result.StdOut)
	assert.Equal(t, &finishedAt, job.FinishedAt)

	// From the server: everything that says whose job this is and what it was.
	assert.Equal(t, "client-1", job.ClientID)
	assert.Equal(t, "agent one", job.ClientName)
	assert.Equal(t, "/usr/bin/uptime", job.Command)
	assert.Equal(t, "", job.Interpreter)
	assert.Equal(t, "/tmp", job.Cwd)
	assert.Equal(t, "operator", job.CreatedBy)
	assert.Equal(t, 60, job.TimeoutSec)
	assert.False(t, job.IsSudo)
	assert.False(t, job.IsScript)
	assert.Equal(t, startedAt, job.StartedAt)
	require.NotNil(t, job.MultiJobID)
	assert.Equal(t, multiJobID, *job.MultiJobID, "an agent must not redirect its result into another multi-job")
	require.NotNil(t, job.ScheduleID)
	assert.Equal(t, scheduleID, *job.ScheduleID)

	require.NotNil(t, provider.InputSaveJob)
	assert.Equal(t, "client-1", provider.InputSaveJob.ClientID)
}
