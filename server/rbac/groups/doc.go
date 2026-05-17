// Package groups provides extended role-based access control for
// ProxiPort, layered on top of the simple group ACLs already enforced
// by server/clientsauth and server/api/users.
//
// The package is scaffolding. The default evaluator returned by
// NewEvaluator() is a NoopEvaluator that permits every action; see
// evaluator.go. The intended model, design rationale, and milestone
// plan live in docs/rbac-design.md in the workspace.
//
// The existing simple model in server/clientsauth treats group
// membership as a flat allow-list keyed by client ID. This package is
// designed to extend that model with:
//
//   - per-action verbs (read, tunnel, command, schedule, ...)
//   - resource scoping by client tag and by client group
//   - explicit deny rules that override allows
//   - principals sourced either from the local users table or from
//     OIDC group claims (see server/auth/oidc)
//
// Nothing here changes existing behavior. Callers that want the new
// model will explicitly instantiate a Policy and pass it to
// Evaluator.
package groups
