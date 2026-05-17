// TODO(v0.2): Replace NoopEvaluator with a real evaluator that loads
// a Config.PolicyFile (YAML), compiles it into an internal rule
// table, and evaluates Permit() in O(rules * matchers). The Noop
// stub keeps the public type stable while the real engine is built.
//
// Two design decisions still open (see docs/rbac-design.md):
//   - Default-deny vs. default-permit when no rule matches.
//   - Whether to cache compiled selectors per request to keep p99
//     evaluation under a few hundred microseconds.

package groups

import "context"

// Evaluator is the small façade callers use to make access decisions.
// It exists so HTTP middleware can hold one Evaluator rather than a
// raw Policy plus its own bookkeeping.
type Evaluator interface {
	Policy
}

// NoopEvaluator is the default Evaluator. It permits every action.
// This is the scaffolding behavior — it lets the rest of the server
// compile against the Evaluator interface without changing any
// authorization semantics.
type NoopEvaluator struct{}

// NewEvaluator returns the default Evaluator. While the package is
// scaffolding, this is always a NoopEvaluator; once a real engine
// lands the constructor will take a *Config.
func NewEvaluator() Evaluator {
	return NoopEvaluator{}
}

// Permit always returns Allow=true with a "noop" reason.
func (NoopEvaluator) Permit(ctx context.Context, action Action, resource Resource, principal Principal) (Decision, error) {
	return Decision{
		Allow:       true,
		MatchedRule: "",
		Reason:      "noop evaluator: all actions permitted",
	}, nil
}
