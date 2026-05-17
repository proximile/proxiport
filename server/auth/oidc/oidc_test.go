package oidc

import (
	"context"
	"errors"
	"testing"
)

func TestNewReturnsDisabledProvider(t *testing.T) {
	p := New()
	if p == nil {
		t.Fatal("New() returned nil Provider")
	}
	if got, want := p.Name(), "disabled"; got != want {
		t.Fatalf("Name() = %q, want %q", got, want)
	}
}

func TestDisabledProviderReturnsErrDisabled(t *testing.T) {
	p := New()

	if _, err := p.AuthCodeURL("state", "nonce"); !errors.Is(err, ErrDisabled) {
		t.Errorf("AuthCodeURL: got %v, want ErrDisabled", err)
	}
	if _, err := p.Exchange(context.Background(), "code", "verifier"); !errors.Is(err, ErrDisabled) {
		t.Errorf("Exchange: got %v, want ErrDisabled", err)
	}
	if _, err := p.UserInfo(context.Background(), "access-token"); !errors.Is(err, ErrDisabled) {
		t.Errorf("UserInfo: got %v, want ErrDisabled", err)
	}
}

func TestConfigValidateIsNoop(t *testing.T) {
	// Validate() is intentionally a no-op until v0.2; pin the
	// behavior so a future tightening is a deliberate test break.
	cfg := &Config{}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Config{}.Validate() = %v, want nil", err)
	}
}
