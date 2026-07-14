package monitoring

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/proximile/proxiport/db/migration/monitoring"
	"github.com/proximile/proxiport/db/sqlite"
	"github.com/proximile/proxiport/share/enc"
	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/models"
	"github.com/proximile/proxiport/share/query"
	"github.com/proximile/proxiport/share/types"
)

type DBProvider interface {
	CreateMeasurement(ctx context.Context, measurement *models.Measurement) error
	DeleteMeasurementsBefore(ctx context.Context, compare time.Time) (int64, error)
	ListGraphByClientID(context.Context, string, float64, *query.ListOptions, string) ([]*ClientGraphMetricsGraphPayload, error)
	ListGraphMetricsByClientID(context.Context, string, float64, *query.ListOptions) ([]*ClientGraphMetricsPayload, error)
	ListMetricsByClientID(context.Context, string, *query.ListOptions) ([]*ClientMetricsPayload, error)
	ListMountpointsByClientID(context.Context, string, *query.ListOptions) ([]*ClientMountpointsPayload, error)
	ListProcessesByClientID(context.Context, string, *query.ListOptions) ([]*ClientProcessesPayload, error)
	CountByClientID(context.Context, string, *query.ListOptions) (int, error)
	Close() error
}

// MaxDeletedEntries to prevent "stop the world" after longer restart, when there is a lot of measurements to clean up
// clean them in chunks and this is the chunk size
const MaxDeletedEntries = 5000

type SqliteProvider struct {
	db        *sqlx.DB
	logger    *logger.Logger
	converter *query.SQLConverter
	enc       *enc.Envelope
}

func NewSqliteProvider(dbPath string, dataSourceOptions sqlite.DataSourceOptions, envelope *enc.Envelope, logger *logger.Logger) (DBProvider, error) {
	db, err := sqlite.New(dbPath, monitoring.AssetNames(), monitoring.Asset, dataSourceOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to create monitoring DB instance: %v", err)
	}

	logger.Infof("initialized database at %s", dbPath)

	if envelope == nil {
		envelope = enc.NewEnvelope(nil)
	}
	p := &SqliteProvider{
		db:        db,
		logger:    logger,
		converter: query.NewSQLConverter(db.DriverName()),
		enc:       envelope,
	}
	if err := p.encryptExistingMeasurements(); err != nil {
		logger.Errorf("monitoring at-rest backfill failed (new measurements are still encrypted): %v", err)
	}
	return p, nil
}

// encryptExistingMeasurements walks the measurements table once at startup and
// encrypts any legacy plaintext processes/mountpoints blob in place. Idempotent
// (already-encrypted or empty values are skipped), so it is safe every boot.
func (p *SqliteProvider) encryptExistingMeasurements() error {
	if !p.enc.Enabled() {
		return nil
	}
	type row struct {
		ClientID    string    `db:"client_id"`
		Timestamp   time.Time `db:"timestamp"`
		Processes   string    `db:"processes"`
		Mountpoints string    `db:"mountpoints"`
	}
	var rows []row
	if err := p.db.Select(&rows, "SELECT client_id, timestamp, processes, mountpoints FROM measurements"); err != nil {
		return fmt.Errorf("read measurements: %w", err)
	}
	migrated := 0
	for _, r := range rows {
		proc, procChanged, err := p.maybeEncrypt(r.Processes)
		if err != nil {
			return err
		}
		mount, mountChanged, err := p.maybeEncrypt(r.Mountpoints)
		if err != nil {
			return err
		}
		if !procChanged && !mountChanged {
			continue
		}
		if _, err := p.db.Exec(
			"UPDATE measurements SET processes = ?, mountpoints = ? WHERE client_id = ? AND timestamp = ?",
			proc, mount, r.ClientID, r.Timestamp,
		); err != nil {
			return fmt.Errorf("update measurement: %w", err)
		}
		migrated++
	}
	if migrated > 0 {
		p.logger.Infof("monitoring at-rest backfill: encrypted %d measurement(s)", migrated)
	}
	return nil
}

func (p *SqliteProvider) maybeEncrypt(v string) (out string, changed bool, err error) {
	if v == "" || enc.IsEncrypted(v) {
		return v, false, nil
	}
	out, err = p.enc.Encrypt(v)
	if err != nil {
		return v, false, err
	}
	return out, true, nil
}

