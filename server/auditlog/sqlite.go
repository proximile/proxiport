package auditlog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"

	"github.com/proximile/proxiport/db/migration/auditlog"
	"github.com/proximile/proxiport/db/sqlite"
	"github.com/proximile/proxiport/share/query"
)

type SQLiteProvider struct {
	db        *sqlx.DB
	converter *query.SQLConverter

	// Tamper-evidence chain state. hmacKey is nil when no key provider is
	// configured (chain disabled). writeMtx serializes the read-head → compute
	// → insert sequence so concurrent writers cannot fork the chain; lastSeq /
	// lastMAC cache the chain head to avoid a query per write.
	hmacKey  []byte
	writeMtx sync.Mutex
	lastSeq  int64
	lastMAC  string
}

func newSQLiteProvider(dataDir string, dataSourceOptions sqlite.DataSourceOptions, hmacKey []byte) (*SQLiteProvider, error) {
	db, err := sqlite.New(
		path.Join(dataDir, sqliteFilename),
		auditlog.AssetNames(),
		auditlog.Asset,
		dataSourceOptions,
	)
	if err != nil {
		return nil, err
	}
	p := &SQLiteProvider{
		db:        db,
		converter: query.NewSQLConverter(db.DriverName()),
		hmacKey:   hmacKey,
	}
	if err := p.loadChainHead(); err != nil {
		return nil, err
	}
	return p, nil
}

// loadChainHead seeds the in-memory chain head from the highest existing chained
// row, so a restart continues the same chain rather than forking it.
func (p *SQLiteProvider) loadChainHead() error {
	if len(p.hmacKey) == 0 {
		return nil
	}
	var head struct {
		Seq int64  `db:"seq"`
		MAC string `db:"mac"`
	}
	err := p.db.Get(&head, "SELECT seq, mac FROM auditlog WHERE seq = (SELECT MAX(seq) FROM auditlog)")
	if errors.Is(err, sql.ErrNoRows) {
		// Empty table — start from genesis.
		p.lastSeq, p.lastMAC = 0, ""
		return nil
	}
	if err != nil {
		return fmt.Errorf("load audit chain head: %w", err)
	}
	p.lastSeq, p.lastMAC = head.Seq, head.MAC
	return nil
}

func (p *SQLiteProvider) Save(e *Entry) error {
	// Serialize the chain head across concurrent writers: read head → compute
	// this row's MAC → insert → advance head, all under one lock.
	if len(p.hmacKey) > 0 {
		p.writeMtx.Lock()
		defer p.writeMtx.Unlock()
		e.Seq = p.lastSeq + 1
		e.PrevMAC = p.lastMAC
		e.MAC = computeMAC(p.hmacKey, e)
	}

	_, err := p.db.NamedExec(
		`INSERT INTO auditlog (
			timestamp,
			username,
			remote_ip,
			application,
			action,
			affected_id,
			client_id,
			client_hostname,
			request,
			response,
			seq,
			mac,
			prev_mac
		) VALUES (
			:timestamp,
			:username,
			:remote_ip,
			:application,
			:action,
			:affected_id,
			:client_id,
			:client_hostname,
			:request,
			:response,
			:seq,
			:mac,
			:prev_mac
		)`,
		e,
	)
	if err != nil {
		return err
	}
	// Only advance the cached head after a durable insert.
	if len(p.hmacKey) > 0 {
		p.lastSeq = e.Seq
		p.lastMAC = e.MAC
	}
	return nil
}

// Verify walks the whole chain in seq order and confirms every row's MAC and
// link. It is the tamper-detection read path.
func (p *SQLiteProvider) Verify(ctx context.Context) (ChainVerification, error) {
	if len(p.hmacKey) == 0 {
		return ChainVerification{Enabled: false}, nil
	}
	var rows []*Entry
	if err := p.db.SelectContext(ctx, &rows, "SELECT * FROM `auditlog` ORDER BY seq ASC"); err != nil {
		return ChainVerification{}, fmt.Errorf("read auditlog for verification: %w", err)
	}
	return verifyChain(p.hmacKey, rows), nil
}

func (p *SQLiteProvider) List(ctx context.Context, options *query.ListOptions) ([]*Entry, error) {
	values := []*Entry{}

	q := "SELECT * FROM `auditlog`"

	q, params := p.converter.ConvertListOptionsToQuery(options, q)

	err := p.db.SelectContext(ctx, &values, q, params...)
	if err != nil {
		return values, err
	}

	return values, nil
}

func (p *SQLiteProvider) Count(ctx context.Context, options *query.ListOptions) (int, error) {
	var result int

	q := "SELECT COUNT(*) FROM `auditlog`"
	countOptions := *options
	countOptions.Pagination = nil
	q, params := p.converter.ConvertListOptionsToQuery(&countOptions, q)

	err := p.db.GetContext(ctx, &result, q, params...)
	if err != nil {
		return 0, err
	}

	return result, nil
}

func (p *SQLiteProvider) OldestTimestamp(ctx context.Context) (time.Time, error) {
	var ts time.Time
	q := "SELECT timestamp FROM auditlog ORDER BY timestamp ASC LIMIT 1"
	err := p.db.GetContext(ctx, &ts, q)
	if err != nil {
		return ts, err
	}
	return ts, nil
}

func (p *SQLiteProvider) Close() error {
	return p.db.Close()
}
