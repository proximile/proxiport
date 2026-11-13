package sqlite

import (
	"os"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sqlitelib "modernc.org/sqlite/lib"

	"github.com/proximile/proxiport/db/migration/dummy"
	"github.com/proximile/proxiport/share/logger"
)

// fakeSQLiteErr stands in for a *modernc.org/sqlite.Error (whose fields are
// unexported and cannot be constructed from a test). It satisfies the same
// `Code() int` shape the busy/constraint classifiers match on.
type fakeSQLiteErr struct {
	code int
	msg  string
}

func (e fakeSQLiteErr) Code() int     { return e.code }
func (e fakeSQLiteErr) Error() string { return e.msg }

var (
	errBusy    = fakeSQLiteErr{code: sqlitelib.SQLITE_BUSY, msg: "database is locked"}
	errCorrupt = fakeSQLiteErr{code: sqlitelib.SQLITE_CORRUPT, msg: "database disk image is malformed"}
)

func TestSqliteWALEnabled(t *testing.T) {
	dataSourceName := t.TempDir() + "/test-db.sqlite3"
	_, err := New(dataSourceName, dummy.AssetNames(), dummy.Asset, DataSourceOptions{WALEnabled: true})
	require.NoError(t, err)
	_, err = os.Stat(dataSourceName + "-shm")
	require.NoError(t, err)
	_, err = os.Stat(dataSourceName + "-wal")
	require.NoError(t, err)
}

func TestSqliteWALDisabled(t *testing.T) {
	dataSourceName := t.TempDir() + "/test-db.sqlite3"
	_, err := New(dataSourceName, dummy.AssetNames(), dummy.Asset, DataSourceOptions{WALEnabled: false})
	require.NoError(t, err)
	_, err = os.Stat(dataSourceName + "-shm")
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(dataSourceName + "-wal")
	assert.ErrorIs(t, err, os.ErrNotExist)
}

func TestShouldSucceedWhenNoError(t *testing.T) {
	testLog := logger.NewLogger("retries", logger.LogOutput{File: os.Stdout}, logger.LogLevelDebug)

	_, err := WithRetryWhenBusy(func() (result any, err error) {
		return nil, nil
	}, "test", testLog)

	assert.NoError(t, err)
}

func TestShouldSucceedAfterRetries(t *testing.T) {
	testLog := logger.NewLogger("retries", logger.LogOutput{File: os.Stdout}, logger.LogLevelDebug)

	attempts := 0

	_, err := WithRetryWhenBusy(func() (result any, err error) {
		if attempts < 2 {
			attempts++
			return nil, errBusy
		}
		return nil, nil
	}, "test", testLog)

	assert.NoError(t, err)
	assert.Equal(t, 2, attempts)
}

func TestShouldFailWhenMaxBusyErrors(t *testing.T) {
	testLog := logger.NewLogger("retries", logger.LogOutput{File: os.Stdout}, logger.LogLevelDebug)

	attempts := 0

	_, err := WithRetryWhenBusy(func() (result any, err error) {
		attempts++
		return nil, errBusy
	}, "test", testLog)

	assert.EqualError(t, err, errBusy.Error())
	assert.Equal(t, DefaultMaxAttempts, attempts)
}

func TestShouldFailImmediatelyWhenNonBusyError(t *testing.T) {
	testLog := logger.NewLogger("retries", logger.LogOutput{File: os.Stdout}, logger.LogLevelDebug)

	attempts := 0

	_, err := WithRetryWhenBusy(func() (result any, err error) {
		attempts++
		// fail immediately when not a busy error
		return nil, errCorrupt
	}, "test", testLog)

	assert.EqualError(t, err, errCorrupt.Error())
	assert.Equal(t, 1, attempts)
}

// The settings every database is opened with. They were the defaults of the
// cgo driver the server used before; the pure-Go driver has to be told.
func TestDriverSettings(t *testing.T) {
	for _, wal := range []bool{false, true} {
		db, err := New(t.TempDir()+"/settings.db", dummy.AssetNames(), dummy.Asset, DataSourceOptions{WALEnabled: wal})
		require.NoError(t, err)

		var busyTimeout, synchronous int
		var journalMode string
		require.NoError(t, db.Get(&busyTimeout, "PRAGMA busy_timeout"))
		require.NoError(t, db.Get(&synchronous, "PRAGMA synchronous"))
		require.NoError(t, db.Get(&journalMode, "PRAGMA journal_mode"))
		assert.Equal(t, 5000, busyTimeout, "a blocked writer waits five seconds")
		assert.Equal(t, 1, synchronous, "synchronous is NORMAL")
		if wal {
			assert.Equal(t, "wal", journalMode)
		} else {
			assert.Equal(t, "delete", journalMode)
		}
		require.NoError(t, db.Close())
	}
}

