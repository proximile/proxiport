package auditlog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/proximile/proxiport/db/sqlite"

	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/query"
)

const (
	sqliteFilename = "auditlog.db"
	// rotatedFilename is a UTC instant, and both halves of that matter.
	//
	// It used to be the local DATE. The rotation ticker is monotonic -- 24h
	// from whenever the daemon started, never re-anchored to midnight -- so on
	// the autumn DST fall-back, when the local day is 25 hours long, a tick
	// whose phase lands in local [00:00, 01:00) recurs on the SAME local date
	// 24 real hours later. Both rotations computed the same filename and
	// os.Rename silently replaced the first rotated database with the second.
	// A day of audit history gone, no error: a successful replace is not an
	// error, and each rotated file carries its own genesis-anchored HMAC chain,
	// so Verify cannot see the gap either. A UTC day is always 24 hours, so the
	// date advances by exactly one per tick.
	//
	// Carrying the time as well makes the name unique by construction rather
	// than by argument -- a restart re-phases the ticker, so two rotations can
	// legitimately fall on one date. Colons are not in the name because Windows
	// will not have them in a filename.
	rotatedFilename = "auditlog.2006-01-02T15-04-05Z.db"
	// rotatedGlob matches rotated files (auditlog.<instant>.db) but not the
	// active auditlog.db, whose name has no middle segment. It also still
	// matches the auditlog.<date>.db files written before this change, and
	// those sort before same-day instants, which is the order they were
	// written in.
	rotatedGlob = "auditlog.*.db"
)

type RotationProvider struct {
	logger            *logger.Logger
	period            time.Duration
	retention         int
	ticker            *time.Ticker
	dataDir           string
	dataSourceOptions sqlite.DataSourceOptions
	hmacKey           []byte

	done chan struct{}

	mtx    sync.RWMutex
	sqlite *SQLiteProvider
}

// errAuditLogUnavailable is returned when a rotation could not reopen the
// database. Callers get an error instead of a nil dereference, which from the
// client listener would be an unrecovered panic.
var errAuditLogUnavailable = errors.New("audit log is unavailable: rotation failed to reopen the database")

func newRotationProvider(l *logger.Logger, period time.Duration, retention int, dataDir string, dataSourceOptions sqlite.DataSourceOptions, hmacKey []byte) (*RotationProvider, error) {
	sqlite, err := newSQLiteProvider(dataDir, dataSourceOptions, hmacKey)
	if err != nil {
		return nil, err
	}

	r := &RotationProvider{
		logger:            l,
		period:            period,
		retention:         retention,
		dataDir:           dataDir,
		dataSourceOptions: dataSourceOptions,
		hmacKey:           hmacKey,
		sqlite:            sqlite,
		ticker:            time.NewTicker(period),
		done:              make(chan struct{}),
	}
	err = r.rotateIfNeeded()
	if err != nil {
		return nil, err
	}

	go r.rotationLoop()

	return r, nil
}

func (r *RotationProvider) rotationLoop() {
	for {
		select {
		case <-r.done:
			return
		case <-r.ticker.C:
			err := r.rotate()
			if err != nil {
				r.logger.Errorf("Could not rotate auditlog: %v", err)
			}
		}
	}
}

// rotatedName is the file the live audit log is renamed to at the given
// instant. It is a function of its own so the DST case that used to collide can
// be exercised without a clock.
func rotatedName(now time.Time) string {
	return now.UTC().Format(rotatedFilename)
}

