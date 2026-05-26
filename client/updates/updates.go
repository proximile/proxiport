package updates

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/proximile/proxiport/share/comm"
	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/models"
)

type PackageManager interface {
	IsAvailable(context.Context) bool
	GetUpdatesStatus(context.Context, *logger.Logger) (*models.UpdatesStatus, error)
}

type Updates struct {
	// mtx protects both conn and status
	mtx    sync.RWMutex
	conn   ssh.Conn
	status *models.UpdatesStatus

	interval    time.Duration
	refreshChan chan struct{}

	pkgMgr PackageManager
	logger *logger.Logger
}

func New(logger *logger.Logger, interval time.Duration) *Updates {
	return &Updates{
		interval:    interval,
		refreshChan: make(chan struct{}),
		logger:      logger,
	}
}

func (u *Updates) Start(ctx context.Context) {
	if u.interval <= 0 {
		return
	}

	go u.refreshLoop(ctx)
}

func (u *Updates) getPackageManager(ctx context.Context) PackageManager {
	if u.pkgMgr != nil {
		return u.pkgMgr
	}
	for _, pm := range packageManagers {
		if pm.IsAvailable(ctx) {
			u.pkgMgr = pm
			return pm
		}
	}
	return nil
}

func (u *Updates) Refresh() {
	select {
	case u.refreshChan <- struct{}{}:
	default:
	}
}

func (u *Updates) refreshLoop(ctx context.Context) {
	for {
		u.refreshStatus(ctx)

		select {
		case <-ctx.Done():
			u.logger.Debugf("OS updates refreshLoop finished")
			return
		// acceptable use of time.After, as the number of triggered refreshes is small
		case <-time.After(u.interval):
		case <-u.refreshChan:
		}
	}
}

func (u *Updates) refreshStatus(ctx context.Context) {
	var newStatus *models.UpdatesStatus

	pkgMgr := u.getPackageManager(ctx)
	if pkgMgr == nil {
		newStatus = &models.UpdatesStatus{
			Error: "no supported package manager found",
		}
	} else {
		u.logger.Infof("Using %v for updates", reflect.TypeOf(pkgMgr).Elem().Name())

		status, err := pkgMgr.GetUpdatesStatus(ctx, u.logger)
		if err != nil {
			newStatus = &models.UpdatesStatus{
				Error: err.Error(),
			}
		} else {
			newStatus = status
		}
	}
	newStatus.Refreshed = time.Now()

	if newStatus.Error != "" {
		u.logger.Infof("Refreshing OS patch level (pending updates) failed: %v", newStatus.Error)
	} else {
		u.logger.Infof("OS patch level (pending updates) refreshed, %v updates available (%v security)",
			newStatus.UpdatesAvailable, newStatus.SecurityUpdatesAvailable)
	}

	u.mtx.Lock()
	u.status = newStatus
	u.mtx.Unlock()

	go u.sendUpdates()
}

// sendUpdates sends updates in background
func (u *Updates) sendUpdates() {
	u.mtx.RLock()
	defer u.mtx.RUnlock()

	if u.conn != nil && u.status != nil {
		data, err := json.Marshal(u.status)
		if err != nil {
			u.logger.Errorf("Could not marshal json for updates status: %v", err)
			return
		}

		_, _, err = u.conn.SendRequest(comm.RequestTypeUpdatesStatus, false, data)
		if err != nil {
			u.logger.Errorf("Could not sent updates status: %v", err)
			return
		}
	}
}

func (u *Updates) SetConn(c ssh.Conn) {
	u.mtx.Lock()
	u.conn = c
	u.mtx.Unlock()

	// Push the cached status to the newly-set conn. Without this the
	// status emitted by the very first refreshStatus() can race against
	// SetConn() — if the sendUpdates goroutine acquires the lock before
	// SetConn, it sees u.conn == nil and silently returns, and nothing
	// re-sends until the next interval (which may be hours). The race
	// shows up reliably under -race and was hanging the test for the
	// full 10-minute timeout.
	go u.sendUpdates()
}

func (u *Updates) Stop() {
	u.conn = nil
}