// Times are written as text in the zone the value carries, and read back as
// the same instant with the same offset. Existing databases hold that text,
// and the server's queries compare it as text, so the format must not move.
//
// The process zone is set to something other than UTC for this test: in UTC
// a driver that converted every time to UTC would look the same as one that
// does not.
func TestTimesKeepTheirTextFormatAndOffset(t *testing.T) {
	processZone := time.Local
	time.Local = time.FixedZone("TEST", -5*60*60)
	defer func() { time.Local = processZone }()

	db, err := New(t.TempDir()+"/times.db", dummy.AssetNames(), dummy.Asset, DataSourceOptions{})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.Exec("CREATE TABLE times (label TEXT PRIMARY KEY, at DATETIME)")
	require.NoError(t, err)

	cases := []struct {
		label  string
		at     time.Time
		stored string
	}{
		{"process zone", time.Date(2026, 3, 8, 1, 30, 0, 123456789, time.Local), "2026-03-08 01:30:00.123456789-05:00"},
		{"another offset", time.Date(2020, 8, 19, 13, 9, 23, 0, time.FixedZone("", 3*60*60)), "2020-08-19 13:09:23+03:00"},
		{"utc", time.Date(2026, 3, 8, 6, 30, 0, 0, time.UTC), "2026-03-08 06:30:00+00:00"},
	}
	for _, tc := range cases {
		_, err = db.Exec("INSERT INTO times (label, at) VALUES (?, ?)", tc.label, tc.at)
		require.NoError(t, err)

		var stored string
		require.NoError(t, db.Get(&stored, "SELECT CAST(at AS TEXT) FROM times WHERE label = ?", tc.label))
		assert.Equal(t, tc.stored, stored, tc.label)

		var got time.Time
		require.NoError(t, db.Get(&got, "SELECT at FROM times WHERE label = ?", tc.label))
		assert.True(t, tc.at.Equal(got), "%s: got %s, stored %s", tc.label, got, tc.at)
		_, wantOffset := tc.at.Zone()
		_, gotOffset := got.Zone()
		assert.Equal(t, wantOffset, gotOffset, tc.label)
	}

	// An integer in a date column is a Unix time, in seconds or milliseconds.
	_, err = db.Exec("INSERT INTO times (label, at) VALUES ('seconds', 1700000000), ('milliseconds', 1700000000000)")
	require.NoError(t, err)
	for _, label := range []string{"seconds", "milliseconds"} {
		var got time.Time
		require.NoError(t, db.Get(&got, "SELECT at FROM times WHERE label = ?", label))
		assert.Equal(t, int64(1700000000), got.Unix(), label)
	}
}

// A writer that finds the database locked by another connection waits, then
// reports an error that IsBusyErr recognizes.
func TestBlockedWriterReportsBusy(t *testing.T) {
	path := t.TempDir() + "/busy.db"
	holder, err := New(path, dummy.AssetNames(), dummy.Asset, DataSourceOptions{})
	require.NoError(t, err)
	defer func() { _ = holder.Close() }()
	_, err = holder.Exec("CREATE TABLE b (a INTEGER)")
	require.NoError(t, err)
	// A short wait keeps the test quick; the default is asserted above.
	other, err := sqlx.Connect("sqlite", path+"?_busy_timeout=200")
	require.NoError(t, err)
	defer func() { _ = other.Close() }()

	tx, err := holder.Begin()
	require.NoError(t, err)
	_, err = tx.Exec("INSERT INTO b VALUES (1)")
	require.NoError(t, err)

	started := time.Now()
	_, err = other.Exec("INSERT INTO b VALUES (2)")
	require.Error(t, err)
	assert.True(t, IsBusyErr(err), "got %v", err)
	assert.False(t, IsConstraintErr(err))
	assert.GreaterOrEqual(t, time.Since(started), 150*time.Millisecond, "the writer waited before giving up")
	require.NoError(t, tx.Rollback())
}

// A duplicate key is a constraint error, not a busy one.
func TestDuplicateKeyIsAConstraintError(t *testing.T) {
	db, err := New(t.TempDir()+"/dup.db", dummy.AssetNames(), dummy.Asset, DataSourceOptions{})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	_, err = db.Exec("CREATE TABLE u (id TEXT PRIMARY KEY)")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO u VALUES ('a')")
	require.NoError(t, err)
	_, err = db.Exec("INSERT INTO u VALUES ('a')")
	require.Error(t, err)
	assert.True(t, IsConstraintErr(err), "got %v", err)
	assert.False(t, IsBusyErr(err))
}
