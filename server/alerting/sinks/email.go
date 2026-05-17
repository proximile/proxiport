// TODO(v0.2): Replace this stub with a real SMTP sink. The likely
// implementation reuses share/email or jordan-wright/email (already
// a project dependency for the notifications package). Honor
// EmailSinkConfig.StartTLS and read the password from the env var
// named in EmailSinkConfig.PasswordEnv.

package sinks

import (
	"context"

	"github.com/proximile/proxiport/server/alerting"
)

// Email is the SMTP delivery sink. While the package is scaffolding,
// Send always returns alerting.ErrNotConfigured.
type Email struct {
	Config *alerting.EmailSinkConfig
}

// Name returns "email".
func (Email) Name() string { return "email" }

// Send always returns alerting.ErrNotConfigured.
func (Email) Send(ctx context.Context, alert alerting.Alert) error {
	return alerting.ErrNotConfigured
}
