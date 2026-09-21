package notifications

import (
	"github.com/proximile/proxiport/share/refs"
)

type NotificationID refs.Identifiable

type NotificationSummary struct {
	State          ProcessingState `db:"state"`
	NotificationID string          `db:"notification_id"`
	Transport      string          `db:"transport"`
	Timestamp      string          `db:"timestamp"`
	Out            string          `db:"out"`
	Err            string          `db:"err"`
}

// SupportedFilters and SupportedSorts are what GET /api/v1/notification-logs
// advertises, and therefore what the notifications log repository has to be
// able to serve. They live here, beside the store interface, rather than in the
// HTTP handler: every one of them used to be a documented HTTP 500, because the
// repository's base query ended in an ORDER BY that the query builder then
// appended WHERE and a second ORDER BY after. Keeping the advertisement and the
// implementation in one package is what lets a repository test drive the whole
// advertised set instead of a copy of it that can drift.
var (
	SupportedFilters = map[string]bool{
		"state":            true,
		"reference_id":     true,
		"transport":        true,
		"subject":          true,
		"timestamp[gt]":    true,
		"timestamp[lt]":    true,
		"timestamp[since]": true,
		"timestamp[until]": true,
	}
	SupportedSorts = map[string]bool{
		"timestamp": true,
		"state":     true,
	}
)
