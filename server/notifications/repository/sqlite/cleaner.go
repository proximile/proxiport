package sqlite

import (
	"context"
	"sync"
	"time"

	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/random"
)

type Closeable interface {
	Close() error
}

type cleaner struct {
	closer    chan struct{}
	done      chan struct{}
	closeOnce *sync.Once
	logger    *logger.Logger
	keepFor   time.Duration
	repo      repository
}

// Close stops the cleaner and waits for its goroutine to return.
//
// The wait is the point. Close used to only close the stop channel, which left
// the worker free to still be inside cleanOld() -- issuing a DELETE -- after
// Close had returned. A caller had no way to know the cleaner had actually
// stopped touching the database, and a test asserting "nothing was deleted
// after Close" was racing the very goroutine it thought it had shut down.
//
// Calling Close more than once is safe, and later calls return once the worker
// has exited.
func (c cleaner) Close() error {
	c.closeOnce.Do(func() { close(c.closer) })
	<-c.done
	return nil
}

func StartCleaner(logger *logger.Logger, r repository, keepFor time.Duration, checkEvery time.Duration) Closeable {
	c := cleaner{
		closer:    make(chan struct{}),
		done:      make(chan struct{}),
		closeOnce: new(sync.Once),
		logger:    logger,
		keepFor:   keepFor,
		repo:      r,
	}
	jam := random.AlphaNum(5)
	logger.Infof("started notifications cleaner id: %s", jam)
	go func() {
		defer close(c.done)

		// A ticker rather than time.After per iteration: one timer for the
		// lifetime of the cleaner instead of a fresh one each pass.
		ticker := time.NewTicker(checkEvery)
		defer ticker.Stop()

		c.cleanOld()
		for {
			select {
			case <-ticker.C:
				logger.Infof("cleaning notifications id: %s", jam)
				c.cleanOld()
			case <-c.closer:
				logger.Infof("closed notifications cleaner id: %s", jam)
				return
			}
		}
	}()

	return c
}

func (c cleaner) cleanOld() {
	before := time.Now().Add(-c.keepFor).UTC()
	c.logger.Infof("cleaning notifications older than %s", before.Format("2006-01-02 15:04:05"))
	ctx := context.Background()
	_, err := c.repo.db.ExecContext(
		ctx,
		"DELETE FROM `notifications_log` WHERE timestamp <= ?",
		before.Format("2006-01-02 15:04:05"),
	)
	if err != nil {
		c.logger.Errorf("cleaning notifications failed: %v", err)
	}
}
