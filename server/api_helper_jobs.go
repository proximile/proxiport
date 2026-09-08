package chserver

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"

	errors2 "github.com/proximile/proxiport/server/api/errors"
	"github.com/proximile/proxiport/server/api/jobs"
	"github.com/proximile/proxiport/server/clients/clientdata"
	"github.com/proximile/proxiport/share/comm"
	"github.com/proximile/proxiport/share/models"
	"github.com/proximile/proxiport/share/query"
	"github.com/proximile/proxiport/share/random"
	"github.com/proximile/proxiport/share/ws"
)

var ErrClientNotConnected = errors.New("client is not connected")

var generateNewJobID = func() (string, error) {
	return random.UUID4()
}

type JobProvider interface {
	GetByJID(clientID, jid string) (*models.Job, error)
	List(ctx context.Context, options *query.ListOptions) ([]*models.Job, error)
	Count(ctx context.Context, options *query.ListOptions) (int, error)
	// SaveJob creates or updates a job
	SaveJob(job *models.Job) error
	// CreateJob creates a new job. If already exist with a given JID - do nothing and return nil
	CreateJob(job *models.Job) error
	GetMultiJob(ctx context.Context, jid string) (*models.MultiJob, error)
	GetMultiJobSummaries(ctx context.Context, options *query.ListOptions) ([]*models.MultiJobSummary, error)
	CountMultiJobs(ctx context.Context, options *query.ListOptions) (int, error)
	SaveMultiJob(multiJob *models.MultiJob) error
	CleanupJobsMultiJobs(context.Context, int) error
	Close() error
}

func (al *APIListener) createAndRunJob(
	uiConnTS *ws.ConcurrentWebSocket,
	multiJobID *string,
	jid, cmd, interpreter, createdBy, cwd string,
	timeoutSec int,
	isSudo, isScript bool,
	client *clientdata.Client,
) error {
	curJob := models.Job{
		JID:          jid,
		StartedAt:    time.Now(),
		ClientID:     client.GetID(),
		ClientName:   client.GetName(),
		Command:      cmd,
		Cwd:          cwd,
		IsSudo:       isSudo,
		IsScript:     isScript,
		Interpreter:  interpreter,
		CreatedBy:    createdBy,
		TimeoutSec:   timeoutSec,
		MultiJobID:   multiJobID,
		StreamResult: uiConnTS != nil,
	}
	logPrefix := curJob.LogPrefix()

	// send the command to the client
	sshResp := &comm.RunCmdResponse{}

	var err error
	if !client.IsPaused() {
		if client.Connection != nil {
			err = comm.SendRequestAndGetResponse(client.GetConnection(), comm.RequestTypeRunCmd, curJob, sshResp, al.Log())
		} else {
			err = ErrClientNotConnected
		}
	} else {
		err = fmt.Errorf("client is paused (reason = %s)", client.PausedReason)
	}

	if err != nil {
		al.Errorf("%s, Error on execute remote command: %v", logPrefix, err)

		curJob.Status = models.JobStatusFailed
		now := time.Now()
		curJob.FinishedAt = &now
		curJob.Error = err.Error()

		// send the failed job to UI
		if uiConnTS != nil {
			_ = uiConnTS.WriteJSON(curJob)
		}
	} else {
		al.Debugf("%s, Job was sent to execute remote command: %q.", logPrefix, curJob.Command)

		// success, set fields received in response
		curJob.PID = &sshResp.Pid
		curJob.StartedAt = sshResp.StartedAt // override with the start time of the command
		curJob.Status = models.JobStatusRunning
	}

	// do not save the failed job if it's a single-client job
	if err != nil && multiJobID == nil {
		return err
	}

	if dbErr := al.jobProvider.CreateJob(&curJob); dbErr != nil {
		// just log it, cmd is running, when it's finished it can be saved on result return
		al.Errorf("%s, Failed to persist job: %v", logPrefix, dbErr)
	}

	return err
}

