// TODO(v0.2): Add a YAML loader for Config and the RuleFile shape
// below. Picking YAML over JSON to keep operator-edited policy files
// readable; see docs/rbac-design.md for the rationale and example
// files.

package groups

// Config holds operator-supplied RBAC settings. While the package is
// scaffolding, no loader populates this; the struct exists to pin
// down the field set a future loader will use.
type Config struct {
	// PolicyFile is the absolute path to a YAML policy document.
	// When empty, the NoopEvaluator is used and every action is
	// permitted.
	PolicyFile string `mapstructure:"policy_file"`

	// DefaultEffect controls what Permit() returns when no rule
	// matches. Must be "allow" or "deny". When empty, "deny" is
	// assumed once a real engine ships.
	DefaultEffect string `mapstructure:"default_effect"`

	// ReloadOnChange, when true, asks the evaluator to watch
	// PolicyFile and rebuild its rule table on every change. When
	// false, policy is loaded once at server start.
	ReloadOnChange bool `mapstructure:"reload_on_change"`
}

// RuleFile is the on-disk shape of a policy document. It is
// canonicalised here so the loader, the evaluator, and the design
// doc all reference the same field names.
//
// Example YAML (illustrative; not yet parsed):
//
//	version: 1
//	default_effect: deny
//	rules:
//	  - id: operators-can-tunnel
//	    description: "Operators can open tunnels to prod clients."
//	    effect: allow
//	    principals:
//	      groups: [operators]
//	    actions: [tunnels.create, tunnels.read]
//	    resources:
//	      kind: client
//	      tag_match:
//	        env: prod
//	  - id: read-only-everyone
//	    effect: allow
//	    actions: [clients.read, clientgroups.read]
type RuleFile struct {
	Version       int        `mapstructure:"version" yaml:"version"`
	DefaultEffect string     `mapstructure:"default_effect" yaml:"default_effect"`
	Rules         []RuleSpec `mapstructure:"rules" yaml:"rules"`
}

// RuleSpec is one entry in a RuleFile.
type RuleSpec struct {
	// ID is a stable identifier used in audit logs and surfaced in
	// Decision.MatchedRule.
	ID string `mapstructure:"id" yaml:"id"`
	// Description is operator documentation; the evaluator ignores it.
	Description string `mapstructure:"description" yaml:"description"`
	// Effect is "allow" or "deny". Deny rules win over allow rules.
	Effect string `mapstructure:"effect" yaml:"effect"`
	// Principals constrains which actors the rule applies to.
	Principals PrincipalSelector `mapstructure:"principals" yaml:"principals"`
	// Actions is the list of action verbs (Action) the rule covers.
	Actions []string `mapstructure:"actions" yaml:"actions"`
	// Resources constrains which resources the rule applies to.
	Resources ResourceSelector `mapstructure:"resources" yaml:"resources"`
}

// PrincipalSelector matches a Principal. An empty selector matches
// every Principal.
type PrincipalSelector struct {
	Usernames []string `mapstructure:"usernames" yaml:"usernames"`
	Groups    []string `mapstructure:"groups" yaml:"groups"`
}

// ResourceSelector matches a Resource. An empty selector matches
// every Resource.
type ResourceSelector struct {
	Kind     string            `mapstructure:"kind" yaml:"kind"`
	IDs      []string          `mapstructure:"ids" yaml:"ids"`
	TagMatch map[string]string `mapstructure:"tag_match" yaml:"tag_match"`
}
