package auditlog

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	apierrors "github.com/proximile/proxiport/server/api/errors"
	"github.com/proximile/proxiport/server/api/users"
	"github.com/proximile/proxiport/server/auditlog/config"

	"github.com/proximile/proxiport/db/sqlite"

	"github.com/proximile/proxiport/server/api"
	"github.com/proximile/proxiport/server/clients/clientdata"
	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/query"
)

var (
	supportedFilters = map[string]bool{
		"timestamp[gt]":    true,
		"timestamp[lt]":    true,
		"timestamp[since]": true,
		"timestamp[until]": true,
		"username":         true,
		"remote_ip":        true,
		"application":      true,
		"action":           true,
		"affected_id":      true,
		"client_id":        true,
		"client_hostname":  true,
	}
	supportedSorts = map[string]bool{
		"timestamp":       true,
		"username":        true,
		"remote_ip":       true,
		"application":     true,
		"action":          true,
		"affected_id":     true,
		"client_id":       true,
		"client_hostname": true,
	}
)

type ClientGetter interface {
	GetByID(id string) (*clientdata.Client, error)
}

type Provider interface {
	io.Closer
	Save(e *Entry) error
	List(context.Context, *query.ListOptions) ([]*Entry, error)
	Count(context.Context, *query.ListOptions) (int, error)
	Verify(context.Context) (ChainVerification, error)
}

type AuditLog struct {
	logger       *logger.Logger
	clientGetter ClientGetter
	provider     Provider
	config       config.Config
}

type NotAllowedError struct {
	Msg string
}

func (e *NotAllowedError) Error() string {
	return e.Msg
}

// New builds the audit log. dek is the server data-encryption key (or nil): when
// present, the audit chain's HMAC key is derived from it so entries are
// tamper-evident. With no key the log still records entries, but the chain is
// inert and Verify reports it disabled.
func New(l *logger.Logger, cg ClientGetter, dataDir string, cfg config.Config, dataSourceOptions sqlite.DataSourceOptions, dek []byte) (*AuditLog, error) {
	a := &AuditLog{
		logger:       l,
		clientGetter: cg,
		config:       cfg,
	}

	if cfg.Enable {
		hmacKey, err := deriveAuditHMACKey(dek)
		if err != nil {
			return nil, err
		}
		if len(hmacKey) == 0 {
			l.Infof("audit log tamper-evidence is OFF: no key provider configured")
		}
		rotation, err := newRotationProvider(
			l,
			cfg.RotationPeriod(),
			cfg.Retention,
			dataDir,
			dataSourceOptions,
			hmacKey,
		)
		if err != nil {
			return nil, err
		}

		a.provider = rotation
	}

	return a, nil
}

// Verify walks the audit chain and reports whether it is intact. Returns a
// disabled result when the log is off or no key provider is configured.
func (a *AuditLog) Verify(ctx context.Context) (ChainVerification, error) {
	if a == nil || a.provider == nil {
		return ChainVerification{Enabled: false}, nil
	}
	return a.provider.Verify(ctx)
}

func (a *AuditLog) Entry(application, action string) *Entry {
	// return nil if auditlog is not initialized, Entry handles nils so we don't panic unnecessarily
	if a == nil || !a.config.Enable {
		return nil
	}

	e := &Entry{
		// UTC, like every other timestamp this project stores. A local time is
		// rendered by the driver with the host's offset, so a table written
		// across a DST change carries two different offsets and its rows are
		// not even ordered correctly by a text comparison, let alone
		// comparable with a caller's ISO-8601 value.
		Timestamp:   time.Now().UTC(),
		Application: application,
		Action:      action,

		al: a,
	}

	return e
}

func (a *AuditLog) Close() error {
	if a == nil || a.provider == nil {
		return nil
	}

	return a.provider.Close()
}

func (a *AuditLog) savePreparedEntry(e *Entry) error {
	if a.provider == nil {
		return nil
	}

	if a.config.UseIPObfuscation && e.RemoteIP != "" {
		ip := net.ParseIP(e.RemoteIP)
		if ip.To4() != nil {
			e.RemoteIP = strings.TrimSuffix(ip.Mask(net.CIDRMask(24, 32)).String(), "0") + "x"
		}
	}

	return a.provider.Save(e)
}

