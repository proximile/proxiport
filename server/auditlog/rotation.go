package auditlog

import (
	"context"
	"database/sql"
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
	sqliteFilename  = "auditlog.db"
	rotatedFilename = "auditlog.2006-01-02.db"
	// rotatedGlob matches rotated files (auditlog.<date>.db) but not the
	// active auditlog.db, whose name has no middle segment.
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

	mtx    sync.RWMutex
	sqlite *SQLiteProvider
}

func newRotationProvider(l *logger.Logger, period time.Duration, retention int, dataDir string, dataSourceOptions sqlite.DataSourceOptions, hmacKey []byte) (*RotationProvider, error) {
	sqlite, err := newSQLiteProvider(dataDir, dataSourceOptions, hmacKey)
	if err != nil {
		return nil, err
	}

	r := &RotationProvider{
		logger:    l,
		period:    period,
		retention: retention,
		dataDir:   dataDir,
		hmacKey:   hmacKey,
		sqlite:    sqlite,
		ticker:    time.NewTicker(period),
	}
	err = r.rotateIfNeeded()
	if err != nil {
		return nil, err
	}

	go r.rotationLoop()

	return r, nil
}

func (r *RotationProvider) rotationLoop() {
	for range r.ticker.C {
		err := r.rotate()
		if err != nil {
			r.logger.Errorf("Could not rotate auditlog: %v", err)
		}
	}
}

func (r *RotationProvider) rotate() error {
	r.mtx.Lock()
	defer r.mtx.Unlock()

	err := r.sqlite.Close()
	if err != nil {
		return err
	}

	sqliteFn := path.Join(r.dataDir, sqliteFilename)
	rotatedFn := path.Join(r.dataDir, time.Now().Format(rotatedFilename))
	err = os.Rename(sqliteFn, rotatedFn)
	if err != nil {
		return err
	}

	r.sqlite, err = newSQLiteProvider(r.dataDir, r.dataSourceOptions, r.hmacKey)
	if err != nil {
		return err
	}

	if err := r.pruneRotated(); err != nil {
		// Pruning is best-effort: a fresh, working DB matters more than a
		// stale rotated file, so log and carry on rather than fail rotation.
		r.logger.Errorf("Could not prune old rotated auditlogs: %v", err)
	}

	return nil
}

// pruneRotated deletes the oldest rotated auditlog files beyond the configured
// retention count. Retention <= 0 keeps every rotated file. Rotated names are
// dated (auditlog.YYYY-MM-DD.db), so a lexicographic sort is chronological.
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

func (r *RotationProvider) Save(e *Entry) error {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	return r.sqlite.Save(e)
}
func (r *RotationProvider) List(ctx context.Context, l *query.ListOptions) ([]*Entry, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	return r.sqlite.List(ctx, l)
}
func (r *RotationProvider) Count(ctx context.Context, l *query.ListOptions) (int, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	return r.sqlite.Count(ctx, l)
}
func (r *RotationProvider) Verify(ctx context.Context) (ChainVerification, error) {
	r.mtx.RLock()
	defer r.mtx.RUnlock()
	return r.sqlite.Verify(ctx)
}
func (r *RotationProvider) Close() error {
	r.mtx.Lock()
	defer r.mtx.Unlock()
	r.ticker.Stop()
	return r.sqlite.Close()
}
