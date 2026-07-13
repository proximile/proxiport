package vault

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/proximile/proxiport/share/enc"
	"github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/query"

	errors2 "github.com/proximile/proxiport/server/api/errors"

	"github.com/jmoiron/sqlx"

	"github.com/proximile/proxiport/db/migration/vaults"

	"github.com/proximile/proxiport/db/sqlite"
)

var ErrDatabaseNotInitialised = errors2.APIError{
	Err:        errors.New("vault is not initialized yet"),
	HTTPStatus: http.StatusConflict,
}
var DataSourceOptions = sqlite.DataSourceOptions{WALEnabled: false}

type SqliteProvider struct {
	db        *sqlx.DB
	logger    *logger.Logger
	converter *query.SQLConverter
	// enc wraps the secret-bearing columns (enc_check and each values.value) under
	// the server DEK at rest — a second layer on top of the passphrase-derived
	// encryption that removes the offline passphrase-guessing oracle a disk thief
	// would otherwise have. Always non-nil; a disabled envelope is passthrough.
	enc *enc.Envelope
}

func NewSqliteProvider(c Config, envelope *enc.Envelope, logger *logger.Logger) (*SqliteProvider, error) {
	dbPath := c.GetVaultDBPath()

	db, err := sqlite.New(dbPath, vaults.AssetNames(), vaults.Asset, DataSourceOptions)
	if err != nil {
		return nil, fmt.Errorf("failed init vault DB instance: %w", err)
	}

	logger.Infof("initialized database at %s", dbPath)

	if envelope == nil {
		envelope = enc.NewEnvelope(nil)
	}
	p := &SqliteProvider{
		logger:    logger,
		db:        db,
		converter: query.NewSQLConverter(db.DriverName()),
		enc:       envelope,
	}
	if err := p.wrapExistingSecrets(context.Background()); err != nil {
		_ = p.db.Close()
		return nil, err
	}
	return p, nil
}

// wrap is the encrypt-on-write path for a secret-bearing vault column. An empty
// value is stored verbatim (nothing to protect); everything else is wrapped
// under the DEK when a key provider is configured, and passed through unchanged
// otherwise.
func (p *SqliteProvider) wrap(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	return p.enc.Encrypt(v)
}

// unwrap is the decrypt-on-read path. Legacy (unwrapped) values pass through; a
// DEK-wrapped value that cannot be decrypted under the current key errors so the
// caller fails closed rather than surfacing ciphertext.
func (p *SqliteProvider) unwrap(v string) (string, error) {
	return p.enc.Decrypt(v)
}

// wrapExistingSecrets is the at-rest DEK migration for the vault: when a key
// provider is configured it wraps any not-yet-wrapped enc_check and values.value
// in place at startup. It operates purely on the DEK layer over the already
// passphrase-encrypted ciphertext, so it needs no passphrase and runs whether the
// vault is locked or not. Idempotent — wrapped values are skipped.
func (p *SqliteProvider) wrapExistingSecrets(ctx context.Context) error {
	if !p.enc.Enabled() {
		return nil
	}

	tx, err := p.db.Beginx()
	if err != nil {
		return err
	}

	var encCheck string
	var statusID int
	err = tx.GetContext(ctx, &statusID, "SELECT id FROM `status` LIMIT 1")
	switch {
	case err == sql.ErrNoRows:
		// no status row yet — nothing to wrap
	case err != nil:
		p.handleRollback(tx)
		return err
	default:
		if err = tx.GetContext(ctx, &encCheck, "SELECT `enc_check` FROM `status` WHERE id = ?", statusID); err != nil {
			p.handleRollback(tx)
			return err
		}
		if encCheck != "" && !enc.IsEncrypted(encCheck) {
			wrapped, werr := p.enc.Encrypt(encCheck)
			if werr != nil {
				p.handleRollback(tx)
				return werr
			}
			if _, err = tx.ExecContext(ctx, "UPDATE `status` SET `enc_check` = ? WHERE id = ?", wrapped, statusID); err != nil {
				p.handleRollback(tx)
				return err
			}
		}
	}

	type row struct {
		ID    int    `db:"id"`
		Value string `db:"value"`
	}
	var rows []row
	if err = tx.SelectContext(ctx, &rows, "SELECT `id`, `value` FROM `values`"); err != nil {
		p.handleRollback(tx)
		return err
	}
	wrapped := 0
	for i := range rows {
		if rows[i].Value == "" || enc.IsEncrypted(rows[i].Value) {
			continue
		}
		w, werr := p.enc.Encrypt(rows[i].Value)
		if werr != nil {
			p.handleRollback(tx)
			return werr
		}
		if _, err = tx.ExecContext(ctx, "UPDATE `values` SET `value` = ? WHERE id = ?", w, rows[i].ID); err != nil {
			p.handleRollback(tx)
			return err
		}
		wrapped++
	}

	if err = tx.Commit(); err != nil {
		return err
	}
	if wrapped > 0 {
		p.logger.Infof("wrapped %d vault value(s) under the key provider at rest", wrapped)
	}
	return nil
}

