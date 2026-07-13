package keyprovider

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

var testKey = []byte("0123456789abcdef0123456789abcdef") // 32 bytes

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

func TestNoneProvider(t *testing.T) {
	p := NewNone()
	if p.Type() != TypeNone {
		t.Fatalf("type = %q", p.Type())
	}
	if p.Enabled() {
		t.Fatal("none provider must be disabled")
	}
	if p.DEK() != nil {
		t.Fatal("none provider must have a nil DEK")
	}
}

func TestFileProvider_Encodings(t *testing.T) {
	cases := map[string][]byte{
		"raw32":  testKey,
		"base64": []byte(base64.StdEncoding.EncodeToString(testKey) + "\n"),
		"hex":    []byte(hex.EncodeToString(testKey) + "\n"),
		"text32": append(append([]byte{}, testKey...), '\n'), // 32-char text + newline
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			p, err := NewFileProvider(writeFile(t, name, data))
			if err != nil {
				t.Fatalf("NewFileProvider: %v", err)
			}
			if !p.Enabled() {
				t.Fatal("provider should be enabled")
			}
			if p.Type() != TypeFile {
				t.Fatalf("type = %q", p.Type())
			}
			if !bytes.Equal(p.DEK(), testKey) {
				t.Fatalf("DEK mismatch: got %x want %x", p.DEK(), testKey)
			}
		})
	}
}

func TestFileProvider_WrongLength(t *testing.T) {
	if _, err := NewFileProvider(writeFile(t, "short", []byte("too-short"))); err == nil {
		t.Fatal("a key that does not decode to 32 bytes must be rejected")
	}
}

func TestFileProvider_Missing(t *testing.T) {
	if _, err := NewFileProvider(filepath.Join(t.TempDir(), "nope.key")); err == nil {
		t.Fatal("a missing key file must be a hard error (fail closed at boot)")
	}
}

func TestEnvProvider(t *testing.T) {
	t.Setenv("PROXIPORT_TEST_DEK", base64.StdEncoding.EncodeToString(testKey))
	p, err := NewEnvProvider("PROXIPORT_TEST_DEK")
	if err != nil {
		t.Fatalf("NewEnvProvider: %v", err)
	}
	if !p.Enabled() || p.Type() != TypeEnv || !bytes.Equal(p.DEK(), testKey) {
		t.Fatalf("unexpected env provider state: enabled=%v type=%q", p.Enabled(), p.Type())
	}

	if _, err := NewEnvProvider("PROXIPORT_TEST_DEK_UNSET"); err == nil {
		t.Fatal("an unset environment variable must be rejected")
	}
}

func TestConfig_ParseAndValidate(t *testing.T) {
	keyPath := writeFile(t, "dek.key", testKey)

	t.Run("none is disabled", func(t *testing.T) {
		for _, typ := range []string{"", "none", "NONE"} {
			c := &Config{Type: typ}
			if err := c.ParseAndValidate(); err != nil {
				t.Fatalf("type=%q: %v", typ, err)
			}
			if c.Provider().Enabled() {
				t.Fatalf("type=%q should be disabled", typ)
			}
			if c.DEK() != nil {
				t.Fatalf("type=%q should have a nil DEK", typ)
			}
		}
	})

	t.Run("file enabled", func(t *testing.T) {
		c := &Config{Type: "file", KeyFile: keyPath}
		if err := c.ParseAndValidate(); err != nil {
			t.Fatal(err)
		}
		if !c.Provider().Enabled() || !bytes.Equal(c.DEK(), testKey) {
			t.Fatal("file provider should be enabled with the configured key")
		}
	})

	t.Run("file requires key_file", func(t *testing.T) {
		if err := (&Config{Type: "file"}).ParseAndValidate(); err == nil {
			t.Fatal("type=file without key_file must error")
		}
	})

	t.Run("env requires env_var", func(t *testing.T) {
		if err := (&Config{Type: "env"}).ParseAndValidate(); err == nil {
			t.Fatal("type=env without env_var must error")
		}
	})

	t.Run("unknown type", func(t *testing.T) {
		if err := (&Config{Type: "kms"}).ParseAndValidate(); err == nil {
			t.Fatal("an unknown provider type must error")
		}
	})

	t.Run("provider before parse is disabled", func(t *testing.T) {
		c := &Config{Type: "file", KeyFile: keyPath}
		if c.Provider().Enabled() {
			t.Fatal("Provider() before ParseAndValidate must be the disabled none provider")
		}
	})
}
