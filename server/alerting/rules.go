// TODO(v0.2): Implement rule evaluation. The Rule shape below is the
// minimum needed for a thresholded match against a monitoring
// measurement; the real engine will also need:
//   - hysteresis (don't refire within X seconds),
//   - resolution alerts ("OK" follow-ups when a firing rule clears),
//   - rule groups so operators can mute a whole family at once.
// See docs/alerting-design.md.

package alerting

import "time"

// Comparator is the operator side of a threshold check.
type Comparator string

// Recognized comparators.
const (
	ComparatorGreater  Comparator = ">"
	ComparatorGreaterE Comparator = ">="
	ComparatorLess     Comparator = "<"
	ComparatorLessE    Comparator = "<="
	ComparatorEqual    Comparator = "=="
	ComparatorNotEqual Comparator = "!="
)

// Rule describes one alert condition. The intended evaluation model
// is: every time a Measurement arrives, walk the rule table; for
// rules whose Metric matches, apply Compare(Threshold) against the
// new value; if the comparison holds continuously for For, fire an
// Alert.
type Rule struct {
	// ID is a stable identifier surfaced on every Alert produced by
	// this rule.
	ID string
	// Description is operator documentation.
	Description string
	// Metric is the measurement to evaluate (e.g. "cpu.usage",
	// "disk.used.percent", "client.offline.seconds").
	Metric string
	// Compare is the comparator applied between the live value and
	// Threshold.
	Compare Comparator
	// Threshold is the right-hand side of the comparison.
	Threshold float64
	// For is how long the comparison must continuously hold before an
	// alert fires. Zero means "fire on first match".
	For time.Duration
	// Severity is the severity stamped onto produced Alerts.
	Severity Severity
	// Labels are merged into Alert.Labels at fire time.
	Labels map[string]string
	// Sinks restricts dispatch to this subset of configured sinks.
	// Empty means "all sinks".
	Sinks []string
}

// Measurement is one data point fed into the rule engine. The shape
// is kept loose so the same Rule can match against any source
// (monitoring agent, scheduler, client-online tracker).
type Measurement struct {
	// Metric is the metric name, matched against Rule.Metric.
	Metric string
	// Value is the data point.
	Value float64
	// ClientID, when non-empty, scopes the measurement to one client.
	ClientID string
	// Labels are merged into produced Alert.Labels.
	Labels map[string]string
	// At is the measurement's wall-clock timestamp.
	At time.Time
}
