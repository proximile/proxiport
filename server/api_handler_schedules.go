package chserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/proximile/proxiport/server/api"
	errors2 "github.com/proximile/proxiport/server/api/errors"
	"github.com/proximile/proxiport/server/api/jobs/schedule"
	"github.com/proximile/proxiport/server/api/users"
	"github.com/proximile/proxiport/server/auditlog"
	"github.com/proximile/proxiport/server/cgroups"
	"github.com/proximile/proxiport/server/clients/clientdata"
)

func (al *APIListener) handleListSchedules(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	items, err := al.scheduleManager.List(ctx, req)
	if err != nil {
		al.jsonError(w, err)
		return
	}

	curUser, err := al.getUserModelForAuth(ctx)
	if err != nil {
		al.jsonError(w, err)
		return
	}

	if !curUser.IsAdmin() {
		if err := al.filterSchedulesForUser(ctx, items, curUser); err != nil {
			al.jsonError(w, err)
			return
		}
	}

	// items is already a *api.SuccessPayload; passing it through directly
	// avoids wrapping the envelope in another envelope ({"data":{"data":...}}).
	al.writeJSONResponse(w, http.StatusOK, items)
}

// filterSchedulesForUser drops any schedule in the payload that curUser is not
// allowed to see, so a non-admin user never receives another user's schedules
// (including their command/script text) via the list endpoint.
func (al *APIListener) filterSchedulesForUser(ctx context.Context, payload *api.SuccessPayload, curUser *users.User) error {
	schedules, ok := payload.Data.([]*schedule.Schedule)
	if !ok {
		return nil
	}

	clientGroups, err := al.clientGroupProvider.GetAll(ctx)
	if err != nil {
		return err
	}

	visible := make([]*schedule.Schedule, 0, len(schedules))
	for _, s := range schedules {
		if al.checkScheduleAccess(ctx, s, curUser, clientGroups) == nil {
			visible = append(visible, s)
		}
	}

	payload.Data = visible
	if payload.Meta != nil {
		payload.Meta = api.NewMeta(len(visible))
	}
	return nil
}

// checkScheduleAccess verifies that curUser is allowed to view and manage the
// given stored schedule. Admins may access any schedule; a non-admin user may
// only access schedules it created and must additionally have access to all of
// the schedule's target clients.
func (al *APIListener) checkScheduleAccess(ctx context.Context, storedSchedule *schedule.Schedule, curUser *users.User, clientGroups []*cgroups.ClientGroup) error {
	if curUser.IsAdmin() {
		return nil
	}

	if storedSchedule.CreatedBy != curUser.GetUsername() {
		return errors2.APIError{
			Message:    "You are not allowed to access this schedule.",
			HTTPStatus: http.StatusForbidden,
		}
	}

	orderedClients, _, err := al.getOrderedClientsWithValidation(ctx, storedSchedule)
	if err != nil {
		return err
	}

	return al.clientService.CheckClientsAccess(orderedClients, curUser, clientGroups)
}

// authorizeScheduleAccess loads the stored schedule by id and verifies that the
// current user is allowed to view and manage it, returning the stored schedule.
func (al *APIListener) authorizeScheduleAccess(req *http.Request, idStr string) (*schedule.Schedule, error) {
	ctx := req.Context()

	storedSchedule, err := al.scheduleManager.Get(ctx, idStr)
	if err != nil {
		return nil, err
	}
	if storedSchedule == nil {
		return nil, errors2.APIError{
			Message:    fmt.Sprintf("Cannot find a schedule by the provided id: %s", idStr),
			HTTPStatus: http.StatusNotFound,
		}
	}

	curUser, err := al.getUserModelForAuth(ctx)
	if err != nil {
		return nil, err
	}

	clientGroups, err := al.clientGroupProvider.GetAll(ctx)
	if err != nil {
		return nil, err
	}

	if err := al.checkScheduleAccess(ctx, storedSchedule, curUser, clientGroups); err != nil {
		return nil, err
	}

	return storedSchedule, nil
}

