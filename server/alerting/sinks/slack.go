// TODO(v0.2): Replace this stub with a real Slack-incoming-webhook
// sink. Implementation hints:
//   - read the webhook URL from os.Getenv(Config.WebhookURLEnv) and
//     refuse to start if unset (don't accept a literal URL in the
//     config file so secrets stay out of dotfiles);
//   - render Severity into Slack block color;
//   - use a context-bound http.Client with a short timeout.

package sinks

import (
	"context"

	"github.com/proximile/proxiport/server/alerting"
)

// Slack is the Slack incoming-webhook sink. While the package is
// scaffolding, Send always returns alerting.ErrNotConfigured.
type Slack struct {
	Config *alerting.SlackSinkConfig
}

// Name returns "slack".
func (Slack) Name() string { return "slack" }

// Send always returns alerting.ErrNotConfigured.
func (Slack) Send(ctx context.Context, alert alerting.Alert) error {
	return alerting.ErrNotConfigured
}
