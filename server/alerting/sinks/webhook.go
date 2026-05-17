// TODO(v0.2): Replace this stub with a real generic-webhook sink.
// Implementation hints:
//   - default Method to POST;
//   - serialize Alert to JSON;
//   - read the bearer token from os.Getenv(Config.AuthBearerEnv) when
//     set and add an Authorization header;
//   - apply Config.Headers verbatim (overriding the default
//     Content-Type if the operator sets one).

package sinks

import (
	"context"

	"github.com/proximile/proxiport/server/alerting"
)

// Webhook is the generic-webhook sink. While the package is
// scaffolding, Send always returns alerting.ErrNotConfigured.
type Webhook struct {
	Config *alerting.WebhookSinkConfig
}

// Name returns "webhook".
func (Webhook) Name() string { return "webhook" }

// Send always returns alerting.ErrNotConfigured.
func (Webhook) Send(ctx context.Context, alert alerting.Alert) error {
	return alerting.ErrNotConfigured
}