// listOptionsFor builds the query options a request from this user produces,
// including the filter that confines a non-admin to their own entries. It is a
// function of its own so that a test can ask for exactly the options the server
// would use -- the forced username filter is what makes the index this listing
// depends on load-bearing.
func listOptionsFor(r *http.Request, user *users.User) (*query.ListOptions, error) {
	options := query.GetListOptions(r)
	if !user.IsAdmin() {
		// Deny none-admins looking for foreign audit logs
		for _, v := range options.Filters {
			for _, col := range v.Column {
				if col == "username" {
					return nil, &NotAllowedError{"only members of group Administrators can filter by usernames"}
				}
			}
		}
		// Add a forced filter so none-admins cannot inspect what others have done
		options.Filters = append(options.Filters, query.FilterOption{
			Column: []string{"username"},
			Values: []string{user.Username},
		})
	}
	if err := query.ValidateListOptions(options, supportedSorts, supportedFilters, nil, &query.PaginationConfig{
		DefaultLimit: 10,
		MaxLimit:     100,
	}); err != nil {
		return nil, err
	}

	if err := normalizeTimestampFilters(options.Filters); err != nil {
		return nil, err
	}

	return options, nil
}

// timestampDbLayout is how SQLite's DATETIME() renders a normalised datetime,
// and therefore the one form a bound value and a stored value are certain to
// agree on.
const timestampDbLayout = "2006-01-02 15:04:05"

// timestampFilterLayouts are the forms a caller may write a timestamp in. The
// zone-less ones are read as UTC, because that is what the rest of this API
// means by a bare timestamp.
var timestampFilterLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	timestampDbLayout,
	"2006-01-02",
}

// normalizeTimestampFilters makes the audit log's timestamp filters compare as
// instants rather than as bytes.
//
// Two things were wrong, and the first one is why an incident investigation
// could reach the wrong conclusion. The caller's value went into the statement
// exactly as written, so an ISO-8601 "2026-09-15T00:00:00Z" was byte-compared
// against a stored "2026-09-15 14:30:00.123456789+02:00" -- and 'T' (0x54)
// sorts above ' ' (0x20), so every row of that day compared as LESS than the
// filter. A `since` query for today returned an empty list with HTTP 200. The
// mirror case, `until`, matched every row in the database including
// future-dated ones. Second, even for the space-separated form the stored value
// carries an offset the filter does not.
//
// So: parse whatever the caller sent, render it in UTC in SQLite's own layout,
// and have SQLite compare both sides through DATETIME(). The DATETIME() wrap is
// what keeps rows written before this change -- in local time, with an offset
// that itself moves across DST -- answering correctly.
func normalizeTimestampFilters(filters []query.FilterOption) error {
	for i := range filters {
		if !isTimestampFilter(filters[i]) {
			continue
		}

		for j, raw := range filters[i].Values {
			parsed, err := parseTimestampFilterValue(raw)
			if err != nil {
				return apierrors.APIError{Message: err.Error(), HTTPStatus: http.StatusBadRequest}
			}
			filters[i].Values[j] = parsed.UTC().Format(timestampDbLayout)
		}
		filters[i].CompareFunc = "DATETIME"
	}

	return nil
}

func isTimestampFilter(fo query.FilterOption) bool {
	if len(fo.Column) == 0 {
		return false
	}
	for _, col := range fo.Column {
		if col != "timestamp" {
			return false
		}
	}
	return true
}

func parseTimestampFilterValue(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)

	for _, layout := range timestampFilterLayouts {
		if t, err := time.Parse(layout, value); err == nil {
			return t, nil
		}
	}

	// Unix seconds, which is the form the monitoring endpoints' gt/lt filters
	// take -- an operator moving between the two should not have to find that
	// out from an empty result set.
	if epoch, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(epoch, 0).UTC(), nil
	}

	return time.Time{}, fmt.Errorf(
		"illegal timestamp filter value %q: expected an RFC 3339 timestamp, %q, a date, or Unix seconds",
		raw, timestampDbLayout)
}

func (a *AuditLog) List(r *http.Request, user *users.User) (*api.SuccessPayload, error) {
	options, err := listOptionsFor(r, user)
	if err != nil {
		return nil, err
	}

	entries, err := a.provider.List(r.Context(), options)
	if err != nil {
		return nil, err
	}

	count, err := a.provider.Count(r.Context(), options)
	if err != nil {
		return nil, err
	}

	return &api.SuccessPayload{
		Data: entries,
		Meta: api.NewMeta(count),
	}, nil
}