func (r *RotationProvider) rotate() error {
	r.mtx.Lock()
	defer r.mtx.Unlock()

	err := r.sqlite.Close()
	if err != nil {
		return err
	}

	sqliteFn := path.Join(r.dataDir, sqliteFilename)
	rotatedFn := path.Join(r.dataDir, rotatedName(time.Now()))

	// Refuse to write over an existing rotated file. The name is a UTC instant
	// and so cannot collide in practice; this is here so that "rotation
	// destroyed a day of audit history" is unreachable by construction rather
	// than by reasoning about clocks.
	if _, statErr := os.Lstat(rotatedFn); statErr == nil {
		r.reopen()
		return fmt.Errorf("refusing to rotate over the existing %s", filepath.Base(rotatedFn))
	} else if !errors.Is(statErr, os.ErrNotExist) {
		r.reopen()
		return statErr
	}

	err = os.Rename(sqliteFn, rotatedFn)
	if err != nil {
		// The live file is still there, just closed. Reopen it so audit writes
		// keep working until the next attempt.
		r.reopen()
		return err
	}

	// Open into a local first. Assigning the multi-value result directly stored
	// nil on failure, and every later Save/List dereferenced it.
	provider, err := newSQLiteProvider(r.dataDir, r.dataSourceOptions, r.hmacKey)
	if err != nil {
		// Put the rotated file back so there is something to serve from.
		if renameErr := os.Rename(rotatedFn, sqliteFn); renameErr != nil {
			r.logger.Errorf("Could not restore auditlog %s after a failed rotation: %v", rotatedFn, renameErr)
		}
		r.reopen()
		return err
	}
	r.sqlite = provider

	if err := r.pruneRotated(); err != nil {
		// Pruning is best-effort: a fresh, working DB matters more than a
		// stale rotated file, so log and carry on rather than fail rotation.
		r.logger.Errorf("Could not prune old rotated auditlogs: %v", err)
	}

	return nil
}

// pruneRotated deletes the oldest rotated auditlog files beyond the configured
// retention count. Retention <= 0 keeps every rotated file. Rotated names lead
// with a UTC instant (auditlog.YYYY-MM-DDTHH-MM-SSZ.db), so a lexicographic
// sort is chronological -- including against the auditlog.YYYY-MM-DD.db names
// written before that format changed.
func (r *RotationProvider) pruneRotated() error {
	if r.retention <= 0 {
		return nil
	}

	matches, err := filepath.Glob(filepath.Join(r.dataDir, rotatedGlob))
	if err != nil {
		return err
	}
	if len(matches) <= r.retention {
		return nil
	}

	sort.Strings(matches)
	for _, old := range matches[:len(matches)-r.retention] {
		if err := os.Remove(old); err != nil {
			return err
		}
		r.logger.Infof("pruned rotated auditlog %s (retention=%d)", filepath.Base(old), r.retention)
	}

	return nil
}

func (r *RotationProvider) rotateIfNeeded() error {
	oldest, err := r.sqlite.OldestTimestamp(context.Background())
	if err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}

	if time.Since(oldest) > r.period {
		return r.rotate()
	}

	return nil
}

// reopen restores r.sqlite after a rotation step failed. It is called with the
// write lock held.
func (r *RotationProvider) reopen() {
	provider, err := newSQLiteProvider(r.dataDir, r.dataSourceOptions, r.hmacKey)
	if err != nil {
		r.sqlite = nil
		r.logger.Errorf("Could not reopen auditlog after a failed rotation: %v", err)
		return
	}
	r.sqlite = provider
}

func (r *RotationProvider) Save(e *Entry) error {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	if r.sqlite == nil {
		return errAuditLogUnavailable
	}
	return r.sqlite.Save(e)
}
func (r *RotationProvider) List(ctx context.Context, l *query.ListOptions) ([]*Entry, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	if r.sqlite == nil {
		return nil, errAuditLogUnavailable
	}
	return r.sqlite.List(ctx, l)
}
func (r *RotationProvider) Count(ctx context.Context, l *query.ListOptions) (int, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	if r.sqlite == nil {
		return 0, errAuditLogUnavailable
	}
	return r.sqlite.Count(ctx, l)
}
func (r *RotationProvider) Verify(ctx context.Context) (ChainVerification, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	if r.sqlite == nil {
		return ChainVerification{}, errAuditLogUnavailable
	}
	return r.sqlite.Verify(ctx)
}
func (r *RotationProvider) Close() error {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	r.ticker.Stop()
	select {
	case <-r.done:
	default:
		close(r.done)
	}
	if r.sqlite == nil {
		return nil
	}
	return r.sqlite.Close()
}
