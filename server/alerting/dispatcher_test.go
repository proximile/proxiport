package alerting

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewDispatcherReturnsDisabled(t *testing.T) {
	d := NewDispatcher()
	if d == nil {
		t.Fatal("NewDispatcher() returned nil")
	}
}

func TestDisabledDispatcherReturnsErrDisabled(t *testing.T) {
	d := NewDispatcher()
	a := Alert{
		ID:       "test-1",
		Severity: SeverityInfo,
		Source:   "test",
		Summary:  "test alert",
		FiredAt:  time.Now(),
	}
	err := d.Notify(context.Background(), a)
	if !errors.Is(err, ErrDisabled) {
		t.Fatalf("Notify: got %v, want ErrDisabled", err)
	}
}