// decryptJSONString decrypts a stored processes/mountpoints value for the API.
// Legacy plaintext passes through; a prefixed value that will not decrypt errors
// (fail closed).
func (p *SqliteProvider) decryptJSONString(v types.JSONString) (types.JSONString, error) {
	out, err := p.enc.Decrypt(string(v))
	if err != nil {
		return "", err
	}
	return types.JSONString(out), nil
}

func (p *SqliteProvider) ListMountpointsByClientID(ctx context.Context, clientID string, o *query.ListOptions) ([]*ClientMountpointsPayload, error) {
	q := "SELECT * FROM `measurements` as `mountpoints` WHERE `client_id` = ? "
	params := []interface{}{}
	params = append(params, clientID)
	q, params = p.converter.AppendOptionsToQuery(o, q, params)

	val := []*ClientMountpointsPayload{}
	if err := p.db.SelectContext(ctx, &val, q, params...); err != nil {
		return nil, err
	}
	for _, m := range val {
		dec, err := p.decryptJSONString(m.Mountpoints)
		if err != nil {
			return nil, err
		}
		m.Mountpoints = dec
	}
	return val, nil
}

func (p *SqliteProvider) ListProcessesByClientID(ctx context.Context, clientID string, o *query.ListOptions) ([]*ClientProcessesPayload, error) {
	q := "SELECT * FROM `measurements` as `processes` WHERE `client_id` = ? "
	params := []interface{}{}
	params = append(params, clientID)
	q, params = p.converter.AppendOptionsToQuery(o, q, params)

	val := []*ClientProcessesPayload{}
	if err := p.db.SelectContext(ctx, &val, q, params...); err != nil {
		return nil, err
	}
	for _, m := range val {
		dec, err := p.decryptJSONString(m.Processes)
		if err != nil {
			return nil, err
		}
		m.Processes = dec
	}
	return val, nil
}

func (p *SqliteProvider) ListMetricsByClientID(ctx context.Context, clientID string, o *query.ListOptions) ([]*ClientMetricsPayload, error) {
	q := "SELECT * FROM `measurements` as `metrics` WHERE `client_id` = ? "
	params := []interface{}{}
	params = append(params, clientID)
	q, params = p.converter.AppendOptionsToQuery(o, q, params)

	val := []*ClientMetricsPayload{}
	err := p.db.SelectContext(ctx, &val, q, params...)
	return val, err
}

func (p *SqliteProvider) CountByClientID(ctx context.Context, clientID string, options *query.ListOptions) (int, error) {
	var result int

	q := "SELECT COUNT(*) FROM `measurements` WHERE `client_id` = ? "
	countOptions := *options
	countOptions.Pagination = nil

	params := []interface{}{}
	params = append(params, clientID)
	q, params = p.converter.AppendOptionsToQuery(&countOptions, q, params)

	err := p.db.GetContext(ctx, &result, q, params...)
	if err != nil {
		return 0, err
	}

	return result, nil
}

func (p *SqliteProvider) ListGraphMetricsByClientID(ctx context.Context, clientID string, hours float64, lo *query.ListOptions) ([]*ClientGraphMetricsPayload, error) {
	params := []interface{}{}
	params = append(params, clientID)

	q := `SELECT
		timestamp,
		round(avg(cpu_usage_percent),2) as cpu_usage_percent_avg,
		min(cpu_usage_percent) as cpu_usage_percent_min,
		max(cpu_usage_percent) as cpu_usage_percent_max,
		round(avg(memory_usage_percent),2) as memory_usage_percent_avg,
		min(memory_usage_percent) as memory_usage_percent_min,
		max(memory_usage_percent) as memory_usage_percent_max,
		round(avg(io_usage_percent),2) as io_usage_percent_avg,
		min(io_usage_percent) as io_usage_percent_min,
		max(io_usage_percent) as io_usage_percent_max
	FROM measurements WHERE client_id = ?`

	q, params = p.converter.AddWhere(lo.Filters, q, params)

	/*This is the part of "downsampling graph data" (group together graph points, so that you don't get too much points in one request).
	The value of "29" comes from Thorsten. He did some research and found out that "29" would be the best fit.
	*/
	q = q + ` GROUP BY round((strftime('%s',timestamp)/(?)),0)`
	divisor := (math.Round(hours*100) / 100) * 29
	params = append(params, divisor)

	q = p.converter.AddOrderBy(lo.Sorts, q)

	val := []*ClientGraphMetricsPayload{}
	err := p.db.SelectContext(ctx, &val, q, params...)
	return val, err
}

