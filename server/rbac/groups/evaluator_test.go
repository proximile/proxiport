package groups

import (
	"context"
	"testing"
)

func TestNewEvaluatorReturnsNoop(t *testing.T) {
	e := NewEvaluator()
	if e == nil {
		t.Fatal("NewEvaluator() returned nil")
	}
	if _, ok := e.(NoopEvaluator); !ok {
		t.Fatalf("NewEvaluator() = %T, want NoopEvaluator", e)
	}
}

func TestNoopEvaluatorPermitsEverything(t *testing.T) {
	e := NewEvaluator()
	cases := []struct {
		name     string
		action   Action
		resource Resource
		who      Principal
	}{
		{
			name:   "global action",
			action: Action("clients.read"),
		},
		{
			name:     "destructive action on a tagged client",
			action:   Action("clients.delete"),
			resource: Resource{Kind: "client", ID: "c-123", Tags: map[string]string{"env": "prod"}},
			who:      Principal{Username: "alice", Groups: []string{"operators"}},
		},
		{
			name:   "unknown action",
			action: Action("totally.made.up"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := e.Permit(context.Background(), tc.action, tc.resource, tc.who)
			if err != nil {
				t.Fatalf("Permit returned error: %v", err)
			}
			if !d.Allow {
				t.Errorf("Decision.Allow = false, want true (noop)")
			}
		})
	}
}
