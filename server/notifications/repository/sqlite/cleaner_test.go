package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/suite"

	"github.com/proximile/proxiport/db/sqlite"
	db "github.com/proximile/proxiport/server/notifications/repository/sqlite"
	"github.com/proximile/proxiport/share/logger"
)

// This suite used to coordinate with the cleaner's background ticker using
// fixed sleeps -- `time.Sleep(time.Second + 100*time.Millisecond)` against a
// one-second interval, and a one-second sleep to age a row. A 100ms margin is
// smaller than the scheduling jitter of a loaded machine, so the suite failed
// 11 of 30 runs under CPU starvation while passing every run on an idle box.
//
// The replacement rests on three rules:
//
//   - Waiting for something to HAPPEN polls with a generous deadline, so a slow
//     machine is merely slow rather than failing.
//   - Asserting something did NOT happen makes it impossible rather than
//     merely unlikely: a retention window measured in minutes cannot elapse
//     during a test, and Close() now waits for the worker to exit.
//   - Rows are aged with SQL, not by sleeping.
//
// Timestamps are stored at second granularity, so every window here is far
// away from a second boundary; nothing depends on sub-second rounding.
const (
	// Long enough that a row created during a test can never age out of it,
	// however starved the machine is -- but short enough that backdating past
	// it is unambiguous.
	freshWindow = time.Minute

	// Short enough that many ticks land inside a test, so "it was cleaned"
	// arrives promptly and "it was not cleaned" has had real opportunities to
	// go wrong.
	fastTick = 10 * time.Millisecond

	// Polling budget for a state change. Generous on purpose: exceeding it
	// means the cleaner is broken, not that the machine is busy.
	settleTimeout = 15 * time.Second

	// How long to let a supposedly-stopped cleaner prove it is stopped. A
	// longer wait here can only make the assertion stronger.
	quietPeriod = 50 * fastTick
)

type CleanerTestSuite struct {
	suite.Suite
	repository db.Repository
	db         *sqlx.DB
	logger     *logger.Logger
}

func (suite *CleanerTestSuite) SetupTest() {
	var err error
	suite.db, err = sqlite.New(":memory:", db.AssetNames(), db.Asset, sqlite.DataSourceOptions{})
	suite.NoError(err)
	suite.repository = db.NewRepository(suite.db, testLog)
	suite.logger = logger.NewLogger("notifications", logger.NewLogOutput(""), logger.LogLevelInfo)
}

func (suite *CleanerTestSuite) TestCleanerCleansNothingWhenNothing() {
	c := suite.startCleaner()
	defer suite.closeCleaner(c)

	suite.expectNotifications(0)
}

// Fresh rows survive the periodic clean. The retention window is a minute, so
// "fresh" cannot expire mid-test, and the wait guarantees many ticks ran.
func (suite *CleanerTestSuite) TestCleanerCleansNothingWhenEverythingIsFresh() {
	suite.createNewNotification()

	c := suite.startCleaner()
	defer suite.closeCleaner(c)

	time.Sleep(quietPeriod)

	suite.expectNotifications(1)
}

// The clean that StartCleaner runs before entering its loop removes what is
// already old, and leaves what is not.
func (suite *CleanerTestSuite) TestCleanerCleansOldNotificationsAfterStart() {
	suite.createNewNotification()
	suite.backdateEverything(2 * freshWindow)
	suite.createNewNotification()

	c := suite.startCleaner()
	defer suite.closeCleaner(c)

	suite.eventuallyNotifications(1)
}

// A row that ages after the cleaner started is removed by a later tick. The
// row is fresh when the initial clean runs, so only the periodic path can
// account for its removal.
func (suite *CleanerTestSuite) TestCleanerCleansOldNotificationsAfterTimeout() {
	suite.createNewNotification()

	c := suite.startCleaner()
	defer suite.closeCleaner(c)

	suite.eventuallyNotifications(1)
	suite.backdateEverything(2 * freshWindow)

	suite.eventuallyNotifications(0)
}

// After Close the cleaner must not touch the database again. Close waits for
// the worker to return, so the row created below is written after the goroutine
// is gone -- there is no tick left that could delete it, whatever the machine
// is doing.
func (suite *CleanerTestSuite) TestCleanerCloses() {
	c := suite.startCleaner()
	suite.NoError(c.Close())

	suite.createNewNotification()
	suite.backdateEverything(2 * freshWindow)

	time.Sleep(quietPeriod)

	suite.expectNotifications(1)
}

// Close is called on shutdown paths and in these tests' defers; calling it
// twice must not panic on a re-closed channel.
func (suite *CleanerTestSuite) TestCleanerCloseIsIdempotent() {
	c := suite.startCleaner()

	suite.NoError(c.Close())
	suite.NoError(c.Close())
}

func TestCleanerTestSuite(t *testing.T) {
	suite.Run(t, new(CleanerTestSuite))
}

func (suite *CleanerTestSuite) startCleaner() db.Closeable {
	repository := db.NewRepository(suite.db, testLog)
	return db.StartCleaner(suite.logger, repository, freshWindow, fastTick)
}

func (suite *CleanerTestSuite) closeCleaner(c db.Closeable) {
	suite.NoError(c.Close())
}

func (suite *CleanerTestSuite) createNewNotification() {
	suite.NoError(suite.repository.SetDone(context.Background(), GenerateNotification(), ""))
}

// backdateEverything ages every stored row by SQL. The INSERT leaves timestamp
// to the column default, so this is the only way to make a row old without
// sleeping for the retention window.
func (suite *CleanerTestSuite) backdateEverything(age time.Duration) {
	ts := time.Now().UTC().Add(-age).Format("2006-01-02 15:04:05")
	res, err := suite.db.Exec("UPDATE `notifications_log` SET `timestamp` = ?", ts)
	suite.NoError(err)

	// Without this the helper silently does nothing if the table is empty or
	// the column is renamed, and every "it was cleaned" test below would pass
	// for the wrong reason.
	affected, err := res.RowsAffected()
	suite.NoError(err)
	suite.NotZero(affected, "backdated no rows -- the fixture is not doing what the test assumes")
}

func (suite *CleanerTestSuite) expectNotifications(count int) {
	all, err := suite.repository.List(context.Background(), nil)
	suite.NoError(err)
	suite.Len(all, count)
}

func (suite *CleanerTestSuite) eventuallyNotifications(count int) {
	var last int
	suite.Eventuallyf(func() bool {
		all, err := suite.repository.List(context.Background(), nil)
		if err != nil {
			return false
		}
		last = len(all)
		return last == count
	}, settleTimeout, fastTick, "expected %d notifications, last saw %d", count, &last)
}