func (p *SqliteProvider) ListGraphByClientID(ctx context.Context, clientID string, hours float64, lo *query.ListOptions, graph string) ([]*ClientGraphMetricsGraphPayload, error) {
	params := []interface{}{}
	params = append(params, clientID)
	field, okField := ClientGraphNameToField[graph]
	alias, okAlias := ClientGraphNameToAlias[graph]
	if !okField || !okAlias {
		return nil, fmt.Errorf("unknown graph: %s", graph)
	}

	q := `SELECT timestamp, `
	q = q + ` 
		round(avg(` + field + `),2) as ` + alias + `_avg,
		min(` + field + `) as ` + alias + `_min,
		max(` + field + `) as ` + alias + `_max`

	if strings.HasPrefix(graph, "net_") {
		field = strings.ReplaceAll(field, "_in", "_out")
		alias = strings.ReplaceAll(alias, "_in", "_out")
		q = q + `, 
		round(avg(` + field + `),2) as ` + alias + `_avg,
		min(` + field + `) as ` + alias + `_min,
		max(` + field + `) as ` + alias + `_max`
	}
	q = q + ` 
	FROM measurements WHERE client_id = ?`

	q, params = p.converter.AddWhere(lo.Filters, q, params)

	q = q + ` GROUP BY round((strftime('%s',timestamp)/(?)),0)`
	divisor := (math.Round(hours*100) / 100) * 29
	params = append(params, divisor)

	query := p.converter.AddOrderBy(lo.Sorts, q)

	val := []*ClientGraphMetricsGraphPayload{}
	err := p.db.SelectContext(ctx, &val, query, params...)
	return val, err
}

func (p *SqliteProvider) CreateMeasurement(ctx context.Context, measurement *models.Measurement) error {
	// Encrypt the two free-text blobs at rest on a copy, so the caller's
	// in-memory measurement is untouched and only the DB holds ciphertext.
	if p.enc.Enabled() {
		mCopy := *measurement
		if ct, changed, err := p.maybeEncrypt(mCopy.Processes); err != nil {
			return err
		} else if changed {
			mCopy.Processes = ct
		}
		if ct, changed, err := p.maybeEncrypt(mCopy.Mountpoints); err != nil {
			return err
		} else if changed {
			mCopy.Mountpoints = ct
		}
		measurement = &mCopy
	}
	q := `INSERT INTO measurements (client_id, timestamp, cpu_usage_percent, memory_usage_percent, io_usage_percent, processes, mountpoints, net_lan_in, net_lan_out, net_wan_in, net_wan_out)
		VALUES (:client_id, :timestamp, :cpu_usage_percent, :memory_usage_percent, :io_usage_percent, :processes, :mountpoints, `
	if measurement.NetLan == nil {
		q = q + `null, null, `
	} else {
		q = q + `:net_lan.in, :net_lan.out, `
	}
	if measurement.NetWan == nil {
		q = q + `null, null`
	} else {
		q = q + `:net_wan.in, :net_wan.out`
	}
	query := q + ")"

	_, err := sqlite.WithRetryWhenBusy(func() (result sql.Result, err error) {
		result, err = p.db.NamedExecContext(ctx, query, measurement)
		return result, err
	}, "createmeasurement", p.logger)

	return err
}

// DeleteMeasurementsBefore deletes entries in chunks of MaxDeletedEntries
// to clean all you can run in loop as long as there are more than 0 rows affected
func (p *SqliteProvider) DeleteMeasurementsBefore(ctx context.Context, compare time.Time) (int64, error) {
	result, err := p.db.ExecContext(ctx, "DELETE FROM measurements WHERE  timestamp IN (SELECT distinct timestamp FROM measurements WHERE timestamp < ? ORDER BY timestamp LIMIT ?)", compare, MaxDeletedEntries)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (p *SqliteProvider) Close() error {
	return p.db.Close()
}
