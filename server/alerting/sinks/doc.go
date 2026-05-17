// Package sinks holds stub implementations of alerting.Sink. Each
// file in this package (email.go, slack.go, webhook.go) defines a
// type that satisfies the Sink interface from server/alerting but
// returns ErrNotConfigured for Send. Real implementations land in
// v0.2; the milestone plan is in docs/alerting-design.md.
package sinks