func (al *APIListener) prepareHandleSchedules(req *http.Request) (schedule.Schedule, string, []*clientdata.Client, error) {
	var scheduleInput schedule.Schedule
	var orderedClients []*clientdata.Client
	var username string
	ctx := req.Context()
	err := parseRequestBody(req.Body, &scheduleInput)
	if err != nil {
		return scheduleInput, username, orderedClients, err
	}

	curUser, err := al.getUserModelForAuth(req.Context())
	if err != nil {
		return scheduleInput, username, orderedClients, err
	}
	username = curUser.GetUsername()

	orderedClients, _, err = al.getOrderedClientsWithValidation(ctx, &scheduleInput)
	if err != nil {
		return scheduleInput, username, orderedClients, err
	}

	clientGroups, err := al.clientGroupProvider.GetAll(ctx)
	if err != nil {
		return scheduleInput, username, orderedClients, err
	}
	err = al.clientService.CheckClientsAccess(orderedClients, curUser, clientGroups)
	if err != nil {
		return scheduleInput, username, orderedClients, err
	}
	return scheduleInput, username, orderedClients, nil
}

func (al *APIListener) handlePostSchedules(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	scheduleInput, username, orderedClients, err := al.prepareHandleSchedules(req)
	if err != nil {
		al.jsonError(w, err)
		return
	}

	storedValue, err := al.scheduleManager.Create(ctx, &scheduleInput, username)
	if err != nil {
		al.jsonError(w, err)
		return
	}

	al.auditLog.Entry(auditlog.ApplicationSchedule, auditlog.ActionCreate).
		WithHTTPRequest(req).
		WithRequest(scheduleInput).
		WithResponse(storedValue).
		WithID(storedValue.ID).
		SaveForMultipleClients(orderedClients)

	al.writeJSONResponse(w, http.StatusCreated, api.NewSuccessPayload(storedValue))
}

func (al *APIListener) handleUpdateSchedule(w http.ResponseWriter, req *http.Request) {
	ctx := req.Context()
	vars := mux.Vars(req)
	idStr, ok := vars["schedule_id"]
	if !ok {
		al.jsonErrorResponseWithTitle(w, http.StatusBadRequest, "Schedule ID is not provided")
		return
	}

	// Ensure the caller may access the schedule being replaced before applying
	// the update from the request body.
	if _, err := al.authorizeScheduleAccess(req, idStr); err != nil {
		al.jsonError(w, err)
		return
	}

	scheduleInput, _, orderedClients, err := al.prepareHandleSchedules(req)
	if err != nil {
		al.jsonError(w, err)
		return
	}

	storedValue, err := al.scheduleManager.Update(ctx, idStr, &scheduleInput)
	if err != nil {
		al.jsonError(w, err)
		return
	}

	al.auditLog.Entry(auditlog.ApplicationSchedule, auditlog.ActionUpdate).
		WithHTTPRequest(req).
		WithRequest(scheduleInput).
		WithResponse(storedValue).
		WithID(idStr).
		SaveForMultipleClients(orderedClients)

	al.writeJSONResponse(w, http.StatusOK, api.NewSuccessPayload(storedValue))
}

func (al *APIListener) handleGetSchedule(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	idStr := vars["schedule_id"]
	if idStr == "" {
		al.jsonError(w, errors2.APIError{
			Err:        errors.New("empty schedule id provided"),
			HTTPStatus: http.StatusBadRequest,
		})
		return
	}

	foundSchedule, err := al.authorizeScheduleAccess(req, idStr)
	if err != nil {
		al.jsonError(w, err)
		return
	}

	al.writeJSONResponse(w, http.StatusOK, api.NewSuccessPayload(foundSchedule))
}

func (al *APIListener) handleDeleteSchedule(w http.ResponseWriter, req *http.Request) {
	vars := mux.Vars(req)
	idStr := vars["schedule_id"]
	if idStr == "" {
		al.jsonError(w, errors2.APIError{
			Err:        errors.New("empty schedule id provided"),
			HTTPStatus: http.StatusBadRequest,
		})
		return
	}

	if _, err := al.authorizeScheduleAccess(req, idStr); err != nil {
		al.jsonError(w, err)
		return
	}

	err := al.scheduleManager.Delete(req.Context(), idStr)
	if err != nil {
		al.jsonError(w, err)
		return
	}

	al.auditLog.Entry(auditlog.ApplicationSchedule, auditlog.ActionDelete).
		WithHTTPRequest(req).
		WithID(idStr).
		Save()

	w.WriteHeader(http.StatusNoContent)
}