func (p *SqliteProvider) Close() error {
	if p.db != nil {
		return p.db.Close()
	}

	return nil
}

func (p *SqliteProvider) GetStatus(ctx context.Context) (DbStatus, error) {
	res := DbStatus{}
	err := p.db.GetContext(ctx, &res, "SELECT * FROM `status` LIMIT 1")
	if err != nil {
		if err == sql.ErrNoRows {
			return res, nil
		}
		return res, err
	}

	if res.EncCheckValue, err = p.unwrap(res.EncCheckValue); err != nil {
		return DbStatus{}, err
	}

	return res, nil
}

func (p *SqliteProvider) SetStatus(ctx context.Context, newStatus DbStatus) error {
	encCheck, err := p.wrap(newStatus.EncCheckValue)
	if err != nil {
		return err
	}

	tx, err := p.db.Beginx()
	if err != nil {
		return err
	}

	var idToUpdate int
	err = tx.GetContext(ctx, &idToUpdate, "SELECT id FROM `status` LIMIT 1")
	if err != nil {
		if err == sql.ErrNoRows {
			idToUpdate = 0
		} else {
			p.handleRollback(tx)
			return err
		}
	}

	if idToUpdate == 0 {
		_, err = tx.ExecContext(
			ctx,
			"INSERT INTO `status` (`db_status`, `enc_check`, `kdf`) VALUES (?, ?, ?)",
			newStatus.StatusName,
			encCheck,
			newStatus.KDF,
		)

		if err != nil {
			p.handleRollback(tx)
			return err
		}
	} else {
		q := "UPDATE `status` SET db_status=?, enc_check = ?, kdf = ? WHERE id = ?"
		params := []interface{}{
			newStatus.StatusName,
			encCheck,
			newStatus.KDF,
			idToUpdate,
		}
		_, err = tx.ExecContext(ctx, q, params...)
		if err != nil {
			p.handleRollback(tx)
			return err
		}
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

func (p *SqliteProvider) GetByID(ctx context.Context, id int) (val StoredValue, found bool, err error) {
	err = p.db.GetContext(ctx, &val, "SELECT * FROM `values` WHERE `id` = ? LIMIT 1", id)
	if err != nil {
		if err == sql.ErrNoRows {
			return val, false, nil
		}

		return val, false, err
	}

	if val.Value, err = p.unwrap(val.Value); err != nil {
		return StoredValue{}, false, err
	}

	return val, true, nil
}

func (p *SqliteProvider) List(ctx context.Context, lo *query.ListOptions) ([]ValueKey, error) {
	values := []ValueKey{}

	q := "SELECT `id`, `client_id`, `created_by`, `created_at`, `key` FROM `values`"

	q, params := p.converter.ConvertListOptionsToQuery(lo, q)

	err := p.db.SelectContext(ctx, &values, q, params...)
	if err != nil {
		return values, err
	}

	return values, nil
}

func (p *SqliteProvider) FindByKeyAndClientID(ctx context.Context, key, clientID string) (val StoredValue, found bool, err error) {
	err = p.db.GetContext(ctx, &val, "SELECT * FROM `values` WHERE `key` = ? and `client_id` = ? LIMIT 1", key, clientID)
	if err != nil {
		if err == sql.ErrNoRows {
			return val, false, nil
		}

		return val, false, err
	}

	if val.Value, err = p.unwrap(val.Value); err != nil {
		return StoredValue{}, false, err
	}

	return val, true, nil
}

func (p *SqliteProvider) Save(ctx context.Context, user string, idToUpdate int64, val *InputValue, nowDate time.Time) (int64, error) {
	storedValue, err := p.wrap(val.Value)
	if err != nil {
		return 0, err
	}

	if idToUpdate == 0 {
		res, err := p.db.ExecContext(
			ctx,
			"INSERT INTO `values` (`client_id`, `required_group`, `created_at`, `created_by`, `updated_at`, `updated_by`, `key`, `value`, `type`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			val.ClientID,
			val.RequiredGroup,
			nowDate.Format(time.RFC3339),
			user,
			nowDate.Format(time.RFC3339),
			user,
			val.Key,
			storedValue,
			val.Type,
		)

		if err != nil {
			return 0, err
		}
		idToUpdate, err = res.LastInsertId()
		if err != nil {
			return 0, err
		}
	} else {
		q := "UPDATE `values` SET `client_id` = ?, `required_group` = ?, `updated_at` = ?, `updated_by` = ?, `key` = ?, `value` = ?, `type` = ? WHERE id = ?"
		params := []interface{}{
			val.ClientID,
			val.RequiredGroup,
			nowDate.Format(time.RFC3339),
			user,
			val.Key,
			storedValue,
			val.Type,
			idToUpdate,
		}
		_, err := p.db.ExecContext(ctx, q, params...)
		if err != nil {
			return 0, err
		}
	}

	return idToUpdate, nil
}

func (p *SqliteProvider) Delete(ctx context.Context, id int) error {
	res, err := p.db.ExecContext(ctx, "DELETE FROM `values` WHERE `id` = ?", id)

	if err != nil {
		return err
	}

	affectedRows, err := res.RowsAffected()
	if err != nil {
		return err
	}

	if affectedRows == 0 {
		return fmt.Errorf("cannot find entry by id %d", id)
	}

	return nil
}

// ReKey re-encrypts every stored value via transform and updates the status
// row's KDF descriptor and enc_check verifier, all inside a single transaction
// so a legacy vault is never left half-migrated.
func (p *SqliteProvider) ReKey(ctx context.Context, transform func(oldValue string) (string, error), newKDF, newEncCheck string) error {
	tx, err := p.db.Beginx()
	if err != nil {
		return err
	}

	type row struct {
		ID    int    `db:"id"`
		Value string `db:"value"`
	}
	var rows []row
	if err = tx.SelectContext(ctx, &rows, "SELECT `id`, `value` FROM `values`"); err != nil {
		p.handleRollback(tx)
		return err
	}

	for _, r := range rows {
		// transform operates on the passphrase-encryption layer, so peel the DEK
		// wrap off first and re-apply it after re-encrypting.
		oldValue, uerr := p.unwrap(r.Value)
		if uerr != nil {
			p.handleRollback(tx)
			return uerr
		}
		newValue, terr := transform(oldValue)
		if terr != nil {
			p.handleRollback(tx)
			return terr
		}
		storedValue, werr := p.wrap(newValue)
		if werr != nil {
			p.handleRollback(tx)
			return werr
		}
		if _, err = tx.ExecContext(ctx, "UPDATE `values` SET `value` = ? WHERE `id` = ?", storedValue, r.ID); err != nil {
			p.handleRollback(tx)
			return err
		}
	}

	wrappedEncCheck, err := p.wrap(newEncCheck)
	if err != nil {
		p.handleRollback(tx)
		return err
	}
	res, err := tx.ExecContext(ctx, "UPDATE `status` SET `enc_check` = ?, `kdf` = ?", wrappedEncCheck, newKDF)
	if err != nil {
		p.handleRollback(tx)
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		p.handleRollback(tx)
		return err
	}
	if affected == 0 {
		p.handleRollback(tx)
		return errors.New("re-key: vault status row is missing")
	}

	if err := tx.Commit(); err != nil {
		return err
	}

	// Scrub freed pages so the legacy ciphertext and the dropped dec_check oracle
	// no longer linger in the file for a host adversary. Best-effort: the re-key
	// is already durable, so a VACUUM failure must not fail the unlock.
	if _, err := p.db.ExecContext(ctx, "VACUUM"); err != nil {
		p.logger.Errorf("vault re-key: VACUUM after re-key failed: %v", err)
	}

	return nil
}

func (p *SqliteProvider) handleRollback(tx *sqlx.Tx) {
	err := tx.Rollback()
	if err != nil {
		p.logger.Errorf("Failed to rollback transaction: %v", err)
	}
}

type NotInitDbProvider struct{}

func (nidp *NotInitDbProvider) Init(ctx context.Context) error {
	return ErrDatabaseNotInitialised
}

func (nidp *NotInitDbProvider) GetStatus(ctx context.Context) (DbStatus, error) {
	return DbStatus{}, ErrDatabaseNotInitialised
}

func (nidp *NotInitDbProvider) SetStatus(ctx context.Context, newStatus DbStatus) error {
	return ErrDatabaseNotInitialised
}

func (nidp *NotInitDbProvider) GetByID(ctx context.Context, id int) (val StoredValue, found bool, err error) {
	err = ErrDatabaseNotInitialised
	return
}

func (nidp *NotInitDbProvider) List(ctx context.Context, lo *query.ListOptions) ([]ValueKey, error) {
	return nil, ErrDatabaseNotInitialised
}

func (nidp *NotInitDbProvider) FindByKeyAndClientID(ctx context.Context, key, clientID string) (val StoredValue, found bool, err error) {
	err = ErrDatabaseNotInitialised
	return
}

func (nidp *NotInitDbProvider) Save(ctx context.Context, user string, idToUpdate int64, val *InputValue, nowDate time.Time) (int64, error) {
	return 0, ErrDatabaseNotInitialised
}

func (nidp *NotInitDbProvider) Delete(ctx context.Context, id int) error {
	return ErrDatabaseNotInitialised
}

func (nidp *NotInitDbProvider) ReKey(ctx context.Context, transform func(oldValue string) (string, error), newKDF, newEncCheck string) error {
	return ErrDatabaseNotInitialised
}

func (nidp *NotInitDbProvider) Close() error {
	return nil
}
