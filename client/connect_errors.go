package chclient

import (
	"errors"
	"fmt"
)

// errEndpointRefused marks a failure that the server itself produced, as
// opposed to a failure of the path taken to reach it.
//
// The distinction decides whether the agent may try the next candidate. The
// SSH credential is built once in NewClient and is the same for every server
// and every transport, so a rejection at one candidate is a rejection at all of
// them — continuing the sweep cannot succeed, and with a transport chain
// configured it would walk the agent down to a clearnet dial because one
// character of `auth` is wrong. A refusal ends the sweep.
var errEndpointRefused = errors.New("the server refused this agent")

// transportFailure is a failure of the egress path: the candidate never got as
// far as an authenticated session with the server. These are the only failures
// that advance the chain.
type transportFailure struct {
	Transport string
	Server    string
	Err       error
}

func (e *transportFailure) Error() string {
	return fmt.Sprintf("via %s to %s: %v", e.Transport, e.Server, e.Err)
}

func (e *transportFailure) Unwrap() error { return e.Err }
