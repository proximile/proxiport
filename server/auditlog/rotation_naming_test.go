package auditlog

import (
	"os"
	"path"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/logger"
)

// TestRotatedNamesDoNotCollideAcrossADSTFallBack is L14.
//
// The rotation ticker is monotonic -- 24h from whenever the daemon started,
// never re-anchored to midnight -- while the rotated filename came from the
// LOCAL date. On the autumn fall-back the local day is 25 hours long, so a
// tick whose phase lands in local [00:00, 01:00) recurs on the same local date
// 24 real hours later, both rotations compute the same name, and os.Rename
// silently replaces the first rotated database with the second. A day of audit
// history gone with no error, and invisible to Verify, because each rotated
// file carries its own genesis-anchored HMAC chain.
func TestRotatedNamesDoNotCollideAcrossADSTFallBack(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)

	// 2026-10-25 in Europe/Berlin is 25 hours long (03:00 CEST -> 02:00 CET).
	first := time.Date(2026, 10, 25, 0, 30, 0, 0, berlin)
	second := first.Add(24 * time.Hour)

	// The premise, asserted rather than assumed: 24 real hours apart, same
	// local date.
	require.Equal(t, "2026-10-25", first.In(berlin).Format("2006-01-02"))
	require.Equal(t, "2026-10-25", second.In(berlin).Format("2006-01-02"))
	require.Equal(t, 24*time.Hour, second.Sub(first))

	// What the old local-date scheme produced. This is the defect, stated as
	// an assertion so it cannot quietly come back.
	const localDateScheme = "auditlog.2006-01-02.db"
	require.Equal(t,
		first.In(berlin).Format(localDateScheme),
		second.In(berlin).Format(localDateScheme),
		"the local-date scheme collides here -- that is the bug this test exists for")

	assert.NotEqual(t, rotatedName(first), rotatedName(second))
	assert.Equal(t, "auditlog.2026-10-24T22-30-00Z.db", rotatedName(first))
	assert.Equal(t, "auditlog.2026-10-25T22-30-00Z.db", rotatedName(second))
}

// TestRotatedNamesSortChronologically covers what pruneRotated depends on: it
// deletes the lexicographically smallest names, so lexicographic order has to
// be chronological -- including across the format change, since a deployment
// upgrading in place still has auditlog.<date>.db files on disk.
func TestRotatedNamesSortChronologically(t *testing.T) {
	names := []string{
		rotatedName(time.Date(2026, 10, 26, 0, 30, 0, 0, time.UTC)),
		"auditlog.2026-10-24.db", // written before the format changed
		rotatedName(time.Date(2026, 10, 25, 22, 30, 0, 0, time.UTC)),
		rotatedName(time.Date(2026, 10, 25, 0, 30, 0, 0, time.UTC)),
		"auditlog.2026-10-25.db", // same date as two of the instants, written first
	}
	sort.Strings(names)

	assert.Equal(t, []string{
		"auditlog.2026-10-24.db",
		"auditlog.2026-10-25.db",
		"auditlog.2026-10-25T00-30-00Z.db",
		"auditlog.2026-10-25T22-30-00Z.db",
		"auditlog.2026-10-26T00-30-00Z.db",
	}, names)

	for _, name := range names {
		matched, err := filepath.Match(rotatedGlob, name)
		require.NoError(t, err)
		assert.True(t, matched, "%s must still be matched by the prune glob", name)
	}
}

// TestRotateRefusesToOverwriteAnExistingRotatedFile is the belt to the UTC
// naming's braces: whatever the clock does, a rotation must never destroy an
// audit database that is already on disk.
func TestRotateRefusesToOverwriteAnExistingRotatedFile(t *testing.T) {
	dir := t.TempDir()
	seedEntry(t, dir, &Entry{Timestamp: time.Now().UTC(), Username: "live"})

	rotation, err := newRotationProvider(logger.NewLogger("test", logger.NewLogOutput(""), logger.LogLevelDebug), time.Hour, 0, dir, dso, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = rotation.Close() })

	// Put something exactly where this rotation is about to write.
	occupied := path.Join(dir, rotatedName(time.Now()))
	require.NoError(t, os.WriteFile(occupied, []byte("an earlier rotated database"), 0o600))

	err = rotation.rotate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "refusing to rotate over")

	content, readErr := os.ReadFile(occupied) //nolint:gosec // path built in this test
	require.NoError(t, readErr)
	assert.Equal(t, "an earlier rotated database", string(content),
		"the existing rotated database must be untouched")

	// And the live log must still be usable, not left closed by the refusal.
	require.NoError(t, rotation.Save(&Entry{Timestamp: time.Now().UTC(), Username: "after"}))
}
