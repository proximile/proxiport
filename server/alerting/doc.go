// Package alerting defines the ProxiPort alert pipeline: rules
// thresholded against monitoring data, dispatched to one or more
// notification sinks (email, Slack, generic webhook).
//
// The package is scaffolding. The default Dispatcher returned by
// NewDispatcher() is a no-op that returns ErrDisabled for every
// Notify call. Sink stubs live in server/alerting/sinks. The design
// rationale, threat model, and milestone plan live in
// docs/alerting-design.md in the workspace.
//
// This package is intentionally separate from server/notifications,
// which already exists as a generic per-user notification queue.
// Alerting is fire-on-condition and operator-facing; notifications
// is request-scoped and end-user facing. The v0.2 plan is for the
// alerting Dispatcher to enqueue into server/notifications for any
// sink that maps cleanly onto it (e.g. email) and to call sinks
// directly for the rest (Slack, webhook).
package alerting
