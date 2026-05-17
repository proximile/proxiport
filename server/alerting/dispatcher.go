// TODO(v0.2): Replace disabledDispatcher with a real fan-out
// dispatcher that:
//   - reads sinks from Config,
//   - resolves each Alert against the configured Rules,
//   - calls every matching sink with a context-bound timeout,
//   - retries with capped exponential backoff per sink,
//   - records delivery state somewhere durable so a restart does
//     not silently drop in-flight alerts.
// See docs/alerting-design.md for the agreed delivery semantics.

package alerting

import (
	"context"
	"errors"
	"time"
)

// Sentinel errors returned by the alerting surface.
var (
	// ErrDisabled is returned by the default disabledDispatcher when
	// no sinks are configured. Callers should treat this as
	// "alerting is off".
	ErrDisabled = errors.New("alerting: dispatcher is disabled")

	// ErrNotConfigured is returned by a Sink whose required config
	// fields (SMTP host, Slack webhook URL, ...) are empty.
	ErrNotConfigured = errors.New("alerting: sink is not configured")

	// ErrDeliveryFailed is returned by a Sink whose underlying
	// transport rejected the alert after the configured retry budget.
	ErrDeliveryFailed = errors.New("alerting: sink delivery failed")
)

// Severity is the operator-facing severity of an Alert.
type Severity string

// Recognized severities. Anything outside this set will be coerced
// to SeverityInfo by a real dispatcher.
const (
	SeverityInfo     Severity = "info"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

// Alert is one message ready for dispatch. It deliberately mirrors
// the Prometheus / Alertmanager shape so operators familiar with that
// vocabulary feel at home.
type Alert struct {
	// ID is a stable identifier for this alert instance. The same
	// alert firing twice in a row should reuse the same ID so sinks
	// can dedupe.
	ID string
	// Severity is the alert's severity.
	Severity Severity
	// Source identifies the producer (e.g. "monitoring.cpu",
	// "client.offline", "schedule.failed").
	Source string
	// Summary is a one-line human-readable summary.
	Summary string
	// Body is the long-form alert text. Sinks that have a length
	// limit (Slack) may truncate; sinks that support rich text
	// (email) may render as Markdown.
	Body string
	// Labels are free-form key/value tags. Rules can match on them
	// and sinks can use them for routing.
	Labels map[string]string
	// FiredAt is the wall-clock time the alert was generated.
	FiredAt time.Time
}

// Dispatcher routes an Alert to all matching sinks.
type Dispatcher interface {
	// Notify dispatches alert. The returned error reflects the
	// dispatcher's own state (not configured, context canceled);
	// per-sink failures are recorded internally and not surfaced
	// here, so a single broken sink does not break the call site.
	Notify(ctx context.Context, alert Alert) error
}

// Sink is one delivery channel.
type Sink interface {
	// Name returns a stable identifier ("email", "slack", "webhook").
	Name() string
	// Send delivers alert. Implementations should respect ctx for
	// cancellation. Real implementations are responsible for their
	// own retry/backoff.
	Send(ctx context.Context, alert Alert) error
}

// NewDispatcher returns the default Dispatcher. While the package is
// scaffolding, this is always a disabledDispatcher.
func NewDispatcher() Dispatcher {
	return disabledDispatcher{}
}

// disabledDispatcher is the zero-config Dispatcher. Every Notify call
// returns ErrDisabled.
type disabledDispatcher struct{}

// Notify always returns ErrDisabled.
func (disabledDispatcher) Notify(ctx context.Context, alert Alert) error {
	return ErrDisabled
}