func (al *APIListener) StartMultiClientJob(ctx context.Context, multiJobRequest *jobs.MultiJobRequest) (*models.MultiJob, error) {
	jid, err := generateNewJobID()
	if err != nil {
		return nil, err
	}

	// by default abortOnErr is true
	abortOnErr := true
	if multiJobRequest.AbortOnError != nil {
		abortOnErr = *multiJobRequest.AbortOnError
	}
	if multiJobRequest.TimeoutSec <= 0 {
		multiJobRequest.TimeoutSec = al.config.Server.RunRemoteCmdTimeoutSec
	}

	if multiJobRequest.OrderedClients == nil {
		// try to rebuild the ordered client list
		if !hasClientTags(multiJobRequest) {
			multiJobRequest.OrderedClients, _, err = al.getOrderedClients(ctx, multiJobRequest.ClientIDs, multiJobRequest.GroupIDs)
			if err != nil {
				return nil, err
			}
		} else {
			multiJobRequest.OrderedClients, err = al.getOrderedClientsByTag(multiJobRequest.ClientTags)
			if err != nil {
				return nil, err
			}
		}
	}

	if len(multiJobRequest.OrderedClients) == 0 {
		return nil, fmt.Errorf("no clients for execution")
	}

	// Authorize the resolved target set here rather than only in the HTTP
	// handlers. The scheduler calls this directly, with the creator's username
	// and no check of its own, so a schedule kept running after its creator was
	// deleted or de-permissioned -- and because group and tag targets are
	// re-resolved on every run, it could reach clients the creator never had
	// access to, simply because someone later added one to a targeted group.
	//
	// The HTTP handlers check before calling too. Checking again costs a lookup
	// and makes the check impossible to route around.
	if err := al.checkMultiJobAccess(ctx, multiJobRequest); err != nil {
		return nil, err
	}

	command := multiJobRequest.Command
	if multiJobRequest.IsScript {
		decodedScriptBytes, err := base64.StdEncoding.DecodeString(multiJobRequest.Script)
		if err != nil {
			return nil, err
		}
		command = string(decodedScriptBytes)
	}

	multiJob := &models.MultiJob{
		MultiJobSummary: models.MultiJobSummary{
			JID:        jid,
			StartedAt:  time.Now(),
			CreatedBy:  multiJobRequest.Username,
			ScheduleID: multiJobRequest.ScheduleID,
		},
		ClientIDs:   multiJobRequest.ClientIDs,
		GroupIDs:    multiJobRequest.GroupIDs,
		ClientTags:  multiJobRequest.ClientTags,
		Command:     command,
		Interpreter: multiJobRequest.Interpreter,
		Cwd:         multiJobRequest.Cwd,
		IsScript:    multiJobRequest.IsScript,
		IsSudo:      multiJobRequest.IsSudo,
		TimeoutSec:  multiJobRequest.TimeoutSec,
		Concurrent:  multiJobRequest.ExecuteConcurrently,
		AbortOnErr:  abortOnErr,
	}
	if err := al.jobProvider.SaveMultiJob(multiJob); err != nil {
		return nil, err
	}

	go al.executeMultiClientJob(multiJob, multiJobRequest.OrderedClients)

	return multiJob, nil
}

func (al *APIListener) executeMultiClientJob(
	job *models.MultiJob,
	orderedClients []*clientdata.Client,
) {
	// for sequential execution - create a channel to get the job result
	var curJobDoneChannel chan *models.Job
	if !job.Concurrent {
		curJobDoneChannel = make(chan *models.Job)
		al.jobsDoneChannel.Set(job.JID, curJobDoneChannel)
		defer func() {
			close(curJobDoneChannel)
			al.jobsDoneChannel.Del(job.JID)
		}()
	}
	for _, client := range orderedClients {
		curJID, err := generateNewJobID()
		if err != nil {
			return
		}
		if job.Concurrent {
			go al.createAndRunJob( //nolint:errcheck // error is logged, nothing to act on here
				nil,
				&job.JID,
				curJID,
				job.Command,
				job.Interpreter,
				job.CreatedBy,
				job.Cwd,
				job.TimeoutSec,
				job.IsSudo,
				job.IsScript,
				client,
			)
		} else {
			err := al.createAndRunJob(
				nil,
				&job.JID,
				curJID,
				job.Command,
				job.Interpreter,
				job.CreatedBy,
				job.Cwd,
				job.TimeoutSec,
				job.IsSudo,
				job.IsScript,
				client,
			)
			if err != nil {
				if job.AbortOnErr && !errors.Is(err, ErrClientNotConnected) {
					break
				}
				continue
			}

			// TODO: review use of this flag as a testing hack. works but not too nice.
			// in tests skip next part to avoid waiting
			if al.insecureForTests {
				continue
			}

			// wait until command is finished
			jobResult := <-curJobDoneChannel
			if job.AbortOnErr && jobResult.Status == models.JobStatusFailed {
				break
			}
		}
	}
	if al.testDone != nil {
		al.testDone <- true
	}
}

// checkMultiJobAccess re-authorizes a resolved target set as the job's own user.
//
// A username with no user behind it is a refusal, not an escape: a schedule
// whose creator has been deleted must stop running, not run unchecked.
func (al *APIListener) checkMultiJobAccess(ctx context.Context, multiJobRequest *jobs.MultiJobRequest) error {
	if multiJobRequest.Username == "" {
		return errors2.APIError{
			Message:    "cannot authorize the job: it names no user",
			HTTPStatus: http.StatusForbidden,
		}
	}

	user, err := al.userService.GetByUsername(multiJobRequest.Username)
	if err != nil {
		return err
	}
	if user == nil {
		return errors2.APIError{
			Message:    fmt.Sprintf("cannot authorize the job: user %q no longer exists", multiJobRequest.Username),
			HTTPStatus: http.StatusForbidden,
		}
	}

	clientGroups, err := al.clientGroupProvider.GetAll(ctx)
	if err != nil {
		return err
	}

	return al.clientService.CheckClientsAccess(multiJobRequest.OrderedClients, user, clientGroups)
}
