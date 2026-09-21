package auditlog

import (
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/db/migration/auditlog"
	"github.com/proximile/proxiport/db/sqlite"
	"github.com/proximile/proxiport/server/api/users"
	"github.com/proximile/proxiport/server/auditlog/config"
	"github.com/proximile/proxiport/share/query"
)

// newAuditLogWithEntries returns an audit log holding one entry per supplied
// instant. The entries are written through the real Save path, so they carry
// whatever text format this server actually stores.
func newAuditLogWithEntries(t *testing.T, at ...time.Time) *AuditLog {
	t.Helper()

	db, err := sqlite.New(":memory:", auditlog.AssetNames(), auditlog.Asset, DataSourceOptions)
	require.NoError(t, err)

	provider := &SQLiteProvider{db: db, converter: query.NewSQLConverter(db.DriverName())}
	log := &AuditLog{config: config.Config{Enable: true}, provider: provider}
	t.Cleanup(func() { require.NoError(t, log.Close()) })

	for i, ts := range at {
		require.NoError(t, provider.Save(&Entry{
			Timestamp:   ts,
			Application: ApplicationLibraryScript,
			Action:      ActionCreate,
			Username:    "bob",
			ID:          strconv.Itoa(i),
		}))
	}

	return log
}

// TestAuditTimestampFiltersFindTheEntriesTheyName is L13.
//
// The audit log advertises timestamp[gt|lt|since|until] but passed the caller's
// value into the statement untouched. Entries are stored with a space between
// date and time and an offset suffix, so an ISO-8601 value -- the form the rest
// of this API's documentation uses -- compared 'T' (0x54) against ' ' (0x20)
// and put every row of that day BELOW the filter. A `since midnight` query
// answered HTTP 200 with an empty list, which reads as "nothing was logged".
func TestAuditTimestampFiltersFindTheEntriesTheyName(t *testing.T) {
	morning := time.Date(2026, 9, 15, 9, 0, 0, 0, time.UTC)
	evening := time.Date(2026, 9, 15, 21, 0, 0, 0, time.UTC)
	nextDay := time.Date(2026, 9, 16, 9, 0, 0, 0, time.UTC)

	testCases := []struct {
		name      string
		filter    string
		wantCount int
	}{
		{"since midnight, RFC 3339", "filter[timestamp][since]=2026-09-15T00:00:00Z", 3},
		{"since midnight, offset form", "filter[timestamp][since]=2026-09-15T02:00:00+02:00", 3},
		{"since midnight, space form", "filter[timestamp][since]=2026-09-15 00:00:00", 3},
		{"since midnight, date only", "filter[timestamp][since]=2026-09-15", 3},
		{"since midday", "filter[timestamp][since]=2026-09-15T12:00:00Z", 2},
		{"until midday", "filter[timestamp][until]=2026-09-15T12:00:00Z", 1},
		{"after midday", "filter[timestamp][gt]=2026-09-15T12:00:00Z", 2},
		{"before midday", "filter[timestamp][lt]=2026-09-15T12:00:00Z", 1},
		{"until a past instant matches nothing", "filter[timestamp][until]=2020-01-01T00:00:00Z", 0},
		// 1789473600 is 2026-09-15T12:00:00Z -- the same instant as the
		// "since midday" case above, written the way the monitoring endpoints
		// take it.
		{"unix seconds are accepted too", "filter[timestamp][since]=1789473600", 2},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			log := newAuditLogWithEntries(t, morning, evening, nextDay)

			req := httptest.NewRequest("GET", "/auditlog?"+encodeFilter(t, tc.filter), nil)
			payload, err := log.List(req, &users.User{Username: "root", Groups: []string{users.Administrators}})
			require.NoError(t, err)

			entries, ok := payload.Data.([]*Entry)
			require.True(t, ok)
			assert.Len(t, entries, tc.wantCount)
			assert.Equal(t, tc.wantCount, payload.Meta.Count, "the count must agree with the page")
		})
	}
}

// TestAuditTimestampFiltersWorkOnRowsWrittenInLocalTime is the half that needs
// DATETIME() rather than value normalisation: every deployment already has rows
// stored in the server's local zone, carrying an offset that itself changes
// across DST, and an investigation has to be able to ask about those.
func TestAuditTimestampFiltersWorkOnRowsWrittenInLocalTime(t *testing.T) {
	berlin, err := time.LoadLocation("Europe/Berlin")
	require.NoError(t, err)

	// The same two instants an old server would have written with +02:00 and
	// +01:00 respectively, spanning the autumn DST change.
	beforeDST := time.Date(2026, 10, 25, 0, 30, 0, 0, berlin) // 2026-10-24 22:30 UTC
	afterDST := time.Date(2026, 10, 25, 23, 30, 0, 0, berlin) // 2026-10-25 22:30 UTC
	require.Equal(t, "+0200", beforeDST.Format("-0700"))
	require.Equal(t, "+0100", afterDST.Format("-0700"))

	log := newAuditLogWithEntries(t, beforeDST, afterDST)

	req := httptest.NewRequest("GET",
		"/auditlog?"+encodeFilter(t, "filter[timestamp][since]=2026-10-25T00:00:00Z"), nil)
	payload, err := log.List(req, &users.User{Username: "root", Groups: []string{users.Administrators}})
	require.NoError(t, err)

	entries, ok := payload.Data.([]*Entry)
	require.True(t, ok)
	assert.Len(t, entries, 1, "only the entry after 2026-10-25T00:00Z is in the window")
}

func TestAuditTimestampFilterRejectsAValueItCannotParse(t *testing.T) {
	log := newAuditLogWithEntries(t, time.Now().UTC())

	req := httptest.NewRequest("GET",
		"/auditlog?"+encodeFilter(t, "filter[timestamp][since]=last tuesday"), nil)
	_, err := log.List(req, &users.User{Username: "root", Groups: []string{users.Administrators}})

	require.Error(t, err, "an unparseable value must be a 400, not an empty result set")
	assert.Contains(t, err.Error(), "illegal timestamp filter value")
}

// TestAuditEntriesAreStoredInUTC keeps the write side honest: the DATETIME()
// comparison rescues old rows, but new ones should not need rescuing.
func TestAuditEntriesAreStoredInUTC(t *testing.T) {
	log := &AuditLog{config: config.Config{Enable: true}}

	entry := log.Entry(ApplicationLibraryScript, ActionCreate)
	require.NotNil(t, entry)
	assert.Equal(t, "UTC", entry.Timestamp.Location().String())
}

// encodeFilter percent-encodes the value half of a single filter parameter so
// that a '+' in an offset survives the query string.
func encodeFilter(t *testing.T, raw string) string {
	t.Helper()

	for i := 0; i < len(raw); i++ {
		if raw[i] == '=' {
			return raw[:i+1] + url.QueryEscape(raw[i+1:])
		}
	}
	t.Fatalf("filter %q has no value", raw)
	return ""
}
