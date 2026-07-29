package auditlog

import (
	"context"
	"io"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/db/migration/auditlog"
	"github.com/proximile/proxiport/db/sqlite"
	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/query"
)

var dso = sqlite.DataSourceOptions{WALEnabled: true}

// seedEntry writes a single entry into a fresh auditlog.db in dir and closes it,
// so a rotation provider constructed against dir sees it on init.
func seedEntry(t *testing.T, dir string, e *Entry) {
	t.Helper()
	sq, err := newSQLiteProvider(dir, dso, nil)
	require.NoError(t, err)
	require.NoError(t, sq.Save(e))
	require.NoError(t, sq.Close())
}

// TestRotationKeepsFreshEntriesOnInit: an entry newer than the rotation period
// must NOT be rotated when the provider starts. Deterministic — a long period
// keeps the background ticker from firing during the test.
func TestRotationKeepsFreshEntriesOnInit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	seedEntry(t, dir, &Entry{Timestamp: time.Now(), Username: "fresh"})

	rotation, err := newRotationProvider(nil, time.Hour, 0, dir, dso, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rotation.Close() })

	entries, err := rotation.List(ctx, &query.ListOptions{})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "fresh", entries[0].Username)
}

// TestRotationRotatesAgedEntriesOnInit: an entry older than the rotation period
// must be rotated out when the provider starts. Deterministic — the age is set
// by the entry timestamp, not by sleeping.
func TestRotationRotatesAgedEntriesOnInit(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	period := time.Hour
	seedEntry(t, dir, &Entry{Timestamp: time.Now().Add(-2 * period), Username: "aged"})

	rotation, err := newRotationProvider(nil, period, 0, dir, dso, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rotation.Close() })

	entries, err := rotation.List(ctx, &query.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, entries, "aged entry must be rotated out of the live db")
	assertRotatedSqlite(t, dir, "aged")
}

// TestRotationExplicitRotate exercises rotate() directly: a live entry is moved
// into the rotated db and the live db is left empty. Deterministic — no ticker,
// no sleep.
func TestRotationExplicitRotate(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	rotation, err := newRotationProvider(nil, time.Hour, 0, dir, dso, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rotation.Close() })

	require.NoError(t, rotation.Save(&Entry{Timestamp: time.Now(), Username: "live"}))
	entries, err := rotation.List(ctx, &query.ListOptions{})
	require.NoError(t, err)
	require.Len(t, entries, 1)

	require.NoError(t, rotation.rotate())

	entries, err = rotation.List(ctx, &query.ListOptions{})
	require.NoError(t, err)
	require.Empty(t, entries, "live db must be empty right after rotate()")
	assertRotatedSqlite(t, dir, "live")
}

// TestRotationTickerRotates confirms the background ticker actually drives
// rotation. It polls (assert.Eventually) rather than sleeping a fixed amount, so
// a slow CI runner simply waits longer instead of failing. It asserts only that
// the live db drains — a subsequent empty rotation could overwrite the same-day
// rotated file, so the rotated content is covered by the deterministic tests
// above, not here.
func TestRotationTickerRotates(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	rotation, err := newRotationProvider(nil, 150*time.Millisecond, 0, dir, dso, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rotation.Close() })

	require.NoError(t, rotation.Save(&Entry{Timestamp: time.Now(), Username: "ticked"}))

	assert.Eventually(t, func() bool {
		entries, err := rotation.List(ctx, &query.ListOptions{})
		return err == nil && len(entries) == 0
	}, 10*time.Second, 20*time.Millisecond, "background ticker must rotate the live db within the timeout")
}

// assertRotatedSqlite opens the single rotated db in dir (found by glob, so it
// does not depend on recomputing the rotation timestamp) and asserts it holds
// exactly the expected entry.
func assertRotatedSqlite(t *testing.T, dir, expectedUsername string) {
	t.Helper()
	// "auditlog.*.db" matches the dated rotated files but not the live
	// "auditlog.db" (which has no dated middle segment).
	matches, err := filepath.Glob(path.Join(dir, "auditlog.*.db"))
	require.NoError(t, err)
	require.Len(t, matches, 1, "expected exactly one rotated db file")

	db, err := sqlite.New(matches[0], auditlog.AssetNames(), auditlog.Asset, dso)
	require.NoError(t, err)
	sq := &SQLiteProvider{db: db}
	defer func() { _ = sq.Close() }()

	entries, err := sq.List(context.Background(), &query.ListOptions{})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, expectedUsername, entries[0].Username)
}

// TestRotationPrunesOldFiles: with a retention of N, pruneRotated keeps only
// the N newest dated auditlog files and never touches the active auditlog.db.
func TestRotationPrunesOldFiles(t *testing.T) {
	dir := t.TempDir()
	l := logger.NewLogger("test", logger.LogOutput{File: io.Discard}, logger.LogLevelError)

	rotation, err := newRotationProvider(l, time.Hour, 2, dir, dso, nil)
	require.NoError(t, err)
	defer func() { _ = rotation.Close() }()

	// Fake several already-rotated files (dated names sort chronologically).
	rotated := []string{
		"auditlog.2026-01-01.db",
		"auditlog.2026-01-02.db",
		"auditlog.2026-01-03.db",
		"auditlog.2026-01-04.db",
	}
	for _, name := range rotated {
		require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600))
	}

	require.NoError(t, rotation.pruneRotated())

	matches, err := filepath.Glob(filepath.Join(dir, rotatedGlob))
	require.NoError(t, err)
	require.Len(t, matches, 2, "retention=2 should keep the 2 newest rotated files")
	assert.Equal(t, filepath.Join(dir, "auditlog.2026-01-03.db"), matches[0])
	assert.Equal(t, filepath.Join(dir, "auditlog.2026-01-04.db"), matches[1])

	// The active DB must survive pruning.
	_, err = os.Stat(filepath.Join(dir, sqliteFilename))
	require.NoError(t, err, "active auditlog.db must not be pruned")
}
