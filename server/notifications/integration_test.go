package notifications_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	smtpmock "github.com/mocktools/go-smtp-mock/v2"
	"github.com/stretchr/testify/suite"

	"github.com/proximile/proxiport/db/sqlite"
	"github.com/proximile/proxiport/server/notifications"
	"github.com/proximile/proxiport/server/notifications/channels/rmailer"
	"github.com/proximile/proxiport/server/notifications/channels/scriptRunner"
	notificationsrepo "github.com/proximile/proxiport/server/notifications/repository/sqlite"
	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/simpleops"
)

var testLog = logger.NewLogger("client", logger.LogOutput{File: os.Stdout}, logger.LogLevelDebug)

type NotificationsIntegrationTestSuite struct {
	suite.Suite
	dispatcher     notifications.Dispatcher
	store          notificationsrepo.Repository
	server         *smtpmock.Server
	runner         notifications.Processor
	mailConsumer   notifications.Consumer
	scriptConsumer notifications.Consumer

	// workDir is the script consumer's working directory: a fresh temp dir per
	// test, so the script's output cannot be a leftover from an earlier run.
	// It used to be the package directory, which meant out.json was a
	// gitignored file that survived between runs -- a passing assertion there
	// proved only that some run at some point had written the right bytes.
	workDir string
	// script is the absolute path to test.sh, which stays in the package dir.
	script string
}

func (suite *NotificationsIntegrationTestSuite) SetupTest() {
	db, err := sqlite.New(":memory:", notificationsrepo.AssetNames(), notificationsrepo.Asset, sqlite.DataSourceOptions{})
	suite.NoError(err)
	suite.store = notificationsrepo.NewRepository(db, testLog)
	suite.dispatcher = notifications.NewDispatcher(suite.store)
	suite.server = smtpmock.New(smtpmock.ConfigurationAttr{
		//LogToStdout:              true, // for debugging (especially connection)
		//LogServerActivity:        true, // for debugging (especially connection)
		MultipleMessageReceiving: true,
		// PortNumber:               33334, // randomly generated
	})

	if err := suite.server.Start(); err != nil {
		fmt.Println(err)
	}

	suite.mailConsumer = rmailer.NewConsumer(rmailer.NewRMailer(rmailer.Config{
		Host:     "localhost",
		Port:     suite.server.PortNumber(),
		Domain:   "example.com",
		From:     "test@example.com",
		TLS:      false,
		AuthType: rmailer.AuthTypeNone,
		NoNoop:   true,
	}, testLog), testLog)

	// The script itself stays where it is tracked, and is named absolutely;
	// only the working directory moves. RunCancelableScript sets cmd.Dir to the
	// working directory, so the script's "> out.json" lands in the temp dir
	// either way, and nothing has to write an executable file at test time.
	pwd, err := os.Getwd()
	suite.Require().NoError(err)
	suite.script = filepath.Join(pwd, "test.sh")
	suite.workDir = suite.T().TempDir()

	suite.scriptConsumer = scriptRunner.NewConsumer(testLog, suite.workDir)

	suite.runner = notifications.NewProcessor(
		logger.NewLogger("notifications", logger.NewLogOutput(filepath.Join(suite.workDir, "out.log")), logger.LogLevelInfo),
		suite.store, suite.mailConsumer, suite.scriptConsumer)
}

func (suite *NotificationsIntegrationTestSuite) TearDownTest() {
	if suite.server != nil {
		_ = suite.server.Stop()
	}
}

type ScriptIO struct {
	Recipients []string `json:"recipients"`
	Data       string   `json:"data"`
}

func (suite *NotificationsIntegrationTestSuite) TestDispatcherCreatesNotification() {
	notification := notifications.NotificationData{
		Target:      "smtp",
		Recipients:  []string{"stefan.tester@example.com"},
		Subject:     "test-subject",
		Content:     "test-content-mail",
		ContentType: notifications.ContentTypeTextHTML,
	}
	_, err := suite.dispatcher.Dispatch(context.Background(), problemIdentifiable, notification)
	suite.NoError(err)

	notification = notifications.NotificationData{
		Target:      suite.script,
		Recipients:  []string{"r1@example.com", "somethin323-55@test.co"},
		Subject:     "test-subject",
		Content:     "test-content",
		ContentType: notifications.ContentTypeTextPlain,
	}
	d, err := suite.dispatcher.Dispatch(context.Background(), problemIdentifiable, notification)
	suite.NoError(err)

	// Both consumers run asynchronously, so wait for the effect rather than for
	// a duration. A fixed 100ms sleep raced the script subprocess and the SMTP
	// delivery, and lost often enough to redden main on an unrelated merge --
	// the failure looked like the notifications package but was only ever the
	// clock.
	outFile := filepath.Join(suite.workDir, "out.json")
	suite.Require().Eventually(func() bool {
		if len(suite.server.Messages()) != 1 {
			return false
		}
		_, err := os.Stat(outFile)
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, "the mail and script consumers should both have run")

	suite.T().Log(suite.store.Details(context.Background(), d.ID()))

	suite.ExpectedMessages(1)
	// suite.ExpectMessage(notification.Recipients, notification.Subject, string(notification.ContentType), notification.Content)

	in := ScriptIO{
		Recipients: []string{"r1@example.com", "somethin323-55@test.co"},
		Data:       "test-content",
	}

	out, err := simpleops.ReadJSONFileIntoStruct[ScriptIO](outFile)
	suite.NoError(err)
	suite.Equal(in, out)
}

func (suite *NotificationsIntegrationTestSuite) ExpectedMessages(count int) bool {
	return suite.Len(suite.server.Messages(), count)
}

func (suite *NotificationsIntegrationTestSuite) ExpectMessage(to []string, subject string, contentType string, content string) {
	if !suite.ExpectedMessages(1) {
		return
	}
	receivedMail := rmailer.ReceivedMail{Message: suite.server.Messages()[0]}

	suite.Equal(to, receivedMail.GetTo())

	suite.Equal(subject, receivedMail.GetSubject())

	suite.Equal(contentType, receivedMail.GetContentType())

	suite.Equal(content, receivedMail.GetContent())

}

func TestNotificationsIntegrationTestSuite(t *testing.T) {
	suite.Run(t, new(NotificationsIntegrationTestSuite))
}
