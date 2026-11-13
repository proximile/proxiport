package sqlite

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	bindata "github.com/golang-migrate/migrate/v4/source/go_bindata"
	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite" // registers the pure-Go "sqlite" database/sql driver
	sqlitelib "modernc.org/sqlite/lib"

	"github.com/proximile/proxiport/share/logger"
)

const (
	// WALEnabled is the DSN fragment that switches the journal to WAL mode.
	WALEnabled = "_pragma=journal_mode(WAL)"
	// CompatDSNParams keeps the pure-Go driver on the settings the server ran
	// with under the cgo driver, whose defaults they were:
	//
	//   _time_format=sqlite   time.Time is written as
	//       "2006-01-02 15:04:05.999999999-07:00", in the zone the value
	//       carries. Existing rows hold exactly this text, and queries compare
	//       it as text, so new rows and query parameters must match it.
	//   _busy_timeout=5000    a writer blocked by another connection waits up
	//       to five seconds before it reports SQLITE_BUSY.
	//   _synchronous=NORMAL   the sync level the server has always run with.
	//   _inttotime=1          an integer in a DATE, DATETIME or TIMESTAMP
	//       column is read as a Unix time.
	//
	// There is deliberately no _timezone here: it would convert every written
	// time to that zone, and a server whose local zone is not UTC would then
	// hold rows in two zones that no longer compare correctly as text.
	CompatDSNParams             = "_time_format=sqlite&_busy_timeout=5000&_synchronous=NORMAL&_inttotime=1"
	defaultDelayBetweenAttempts = 200 * time.Millisecond
	DefaultMaxAttempts          = 5
	DefaultMaxOpenConnections   = 1
)

// MemoryDSN is the in-memory data source used by tests, with the same
// settings as a database file.
const MemoryDSN = ":memory:?" + CompatDSNParams

// DSN returns the data source name for the SQLite database at path. Every
// SQLite database the server opens goes through it, so they all get the same
// driver settings.
func DSN(path string, wal bool) string {
	params := []string{CompatDSNParams}
	if wal {
		params = append(params, WALEnabled)
	}
	return path + "?" + strings.Join(params, "&")
}

type DataSourceOptions struct {
	WALEnabled         bool
	MaxOpenConnections int
}

// New returns a new sqlite DB instance with migrated DB scheme to the latest version.
// assetNames and asset are used to migrate DB scheme.
func New(dataSourceName string, assetNames []string, asset func(name string) ([]byte, error), dataSourceOptions DataSourceOptions) (*sqlx.DB, error) {
	dbPath := dataSourceName
	dataSourceName = DSN(dataSourceName, dataSourceOptions.WALEnabled)
	db, err := sqlx.Connect("sqlite", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to DB: %v", err)
	}
	if dbPath != ":memory:" {
		if err = os.Chmod(dbPath, 0600); err != nil {
			return nil, fmt.Errorf("failed to chmod %s: %s", dbPath, err)
		}
	}

	maxConns := dataSourceOptions.MaxOpenConnections
	if maxConns == 0 {
		maxConns = DefaultMaxOpenConnections
	}

	db.SetMaxOpenConns(maxConns)

	s := bindata.Resource(assetNames,
		func(name string) ([]byte, error) {
			return asset(name)
		})
	sourceDriver, err := bindata.WithInstance(s)
	if err != nil {
		return nil, fmt.Errorf("failed to init DB source driver: %v", err)
	}

	dbDriver, err := migratesqlite.WithInstance(db.DB, &migratesqlite.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to init DB migration driver: %v", err)
	}

	m, err := migrate.NewWithInstance("go-bindata", sourceDriver, "sqlite", dbDriver)
	if err != nil {
		return nil, fmt.Errorf("failed to init DB migration instance: %v", err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return nil, fmt.Errorf("failed to migrate DB to the latest version: %v", err)
	}

	return db, nil
}

// sqliteCoder is satisfied by *modernc.org/sqlite.Error (real driver errors) and
// by test doubles. Classifying through it — via errors.As — keeps the concrete
// driver error type out of every call site that needs to inspect a result code.
type sqliteCoder interface{ Code() int }

// sqliteResultCode extracts the primary SQLite result code from err, if err (or
// anything it wraps) carries one. modernc stores the operation result code,
// which may be an extended code (e.g. SQLITE_CONSTRAINT_UNIQUE = 2067), so the
// low byte is masked back to the primary code (SQLITE_CONSTRAINT = 19).
func sqliteResultCode(err error) (int, bool) {
	var c sqliteCoder
	if errors.As(err, &c) {
		return c.Code() & 0xff, true
	}
	return 0, false
}

// IsBusyErr reports whether err is a SQLITE_BUSY condition (database locked).
func IsBusyErr(err error) bool {
	code, ok := sqliteResultCode(err)
	return ok && code == sqlitelib.SQLITE_BUSY
}

// IsConstraintErr reports whether err is a SQLITE_CONSTRAINT violation, e.g. a
// duplicate primary key or unique-index collision.
func IsConstraintErr(err error) bool {
	code, ok := sqliteResultCode(err)
	return ok && code == sqlitelib.SQLITE_CONSTRAINT
}

func WithRetryWhenBusy[R any](retryAbleFn func() (result R, err error), label string, l *logger.Logger) (result R, err error) {
	for attempt := 1; attempt <= DefaultMaxAttempts; attempt++ {
		if attempt > 1 && err != nil {
			if IsBusyErr(err) {
				l.Debugf("%s: attempt %d: source err = %+v\n", label, attempt, err)
				jitter := time.Duration((rand.Intn(100))) * time.Millisecond
				time.Sleep(defaultDelayBetweenAttempts + jitter)
			} else {
				// a different error from database busy, so fail immediately
				l.Debugf("%s: attempt %d: non-retryable err = %+v\n", label, attempt, err)
				return result, err
			}
		}
		// make an attempt to complete the retryable fn
		result, err = retryAbleFn()
		// if no error then return immediately if with success result
		if err == nil {
			return result, nil
		}
	}

	l.Errorf("%s: failed after max attempts: err = %+v\n", label, err)
	return result, err
}
