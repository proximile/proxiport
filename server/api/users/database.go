package users

import (
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strings"

	errors2 "github.com/proximile/proxiport/server/api/errors"
	"github.com/proximile/proxiport/share/enc"
	"github.com/proximile/proxiport/share/enums"
	"github.com/proximile/proxiport/share/logger"

	"github.com/jmoiron/sqlx"
)

type UserDatabase struct {
	db *sqlx.DB

	usersTableName        string
	groupsTableName       string
	groupDetailsTableName string

	twoFAOn bool
	totPOn  bool
	// enc encrypts the recoverable totp_secret column at rest. It is always
	// non-nil; a disabled envelope (no key provider) passes values through.
	enc    *enc.Envelope
	logger *logger.Logger
}

func NewUserDatabase(
	DB *sqlx.DB,
	usersTableName, groupsTableName, groupDetailsTableName string,
	twoFAOn, totPOn bool,
	envelope *enc.Envelope,
	logger *logger.Logger,
) (*UserDatabase, error) {
	if envelope == nil {
		envelope = enc.NewEnvelope(nil)
	}
	d := &UserDatabase{
		db: DB,

		usersTableName:        usersTableName,
		groupsTableName:       groupsTableName,
		groupDetailsTableName: groupDetailsTableName,

		twoFAOn: twoFAOn,
		totPOn:  totPOn,
		enc:     envelope,
		logger:  logger,
	}
	if err := d.checkDatabaseTables(); err != nil {
		return nil, err
	}
	if err := d.encryptExistingTotP(); err != nil {
		return nil, err
	}
	return d, nil
}

// rejectPlaintextPasswords fails startup if any stored API-user password is not
// a bcrypt hash. A plaintext password in the user table would be readable from
// a stolen disk and would authenticate through the (now removed) plaintext
// verify path, so the server refuses to run with one rather than trusting it.
// Credentials created through the API/CLI are always hashed on write; this only
// catches a hand-seeded or legacy row, which the operator must hash (e.g. with
// `htpasswd -bnBC 10 "" <password> | tr -d ':'`).
func (d *UserDatabase) rejectPlaintextPasswords() error {
	var rows []struct {
		Username string `db:"username"`
		Password string `db:"password"`
	}
	err := d.db.Select(&rows, fmt.Sprintf("SELECT username, password FROM `%s`", d.usersTableName))
	if err != nil {
		return fmt.Errorf("password format check: read users: %w", err)
	}
	for _, r := range rows {
		if r.Password == "" {
			continue
		}
		if !IsBcryptHash(r.Password) {
			return fmt.Errorf("API user %q has a non-bcrypt password stored; hash it (e.g. `htpasswd -bnBC 10 \"\" <password> | tr -d ':'`) — plaintext passwords are refused", r.Username)
		}
	}
	return nil
}

// encryptTotP is the encrypt-on-write path for the totp_secret column. Empty
// and whitespace-only values (including the " " sentinel the delete path
// writes to clear a secret) are stored verbatim so they round-trip and are
// still recognized as "no secret" on read.
func (d *UserDatabase) encryptTotP(v string) (string, error) {
	if strings.TrimSpace(v) == "" {
		return v, nil
	}
	return d.enc.Encrypt(v)
}

// decryptTotP is the decrypt-on-read path. Legacy plaintext (no prefix) passes
// through unchanged; an encrypted value that cannot be decrypted under the
// current key returns an error so the caller fails closed rather than exposing
// ciphertext.
func (d *UserDatabase) decryptTotP(v string) (string, error) {
	return d.enc.Decrypt(v)
}

