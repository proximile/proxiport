package chserver

import (
	"context"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/proximile/proxiport/server/api"
	"github.com/proximile/proxiport/server/notifications"
	"github.com/proximile/proxiport/server/routes"
	"github.com/proximile/proxiport/share/query"
)

func (al *APIListener) notificationsList(ctx context.Context, options *query.ListOptions) (*api.SuccessPayload, error) {

	// The advertised filter and sort sets live with the repository that has to
	// serve them, so a filter cannot be advertised here and be unserveable
	// there -- which is precisely what M4 was.
	err := query.ValidateListOptions(options, notifications.SupportedSorts, notifications.SupportedFilters, nil, &query.PaginationConfig{
		DefaultLimit: 10,
		MaxLimit:     100,
	})
	if err != nil {
		return nil, err
	}

	entries, err := al.notificationsStorage.List(ctx, options)
	if err != nil {
		return nil, err
	}

	count, err := al.notificationsStorage.Count(ctx, options)
	if err != nil {
		return nil, err
	}

	return &api.SuccessPayload{
		Data: entries,
		Meta: api.NewMeta(count),
	}, nil
}

func (al *APIListener) handleGetNotifications(writer http.ResponseWriter, request *http.Request) {
	ctx := request.Context()

	options := query.GetListOptions(request)
	result, err := al.notificationsList(ctx, options)
	if err != nil {
		al.jsonError(writer, err)
		return
	}

	al.writeJSONResponse(writer, http.StatusOK, result)
}

func (al *APIListener) handleGetNotificationDetails(writer http.ResponseWriter, request *http.Request) {
	ctx := request.Context()
	vars := mux.Vars(request)
	nid := vars[routes.ParamNotificationID]

	notification, found, err := al.notificationsStorage.Details(ctx, nid)
	if err != nil {
		al.jsonError(writer, err)
		return
	}

	if !found {
		al.writeJSONResponse(writer, http.StatusNotFound, nil)
		return
	}

	al.writeJSONResponse(writer, http.StatusOK, notification)
}
