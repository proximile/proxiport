package chconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// repoRoot walks up from this package until it finds the go.mod at the module
// root.
func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		require.NotEqual(t, dir, parent, "walked to the filesystem root without finding go.mod")
		dir = parent
	}

	t.Fatal("go.mod not found within 10 parent directories")
	return ""
}

// Committed .conf files must not carry the published example-config
// placeholders, because validateNoPlaceholderSecrets refuses to start on them.
//
// This is a regression guard with history: every bdd/**/proxiportd.conf shipped
// `auth = "clientAuth1:1234"` and `auth = "admin:foobaz"`, so once the
// placeholder refusal landed the daemon exited before printing "API Listening"
// and all four end-to-end suites failed. Nothing reported it -- the CI step
// carried continue-on-error and the local gate skipped ./bdd/... -- so it sat
// broken. A unit test catches it in seconds even if those gates are ever
// disarmed again.
func TestCommittedConfigsCarryNoPlaceholderSecrets(t *testing.T) {
	root := repoRoot(t)

	var checked []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Vendored and build output holds third-party configs we do not own.
			switch info.Name() {
			case ".git", "node_modules", "vendor", "build":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".conf" {
			return nil
		}
		// The example configs are the source of these placeholders; the
		// packaging scripts substitute them at install time.
		if strings.HasSuffix(info.Name(), ".example.conf") {
			return nil
		}

		// G304: path comes from filepath.Walk over this repository, not from
		// any caller-supplied input.
		body, err := os.ReadFile(path) //nolint:gosec
		if err != nil {
			return err
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			rel = path
		}
		checked = append(checked, rel)

		for _, p := range placeholderSecrets {
			assert.NotContainsf(t, string(body), p.value,
				"%s carries the %s placeholder from the example config; proxiportd refuses "+
					"to start on it, so any suite using this config cannot run", rel, p.setting)
		}
		return nil
	})
	require.NoError(t, err)

	// Without this the test passes vacuously the moment the fixtures move or
	// the walk stops matching -- which is the same shape of blind spot that let
	// the original breakage survive.
	require.NotEmpty(t, checked, "found no committed .conf files to check")
	assert.GreaterOrEqual(t, len(checked), 8,
		"expected at least the 8 bdd/ config files, found %d: %v", len(checked), checked)
}
