package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/pflag"
)

// writeClientConf writes a minimal [client] config that carries a server and
// fingerprint but deliberately NO `auth` line, so the only place a credential
// can come from is the environment / CLI flag.
func writeClientConf(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "proxiport.conf")
	body := "" +
		"[client]\n" +
		"  server = \"example.test:80\"\n" +
		"  fingerprint = \"SHA256:deadbeef\"\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	return p
}

// newPFlags builds the exact flag set the binary uses (SetPFlags), which is
// where PROXIPORT_AUTH is wired in as the --auth flag default.
func newPFlags(t *testing.T) *pflag.FlagSet {
	t.Helper()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	SetPFlags(fs)
	return fs
}

// TestEnvAuthIsHonored asserts that setting PROXIPORT_AUTH delivers the client
// credential to the decoded config when no --auth flag and no config `auth`
// line are present. This is the interactive path (overrideConfigWithCLIArgs=true).
func TestEnvAuthIsHonored(t *testing.T) {
	t.Setenv("PROXIPORT_AUTH", "clientid:secretpw")
	cfgPath := writeClientConf(t)
	fs := newPFlags(t)

	cfg, err := DecodeConfig(cfgPath, fs, true)
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if got := cfg.Client.Auth; got != "clientid:secretpw" {
		t.Fatalf("PROXIPORT_AUTH not honored: client.auth = %q, want %q", got, "clientid:secretpw")
	}
}

// TestEnvAuthIsHonoredServicePath is the non-interactive (service) path, where
// DecodeConfig is called with overrideConfigWithCLIArgs=false. A daemonized
// agent still needs its env credential.
func TestEnvAuthIsHonoredServicePath(t *testing.T) {
	t.Setenv("PROXIPORT_AUTH", "clientid:secretpw")
	cfgPath := writeClientConf(t)
	fs := newPFlags(t)

	cfg, err := DecodeConfig(cfgPath, fs, false)
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if got := cfg.Client.Auth; got != "clientid:secretpw" {
		t.Fatalf("PROXIPORT_AUTH not honored (service path): client.auth = %q, want %q", got, "clientid:secretpw")
	}
}

// TestConfigAuthStillWorks is the control: the config-file credential path,
// which the field report confirms works today, must keep working.
func TestConfigAuthStillWorks(t *testing.T) {
	_ = os.Unsetenv("PROXIPORT_AUTH")
	dir := t.TempDir()
	p := filepath.Join(dir, "proxiport.conf")
	body := "" +
		"[client]\n" +
		"  server = \"example.test:80\"\n" +
		"  fingerprint = \"SHA256:deadbeef\"\n" +
		"  auth = \"clientid:frompw\"\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	fs := newPFlags(t)

	cfg, err := DecodeConfig(p, fs, true)
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if got := cfg.Client.Auth; got != "clientid:frompw" {
		t.Fatalf("config auth broken: client.auth = %q, want %q", got, "clientid:frompw")
	}
}

// TestCompatEnvAuthName confirms the upstream RPORT_AUTH alias also delivers the
// credential (documented for compatibility).
func TestCompatEnvAuthName(t *testing.T) {
	_ = os.Unsetenv("PROXIPORT_AUTH")
	t.Setenv("RPORT_AUTH", "clientid:compatpw")
	cfgPath := writeClientConf(t)
	fs := newPFlags(t)

	cfg, err := DecodeConfig(cfgPath, fs, false)
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if got := cfg.Client.Auth; got != "clientid:compatpw" {
		t.Fatalf("RPORT_AUTH not honored: client.auth = %q, want %q", got, "clientid:compatpw")
	}
}

// TestCanonicalEnvAuthWinsOverCompat asserts PROXIPORT_AUTH takes precedence over
// the RPORT_AUTH alias when both are set.
func TestCanonicalEnvAuthWinsOverCompat(t *testing.T) {
	t.Setenv("PROXIPORT_AUTH", "clientid:canonical")
	t.Setenv("RPORT_AUTH", "clientid:compat")
	cfgPath := writeClientConf(t)
	fs := newPFlags(t)

	cfg, err := DecodeConfig(cfgPath, fs, false)
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if got := cfg.Client.Auth; got != "clientid:canonical" {
		t.Fatalf("PROXIPORT_AUTH should win over RPORT_AUTH: client.auth = %q, want %q", got, "clientid:canonical")
	}
}

// TestEnvFingerprintIsHonored guards the sibling PROXIPORT_FINGERPRINT env var,
// which suffered the same pflag-default gotcha, on the service path.
func TestEnvFingerprintIsHonored(t *testing.T) {
	_ = os.Unsetenv("PROXIPORT_AUTH")
	t.Setenv("PROXIPORT_FINGERPRINT", "SHA256:fromenv")
	dir := t.TempDir()
	p := filepath.Join(dir, "proxiport.conf")
	// config carries server + auth but NO fingerprint line.
	body := "" +
		"[client]\n" +
		"  server = \"example.test:80\"\n" +
		"  auth = \"clientid:pw\"\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write conf: %v", err)
	}
	fs := newPFlags(t)

	cfg, err := DecodeConfig(p, fs, false)
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if got := cfg.Client.Fingerprint; got != "SHA256:fromenv" {
		t.Fatalf("PROXIPORT_FINGERPRINT not honored: client.fingerprint = %q, want %q", got, "SHA256:fromenv")
	}
}

// TestFlagAuthOverridesEnv confirms an explicit --auth flag still wins over the
// environment (flag precedence must not regress when we fix env resolution).
func TestFlagAuthOverridesEnv(t *testing.T) {
	t.Setenv("PROXIPORT_AUTH", "clientid:fromenv")
	cfgPath := writeClientConf(t)
	fs := newPFlags(t)
	if err := fs.Parse([]string{"--auth", "clientid:fromflag"}); err != nil {
		t.Fatalf("parse: %v", err)
	}

	cfg, err := DecodeConfig(cfgPath, fs, true)
	if err != nil {
		t.Fatalf("DecodeConfig: %v", err)
	}
	if got := cfg.Client.Auth; got != "clientid:fromflag" {
		t.Fatalf("--auth did not override env: client.auth = %q, want %q", got, "clientid:fromflag")
	}
}
