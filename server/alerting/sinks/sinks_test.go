package sinks

import (
	"context"
	"errors"
	"testing"

	"github.com/proximile/proxiport/server/alerting"
)

func TestAllSinksReturnErrNotConfigured(t *testing.T) {
	ctx := context.Background()
	a := alerting.Alert{
		ID:       "t-1",
		Severity: alerting.SeverityInfo,
		Source:   "test",
		Summary:  "stub send",
	}

	sinks := []alerting.Sink{
		Email{},
		Slack{},
		Webhook{},
	}
	for _, s := range sinks {
		t.Run(s.Name(), func(t *testing.T) {
			if err := s.Send(ctx, a); !errors.Is(err, alerting.ErrNotConfigured) {
				t.Errorf("%s.Send: got %v, want ErrNotConfigured", s.Name(), err)
			}
		})
	}
}
