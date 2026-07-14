package monitoring

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/enc"
)

var monTestDEK = []byte("0123456789abcdef0123456789abcdef")

func rawMeasurement(t *testing.T, p *SqliteProvider, clientID string) (processes, mountpoints string) {
	t.Helper()
	row := struct {
		Processes   string `db:"processes"`
		Mountpoints string `db:"mountpoints"`
	}{}
	require.NoError(t, p.db.Get(&row, "SELECT processes, mountpoints FROM measurements WHERE client_id = ? LIMIT 1", clientID))
	return row.Processes, row.Mountpoints
}

func listProcs(t *testing.T, dp DBProvider, clientID string) string {
	t.Helper()
	res, err := dp.ListProcessesByClientID(context.Background(), clientID, createProcessesDefaultOptions())
	require.NoError(t, err)
	require.NotEmpty(t, res)
	return string(res[0].Processes)
}

// processes/mountpoints are ciphertext at rest but decrypt transparently for the
// monitoring API.
func TestMeasurementEncryptedAtRest(t *testing.T) {
	dp, err := NewSqliteProvider(":memory:", DataSourceOptions, enc.NewEnvelope(monTestDEK), testLog)
	require.NoError(t, err)
	defer func() { _ = dp.Close() }()
	p := dp.(*SqliteProvider)

	m := testData[0]
	require.NoError(t, dp.CreateMeasurement(context.Background(), &m))
	assert.Contains(t, m.Processes, "chrome", "caller's measurement must not be mutated")

	proc, mount := rawMeasurement(t, p, "test_client_1")
	assert.NotContains(t, proc, "chrome", "process blob must be ciphertext at rest")
	assert.Contains(t, proc, "enc:v1:")
	assert.NotContains(t, mount, "free_b", "mountpoints must be ciphertext at rest")

	assert.Contains(t, listProcs(t, dp, "test_client_1"), "chrome", "API read must decrypt transparently")
}

// A monitoring DB seeded plaintext is re-encrypted in place at startup.
func TestMeasurementBackfill(t *testing.T) {
	dp, err := NewSqliteProvider(":memory:", DataSourceOptions, nil, testLog)
	require.NoError(t, err)
	m := testData[0]
	require.NoError(t, dp.CreateMeasurement(context.Background(), &m))
	p := dp.(*SqliteProvider)
	proc, _ := rawMeasurement(t, p, "test_client_1")
	require.Contains(t, proc, "chrome")

	// Re-open the same DB with an enabled envelope: the constructor backfills.
	enced := &SqliteProvider{db: p.db, logger: testLog, converter: p.converter, enc: enc.NewEnvelope(monTestDEK)}
	require.NoError(t, enced.encryptExistingMeasurements())

	proc, _ = rawMeasurement(t, p, "test_client_1")
	assert.NotContains(t, proc, "chrome", "backfill must encrypt legacy plaintext")
	assert.Contains(t, listProcs(t, enced, "test_client_1"), "chrome")
}

// Reading under the wrong key fails closed.
func TestMeasurementWrongKeyFailsClosed(t *testing.T) {
	dp, err := NewSqliteProvider(":memory:", DataSourceOptions, enc.NewEnvelope(monTestDEK), testLog)
	require.NoError(t, err)
	m := testData[0]
	require.NoError(t, dp.CreateMeasurement(context.Background(), &m))

	p := dp.(*SqliteProvider)
	wrong := &SqliteProvider{db: p.db, logger: testLog, converter: p.converter, enc: enc.NewEnvelope([]byte("ffffffffffffffffffffffffffffffff"))}
	_, err = wrong.ListProcessesByClientID(context.Background(), "test_client_1", createProcessesDefaultOptions())
	require.Error(t, err, "reading encrypted processes under the wrong key must fail closed")
}