// encryptExistingTotP is the at-rest migration for the totp_secret column: when
// a key provider is configured it walks the users table once at startup and
// encrypts any legacy plaintext secret in place. It is idempotent — values that
// are already encrypted (or empty) are skipped — so it is safe on every boot.
func (d *UserDatabase) encryptExistingTotP() error {
	if !d.totPOn || !d.enc.Enabled() {
		return nil
	}

	var rows []struct {
		Username string `db:"username"`
		TotP     string `db:"totp_secret"`
	}
	err := d.db.Select(&rows, fmt.Sprintf("SELECT username, totp_secret FROM `%s`", d.usersTableName))
	if err != nil {
		return fmt.Errorf("totp_secret migration: read users: %w", err)
	}

	migrated := 0
	for i := range rows {
		if strings.TrimSpace(rows[i].TotP) == "" || enc.IsEncrypted(rows[i].TotP) {
			continue
		}
		ciphertext, err := d.enc.Encrypt(rows[i].TotP)
		if err != nil {
			return fmt.Errorf("totp_secret migration: encrypt %q: %w", rows[i].Username, err)
		}
		_, err = d.db.Exec(
			fmt.Sprintf("UPDATE `%s` SET `totp_secret` = ? WHERE username = ?", d.usersTableName),
			ciphertext, rows[i].Username,
		)
		if err != nil {
			return fmt.Errorf("totp_secret migration: update %q: %w", rows[i].Username, err)
		}
		migrated++
	}
	if migrated > 0 && d.logger != nil {
		d.logger.Infof("encrypted %d plaintext totp_secret value(s) at rest", migrated)
	}
	return nil
}

func (d *UserDatabase) getSelectClause() string {
	s := "username, password, password_expired"
	if d.twoFAOn {
		s += ", two_fa_send_to"
	}
	if d.totPOn {
		s += ", totp_secret"
	}
	return s
}

