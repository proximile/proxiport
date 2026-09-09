package chserver

import (
	"context"
	"errors"
	"time"

	"github.com/gorilla/websocket"

	"github.com/proximile/proxiport/server/api/jobs"
	"github.com/proximile/proxiport/server/auditlog"
	"github.com/proximile/proxiport/server/validation"
	"github.com/proximile/proxiport/share/models"
	"github.com/proximile/proxiport/share/ws"
)

func (al *APIListener) handleCommandsExecutionWS(
	ctx context.Context,
	uiConnTS *ws.ConcurrentWebSocket,
	inboundMsg *jobs.MultiJobRequest,
	auditLogEntry *auditlog.Entry,
) {
	if inboundMsg.Command == "" {
		uiConnTS.WriteError("Command cannot be empty.", nil)
		return
	}
	if err := validation.ValidateInterpreter(inboundMsg.Interpreter, inboundMsg.IsScript); err != nil {
		uiConnTS.WriteError("Invalid interpreter", err)
		return
	}

	if inboundMsg.TimeoutSec <= 0 {
		inboundMsg.TimeoutSec = al.config.Server.RunRemoteCmdTimeoutSec
	}

	curUser, err := al.getUserModelForAuth(ctx)
	if err != nil {
		uiConnTS.WriteError("Could not get current user.", err)
		return
	}

	err = al.extendedPermissionCommandRaw(inboundMsg.Command, curUser)
	if err != nil {
		uiConnTS.WriteError("Extended Permission failed with ", err)
		al.Debugf("extended \"commands\" permission middleware: %v", err.Error())
		return
	}

	clientGroups, err := al.clientGroupProvider.GetAll(ctx)
	if err != nil {
		uiConnTS.WriteError("Could not get client groups", err)
	}
	err = al.clientService.CheckClientsAccess(inboundMsg.OrderedClients, curUser, clientGroups)
	if err != nil {
		uiConnTS.WriteError(err.Error(), nil)
		return
	}

	jid, err := generateNewJobID()
	if err != nil {
		uiConnTS.WriteError("Could not generate job id.", err)
		return
	}
	al.uiJobWebSockets.Set(jid, uiConnTS)
	defer al.uiJobWebSockets.Delete(jid)

	auditLogEntry.
		WithRequest(inboundMsg).
		WithID(jid).
		SaveForMultipleClients(inboundMsg.OrderedClients)

	createdBy := curUser.Username
	if inboundMsg.OrderedClients != nil && len(inboundMsg.OrderedClients) > 0 {
		// by default abortOnErr is true
		abortOnErr := true
		if inboundMsg.AbortOnError != nil {
			abortOnErr = *inboundMsg.AbortOnError
		}

		multiJob := &models.MultiJob{
			MultiJobSummary: models.MultiJobSummary{
				JID:       jid,
				StartedAt: time.Now(),
				CreatedBy: createdBy,
			},
			ClientIDs:   inboundMsg.ClientIDs,
			GroupIDs:    inboundMsg.GroupIDs,
			ClientTags:  inboundMsg.ClientTags,
			Command:     inboundMsg.Command,
			Cwd:         inboundMsg.Cwd,
			Interpreter: inboundMsg.Interpreter,
			TimeoutSec:  inboundMsg.TimeoutSec,
			Concurrent:  inboundMsg.ExecuteConcurrently,
			AbortOnErr:  abortOnErr,
			IsSudo:      inboundMsg.IsSudo,
			IsScript:    inboundMsg.IsScript,
		}
		if err := al.jobProvider.SaveMultiJob(multiJob); err != nil {
			uiConnTS.WriteError("Failed to persist a new multi-client job.", err)
			return
		}

		al.Debugf("Multi-client Job[id=%q] created to execute remote command on clients %s, groups %s tags %s: %q.", multiJob.JID, inboundMsg.ClientIDs, inboundMsg.GroupIDs, inboundMsg.GetClientTags(), inboundMsg.Command)

		uiConnTS.SetWritesBeforeClose(len(inboundMsg.OrderedClients))

		// for sequential execution - create a channel to get the job result
		var curJobDoneChannel chan *models.Job

		if !multiJob.Concurrent {
			// Buffered for every client in the run, and never closed. The
			// listener sends results into this channel from a detached
			// goroutine, so closing it raced that send -- an agent reporting a
			// result for a run that had just finished panicked the whole daemon
			// with "send on closed channel". The buffer means a legitimate
			// result is always accepted even if this loop has not reached its
			// receive yet, and a late or duplicate one is dropped by the
			// non-blocking send rather than blocking a goroutine forever.
			// Nothing here ranges over the channel, so the close signalled
			// nothing to begin with.
			curJobDoneChannel = make(chan *models.Job, len(inboundMsg.OrderedClients))
			al.jobsDoneChannel.Set(multiJob.JID, curJobDoneChannel)
			defer al.jobsDoneChannel.Del(multiJob.JID)
		}

		for _, client := range inboundMsg.OrderedClients {
			curJID, err := generateNewJobID()
			if err != nil {
				uiConnTS.WriteError("Could not generate job id.", err)
				return
			}
			if multiJob.Concurrent {
				go al.createAndRunJob( //nolint:errcheck // error is logged, nothing to act on here
					uiConnTS,
					&jid,
					curJID,
					inboundMsg.Command,
					multiJob.Interpreter,
					createdBy,
					multiJob.Cwd,
					multiJob.TimeoutSec,
					multiJob.IsSudo,
					multiJob.IsScript,
					client,
				)
			} else {
				err := al.createAndRunJob(
					uiConnTS,
					&jid,
					curJID,
					inboundMsg.Command,
					multiJob.Interpreter,
					createdBy,
					multiJob.Cwd,
					multiJob.TimeoutSec,
					multiJob.IsSudo,
					multiJob.IsScript,
					client,
				)

				if err != nil {
					if multiJob.AbortOnErr && !errors.Is(err, ErrClientNotConnected) {
						uiConnTS.Close()
						return
					}
					continue
				}

				// TODO: review use of this flag as a testing hack. works but not too nice.
				if al.insecureForTests {
					continue
				}

				// wait until command is finished
				jobResult := <-curJobDoneChannel
				if multiJob.AbortOnErr && jobResult.Status == models.JobStatusFailed {
					uiConnTS.Close()
					return
				}
			}
		}
	} else {
		client := inboundMsg.OrderedClients[0]

		al.createAndRunJob( //nolint:errcheck // error is logged, nothing to act on here
			uiConnTS,
			nil,
			jid,
			inboundMsg.Command,
			inboundMsg.Interpreter,
			createdBy,
			inboundMsg.Cwd,
			inboundMsg.TimeoutSec,
			inboundMsg.IsSudo,
			inboundMsg.IsScript,
			client,
		)
	}

	if al.testDone != nil {
		al.testDone <- true
	}

	// check for Close message from client to close the connection
	mt, message, err := uiConnTS.ReadMessage()
	if err != nil {
		if closeErr, ok := err.(*websocket.CloseError); ok {
			al.Debugf("Received a closed err on WS read: %v", closeErr)
			return
		}
		al.Debugf("Error read from websocket: %v", err)
		return
	}

	al.Debugf("Message received: type %v, msg %s", mt, message)
	uiConnTS.Close()
}
