// TODO(v0.2): Define the concrete policy-rule struct (verb, resource
// selector, effect) and a loader that reads from Config.PolicyFile.
// The Policy interface below is the contract every backend (file,
// database, remote PDP) has to satisfy. See docs/rbac-design.md for
// the agreed verb vocabulary and resource-selector grammar.

package groups

import (
	"context"
	"errors"
)

// Sentinel errors returned by the RBAC surface.
var (
	// ErrPolicyNotLoaded is returned by a Policy whose backing store
	// has not been initialized. Callers should treat this as a
	// configuration error.
	ErrPolicyNotLoaded = errors.New("rbac: policy not loaded")

	// ErrUnknownAction is returned when an action verb is not part of
	// the agreed vocabulary. The vocabulary lives in
	// docs/rbac-design.md.
	ErrUnknownAction = errors.New("rbac: unknown action")
)

// Action is the verb being attempted ("clients.read", "tunnels.create",
// "commands.execute", ...). The string form keeps the type wire- and
// config-friendly.
type Action string

// Resource identifies the object being acted on. A zero-value
// Resource means "global / no specific object" (e.g. "list all
// clients").
type Resource struct {
	// Kind is the resource family ("client", "client_group",
	// "tunnel", "schedule", ...).
	Kind string
	// ID is the resource's stable identifier within Kind, when
	// applicable.
	ID string
	// Tags are arbitrary key/value labels carried on the resource,
	// used for selector matching.
	Tags map[string]string
}

// Principal is the actor whose authority is being checked. It does
// not embed a *users.User to avoid pulling the heavyweight user
// package into the RBAC engine.
type Principal struct {
	// Username is the resolved local username, when one exists.
	Username string
	// Groups are the principal's group memberships, sourced from the
	// local user-groups table and/or from an OIDC group claim.
	Groups []string
	// Attributes carries free-form labels (e.g. "auth_method":"oidc")
	// that policies can match on.
	Attributes map[string]string
}

// Decision is the structured result of a policy evaluation.
type Decision struct {
	// Allow is true when the action is permitted.
	Allow bool
	// MatchedRule is a stable identifier for the rule that produced
	// the decision (e.g. "rule:operators-can-tunnel"). Empty when no
	// rule matched and the default applied.
	MatchedRule string
	// Reason is a short human-readable explanation, suitable for an
	// audit-log line.
	Reason string
}

// Policy is the read-side of an RBAC backend. Implementations may
// hold an in-memory ruleset, query a database, or call out to a
// remote policy decision point.
type Policy interface {
	// Permit returns a Decision for the (action, resource, principal)
	// triple. Implementations MUST be safe for concurrent use.
	Permit(ctx context.Context, action Action, resource Resource, principal Principal) (Decision, error)
}
