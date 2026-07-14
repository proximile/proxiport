package jobs

import (
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	jobsmig "github.com/proximile/proxiport/db/migration/jobs"
	"github.com/proximile/proxiport/db/sqlite"
	"github.com/proximile/proxiport/server/test/jb"
	"github.com/proximile/proxiport/share/enc"
	"github.com/proximile/proxiport/share/models"
)

var jobsTestDEK = []byte("0123456789abcdef0123456789abcdef")

func rawDetails(t *testing.T, p *SqliteProvider, jid string) string {
	t.Helper()
	var raw string
	require.NoError(t, p.db.Get(&raw, "SELECT details FROM jobs WHERE jid = ?", jid))
	return raw
}

func jobWithOutput(t *testing.T) *models.Job {
	job := jb.New(t).
		Result(&models.JobResult{StdOut: "SECRET-STDOUT", StdErr: "SECRET-STDERR", Summary: "exit-0"}).
		Build()
	job.Error = "SECRET-ERROR"
	return job
}

// With a key provider configured, stdout/stderr/error are ciphertext at rest but
// round-trip as plaintext through the API; the command and summary stay
// plaintext so the schedules "last execution" view still works without the key.
func TestJobResultEncryptedAtRest(t *testing.T) {
	db, err := sqlite.New(":memory:", jobsmig.AssetNames(), jobsmig.Asset, DataSourceOptions)
	require.NoError(t, err)
	p := NewSqliteProvider(db, enc.NewEnvelope(jobsTestDEK), testLog)
	defer func() { _ = p.Close() }()

	job := jobWithOutput(t)
	require.NoError(t, p.SaveJob(job))

	// The caller's in-memory job keeps the plaintext (only the copy is encrypted).
	assert.Equal(t, "SECRET-STDOUT", job.Result.StdOut, "caller's job must not be mutated")

	raw := rawDetails(t, p, job.JID)
	assert.NotContains(t, raw, "SECRET-STDOUT", "stdout must be ciphertext at rest")
	assert.NotContains(t, raw, "SECRET-STDERR", "stderr must be ciphertext at rest")
	assert.NotContains(t, raw, "SECRET-ERROR", "error must be ciphertext at rest")
	assert.Contains(t, raw, "enc:v1:", "the output fields must carry the envelope prefix")
	assert.Contains(t, raw, "exit-0", "summary stays plaintext (schedules read it without the key)")

	got, err := p.GetByJID(job.ClientID, job.JID)
	require.NoError(t, err)
	assert.Equal(t, "SECRET-STDOUT", got.Result.StdOut)
	assert.Equal(t, "SECRET-STDERR", got.Result.StdErr)
	assert.Equal(t, "SECRET-ERROR", got.Error)
}

// A jobs DB seeded in the legacy plaintext format is re-encrypted in place at
// startup and still reads back; empty fields are left alone.
func TestJobResultBackfill(t *testing.T) {
	db, err := sqlite.New(":memory:", jobsmig.AssetNames(), jobsmig.Asset, DataSourceOptions)
	require.NoError(t, err)

	// Seed a legacy plaintext row via a disabled provider.
	plain := NewSqliteProvider(db, nil, testLog)
	job := jobWithOutput(t)
	require.NoError(t, plain.SaveJob(job))
	require.Contains(t, rawDetails(t, plain, job.JID), "SECRET-STDOUT")

	// Constructing an enabled provider runs the backfill.
	enced := NewSqliteProvider(db, enc.NewEnvelope(jobsTestDEK), testLog)
	raw := rawDetails(t, enced, job.JID)
	assert.NotContains(t, raw, "SECRET-STDOUT", "backfill must encrypt legacy plaintext output")
	assert.Contains(t, raw, "enc:v1:")

	got, err := enced.GetByJID(job.ClientID, job.JID)
	require.NoError(t, err)
	assert.Equal(t, "SECRET-STDOUT", got.Result.StdOut)
}

// A stolen DB read under the wrong key fails closed rather than exposing
// ciphertext as if it were the value.
func TestJobResultWrongKeyFailsClosed(t *testing.T) {
	db, err := sqlite.New(":memory:", jobsmig.AssetNames(), jobsmig.Asset, DataSourceOptions)
	require.NoError(t, err)

	writer := NewSqliteProvider(db, enc.NewEnvelope(jobsTestDEK), testLog)
	job := jobWithOutput(t)
	require.NoError(t, writer.SaveJob(job))

	wrongKey := []byte("ffffffffffffffffffffffffffffffff")
	reader := NewSqliteProvider(db, enc.NewEnvelope(wrongKey), testLog)
	_, err = reader.GetByJID(job.ClientID, job.JID)
	require.Error(t, err, "reading encrypted output under the wrong key must fail closed")
}

// With no key provider, plaintext round-trips unchanged (transition mode).
func TestJobResultDisabledPassthrough(t *testing.T) {
	db, err := sqlite.New(":memory:", jobsmig.AssetNames(), jobsmig.Asset, DataSourceOptions)
	require.NoError(t, err)
	p := NewSqliteProvider(db, nil, testLog)
	defer func() { _ = p.Close() }()

	job := jobWithOutput(t)
	require.NoError(t, p.SaveJob(job))
	assert.Contains(t, rawDetails(t, p, job.JID), "SECRET-STDOUT")

	got, err := p.GetByJID(job.ClientID, job.JID)
	require.NoError(t, err)
	assert.Equal(t, "SECRET-STDOUT", got.Result.StdOut)
}