// checkDatabaseTables @todo use context for all db operations
func (d *UserDatabase) checkDatabaseTables() error {
	_, err := d.db.Exec(fmt.Sprintf("SELECT %s FROM `%s` LIMIT 0", d.getSelectClause(), d.usersTableName))
	if err != nil {
		err = fmt.Errorf("%v: if 2FA is enabled the users table needs the additional 2FA columns — see the user-auth section of the docs", err)
		return err
	}
	_, err = d.db.Exec(fmt.Sprintf("SELECT username, `group` FROM `%s` LIMIT 0", d.groupsTableName))
	if err != nil {
		return err
	}
	if d.groupDetailsTableName != "" {
		_, err = d.db.Exec(fmt.Sprintf("SELECT name, permissions, tunnels_restricted, commands_restricted FROM `%s` LIMIT 0", d.groupDetailsTableName))
		if err != nil {
			// Legacy schemas may predate the extended-permission columns. Fall back to the minimal check.
			_, err = d.db.Exec(fmt.Sprintf("SELECT name, permissions FROM `%s` LIMIT 0", d.groupDetailsTableName))
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// GetByUsername @todo use context for all db operations
func (d *UserDatabase) GetByUsername(username string) (*User, error) {
	user := &User{}
	err := d.db.Get(user, fmt.Sprintf("SELECT %s FROM `%s` WHERE username = ? LIMIT 1", d.getSelectClause(), d.usersTableName), username)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	err = d.db.Select(&user.Groups, fmt.Sprintf("SELECT DISTINCT(`group`) FROM `%s` WHERE username = ?", d.groupsTableName), username)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	if user.TotP, err = d.decryptTotP(user.TotP); err != nil {
		return nil, err
	}

	return user, nil
}

// GetAll @todo use context for all db operations
func (d *UserDatabase) GetAll() ([]*User, error) {
	var usrs []*User
	err := d.db.Select(&usrs, fmt.Sprintf("SELECT %s FROM `%s` ORDER BY username", d.getSelectClause(), d.usersTableName))
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	for i := range usrs {
		if usrs[i].TotP, err = d.decryptTotP(usrs[i].TotP); err != nil {
			return nil, err
		}
	}

	var groups []struct {
		Username string `db:"username"`
		Group    string `db:"group"`
	}
	err = d.db.Select(&groups, fmt.Sprintf("SELECT `username`, `group` FROM `%s` ORDER BY `group`", d.groupsTableName))
	if err != nil {
		if err == sql.ErrNoRows {
			return usrs, nil
		}
		return nil, err
	}
	for i := range groups {
		for y := range usrs {
			if usrs[y].Username == groups[i].Username {
				usrs[y].Groups = append(usrs[y].Groups, groups[i].Group)
			}
		}
	}

	return usrs, nil
}

func (d *UserDatabase) ListGroups() ([]Group, error) {
	var groups []Group

	if d.groupDetailsTableName != "" {
		err := d.db.Select(&groups, fmt.Sprintf("SELECT * FROM `%s` ORDER BY `name`", d.groupDetailsTableName))
		if err != nil && err != sql.ErrNoRows {
			return nil, err
		}
	}

	var userGroups []string
	err := d.db.Select(&userGroups, fmt.Sprintf("SELECT DISTINCT `group` FROM `%s` ORDER BY `group`", d.groupsTableName))
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	for _, ug := range userGroups {
		found := false
		for _, g := range groups {
			if ug == g.Name {
				found = true
				break
			}
		}
		if !found {
			groups = append(groups, NewGroup(ug, nil, nil))
		}
	}

	return groups, nil
}

func (d *UserDatabase) GetGroup(name string) (Group, error) {
	if d.groupDetailsTableName == "" {
		return NewGroup(name, nil, nil), nil
	}

	group := Group{}
	err := d.db.Get(&group, fmt.Sprintf("SELECT * FROM `%s` WHERE name = ? LIMIT 1", d.groupDetailsTableName), name)
	if err == sql.ErrNoRows {
		return NewGroup(name, nil, nil), nil
	} else if err != nil {
		return Group{}, err
	}

	return group, nil
}

func (d *UserDatabase) UpdateGroup(name string, group Group) error {
	if d.groupDetailsTableName == "" {
		return errors2.APIError{
			Message:    "User group details table must be configured for this operation.",
			HTTPStatus: http.StatusBadRequest,
		}
	}

	// We rely on a unique index. Let the database decide, if INSERT or UPDATE is needed.
	var err error
	group.Name = name

	qt1 := ""
	qt2 := ""
	if group.TunnelsRestricted != nil {
		qt1 = ", tunnels_restricted"
		qt2 = ", :tunnels_restricted"
	}

	qc1 := ""
	qc2 := ""
	if group.CommandsRestricted != nil {
		qc1 = ", commands_restricted"
		qc2 = ", :commands_restricted"
	}
	// compose the query (assume the extended fields are present)
	// We rely on a unique index. Let the database decide, if INSERT or UPDATE is needed.
	qb := fmt.Sprintf("REPLACE INTO `%s` (name, permissions%s%s) VALUES (:name, :permissions%s%s)", d.groupDetailsTableName, qt1, qc1, qt2, qc2)

	_, err = d.db.NamedExec(qb, group)

	if err != nil {
		// Legacy schemas may not have the extended-permission columns. Retry without them.
		_, err = d.db.NamedExec(fmt.Sprintf("REPLACE INTO `%s` (name, permissions) VALUES (:name, :permissions)", d.groupDetailsTableName), group)
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *UserDatabase) DeleteGroup(name string) error {
	if d.groupDetailsTableName == "" {
		return errors2.APIError{
			Message:    "User group details table must be configured for this operation.",
			HTTPStatus: http.StatusBadRequest,
		}
	}

	tx, err := d.db.Beginx()
	if err != nil {
		return err
	}

	_, err = tx.Exec(fmt.Sprintf("DELETE FROM `%s` WHERE `group` = ?", d.groupsTableName), name)
	if err != nil {
		d.handleRollback(tx)
		return err
	}

	_, err = tx.Exec(fmt.Sprintf("DELETE FROM `%s` WHERE `name` = ?", d.groupDetailsTableName), name)
	if err != nil {
		d.handleRollback(tx)
		return err
	}

	return tx.Commit()
}

func (d *UserDatabase) handleRollback(tx *sqlx.Tx) {
	err := tx.Rollback()
	if err != nil {
		d.logger.Errorf("Failed to rollback transaction: %v", err)
	}
}

// Add todo use context for all db operations
func (d *UserDatabase) Add(usr *User) error {
	tx, err := d.db.Beginx()
	if err != nil {
		return err
	}

	columns := []string{
		"`username`",
		"`password`",
	}
	params := []interface{}{
		usr.Username,
		usr.Password,
	}

	if d.twoFAOn {
		columns = append(columns, "`two_fa_send_to`")
		params = append(params, usr.TwoFASendTo)
	}

	if d.totPOn {
		totp, encErr := d.encryptTotP(usr.TotP)
		if encErr != nil {
			d.handleRollback(tx)
			return encErr
		}
		columns = append(columns, "`totp_secret`")
		params = append(params, totp)
	}

	_, err = tx.Exec(
		fmt.Sprintf(
			"INSERT INTO `%s` (%s) VALUES (%s)",
			d.usersTableName,
			strings.Join(columns, ", "),
			strings.TrimRight(strings.Repeat("?,", len(params)), ","),
		),
		params...,
	)

	if err != nil {
		d.handleRollback(tx)
		return err
	}

	for i := range usr.Groups {
		_, err := tx.Exec(
			fmt.Sprintf("INSERT INTO `%s` (`username`, `group`) VALUES (?, ?)", d.groupsTableName),
			usr.Username,
			usr.Groups[i],
		)
		if err != nil {
			d.handleRollback(tx)
			return err
		}
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

// Update @todo use context for all db operations
func (d *UserDatabase) Update(usr *User, usernameToUpdate string) error {
	if usernameToUpdate == "" {
		return errors.New("cannot update user with empty username")
	}

	params := []interface{}{}
	statements := []string{}
	if usr.Password != "" {
		statements = append(statements, "`password` = ?")
		params = append(params, usr.Password)
	}

	if usr.PasswordExpired != nil {
		statements = append(statements, "`password_expired` = ?")
		params = append(params, usr.PasswordExpired)
	}

	if usr.TwoFASendTo != "" {
		statements = append(statements, "`two_fa_send_to` = ?")
		params = append(params, usr.TwoFASendTo)
	}

	if usr.TotP != "" {
		totp, err := d.encryptTotP(usr.TotP)
		if err != nil {
			return err
		}
		statements = append(statements, "`totp_secret` = ?")
		params = append(params, totp)
	}

	if usr.Username != "" && usr.Username != usernameToUpdate {
		statements = append(statements, "`username` = ?")
		params = append(params, usr.Username)
	}

	tx, err := d.db.Beginx()
	if err != nil {
		return err
	}

	if len(params) > 0 {
		q := fmt.Sprintf(
			"UPDATE `%s` SET %s WHERE username = ?",
			d.usersTableName,
			strings.Join(statements, ", "),
		)
		params = append(params, usernameToUpdate)
		_, err := tx.Exec(q, params...)
		if err != nil {
			d.handleRollback(tx)
			return err
		}
	}

	if usr.Username != "" && usernameToUpdate != usr.Username {
		_, err := tx.Exec(
			fmt.Sprintf("UPDATE `%s` SET `username` = ? WHERE `username` = ?", d.groupsTableName),
			usr.Username,
			usernameToUpdate,
		)
		if err != nil {
			d.handleRollback(tx)
			return err
		}
	}

	groupUserName := usernameToUpdate
	if usr.Username != "" {
		groupUserName = usr.Username
	}

	if usr.Groups != nil {
		_, err := tx.Exec(fmt.Sprintf("DELETE FROM `%s` WHERE `username` = ?", d.groupsTableName), groupUserName)
		if err != nil {
			d.handleRollback(tx)
			return err
		}
	}

	for i := range usr.Groups {
		group := usr.Groups[i]
		_, err := tx.Exec(
			fmt.Sprintf("INSERT INTO `%s` (`username`, `group`) VALUES (?, ?)", d.groupsTableName),
			groupUserName,
			group,
		)
		if err != nil {
			d.handleRollback(tx)
			return err
		}
	}

	err = tx.Commit()
	if err != nil {
		return err
	}

	return nil
}

// Delete @todo use context for all db operations
func (d *UserDatabase) Delete(usernameToDelete string) error {
	tx, err := d.db.Beginx()
	if err != nil {
		return err
	}

	_, err = tx.Exec(fmt.Sprintf("DELETE FROM `%s` WHERE `username` = ?", d.usersTableName), usernameToDelete)
	if err != nil {
		d.handleRollback(tx)
		return err
	}

	_, err = tx.Exec(fmt.Sprintf("DELETE FROM `%s` WHERE `username` = ?", d.groupsTableName), usernameToDelete)
	if err != nil {
		d.handleRollback(tx)
		return err
	}

	return tx.Commit()
}

func (d UserDatabase) Type() enums.ProviderSource {
	return enums.ProviderSourceDB
}
func (d UserDatabase) SupportsGroupPermissions() bool {
	return d.groupDetailsTableName != ""
}
