package auditlog

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/db/migration/auditlog"
	"github.com/proximile/proxiport/db/sqlite"
)

var chainKey = []byte("audit-hmac-key-32-bytes-long-xxx")

func newMemProvider(t *testing.T, key []byte) *SQLiteProvider {
	t.Helper()
	db, err := sqlite.New(":memory:", auditlog.AssetNames(), auditlog.Asset, DataSourceOptions)
	require.NoError(t, err)
	p := &SQLiteProvider{db: db, hmacKey: key}
	require.NoError(t, p.loadChainHead())
	return p
}

func saveN(t *testing.T, p *SQLiteProvider, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		e := &Entry{
			Timestamp:   time.Date(2026, 7, 14, 12, 0, i, 0, time.UTC),
			Username:    "admin",
			Application: ApplicationLibraryCommand,
			Action:      ActionCreate,
			ID:          "id",
		}
		require.NoError(t, p.Save(e))
	}
}

// A freshly-written chain verifies, and each row carries an advancing seq and a
// MAC linked to the previous row.
func TestChainVerifyValid(t *testing.T) {
	p := newMemProvider(t, chainKey)
	defer func() { _ = p.Close() }()
	saveN(t, p, 5)

	res, err := p.Verify(context.Background())
	require.NoError(t, err)
	assert.True(t, res.Enabled)
	assert.True(t, res.Valid, "a freshly-written chain must verify")
	assert.Equal(t, 5, res.Checked)
	assert.Equal(t, int64(0), res.BreakSeq)
}

// Editing a stored row (the host adversary) is detected: the recomputed MAC no
// longer matches, and verification points at that row.
func TestChainTamperDetected(t *testing.T) {
	p := newMemProvider(t, chainKey)
	defer func() { _ = p.Close() }()
	saveN(t, p, 5)

	// Tamper: change a field of row seq=3 directly, as a disk editor would.
	_, err := p.db.Exec("UPDATE auditlog SET username = 'attacker' WHERE seq = 3")
	require.NoError(t, err)

	res, err := p.Verify(context.Background())
	require.NoError(t, err)
	assert.False(t, res.Valid, "a tampered row must fail verification")
	assert.Equal(t, int64(3), res.BreakSeq)
	assert.Equal(t, "mac", res.BreakKind)
}

// Deleting a row breaks the chain (a seq gap / link discontinuity).
func TestChainDeletionDetected(t *testing.T) {
	p := newMemProvider(t, chainKey)
	defer func() { _ = p.Close() }()
	saveN(t, p, 5)

	_, err := p.db.Exec("DELETE FROM auditlog WHERE seq = 3")
	require.NoError(t, err)

	res, err := p.Verify(context.Background())
	require.NoError(t, err)
	assert.False(t, res.Valid, "a deleted row must break the chain")
	assert.Equal(t, int64(4), res.BreakSeq, "the break surfaces at the row whose predecessor was removed")
}

// With no key provider the chain is inert: rows are written with seq 0 and
// Verify reports it disabled.
func TestChainDisabled(t *testing.T) {
	p := newMemProvider(t, nil)
	defer func() { _ = p.Close() }()
	saveN(t, p, 3)

	res, err := p.Verify(context.Background())
	require.NoError(t, err)
	assert.False(t, res.Enabled)

	var maxSeq int64
	require.NoError(t, p.db.Get(&maxSeq, "SELECT COALESCE(MAX(seq),0) FROM auditlog"))
	assert.Equal(t, int64(0), maxSeq, "with no key, seq stays 0")
}

// A restart continues the same chain rather than forking it: a second provider
// over the same database picks up the head and the combined chain verifies.
func TestChainSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	dso := DataSourceOptions

	p1, err := newSQLiteProvider(dir, dso, chainKey)
	require.NoError(t, err)
	saveN(t, p1, 3)
	require.NoError(t, p1.Close())

	p2, err := newSQLiteProvider(dir, dso, chainKey)
	require.NoError(t, err)
	defer func() { _ = p2.Close() }()
	saveN(t, p2, 2)

	res, err := p2.Verify(context.Background())
	require.NoError(t, err)
	assert.True(t, res.Valid, "the chain must stay valid across a restart")
	assert.Equal(t, 5, res.Checked)

	var maxSeq int64
	require.NoError(t, p2.db.Get(&maxSeq, "SELECT MAX(seq) FROM auditlog"))
	assert.Equal(t, int64(5), maxSeq, "seq continues, not restarts, across the reopen")
}

// The wrong key fails verification of a genuine chain (the key is load-bearing).
func TestChainWrongKeyFails(t *testing.T) {
	p := newMemProvider(t, chainKey)
	saveN(t, p, 3)
	rows := []*Entry{}
	require.NoError(t, p.db.Select(&rows, "SELECT * FROM `auditlog` ORDER BY seq ASC"))
	_ = p.Close()

	res := verifyChain([]byte("a-totally-different-32-byte-key!!"), rows)
	assert.False(t, res.Valid, "verifying under the wrong key must fail")
}
