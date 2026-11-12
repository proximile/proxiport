package usercli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jmoiron/sqlx"

	"github.com/proximile/proxiport/db/migration/api_token"
	"github.com/proximile/proxiport/db/sqlite"
	"github.com/proximile/proxiport/server/api/authorization"
	"github.com/proximile/proxiport/server/api/session"
	"github.com/proximile/proxiport/server/api/users"
	"github.com/proximile/proxiport/server/chconfig"
	"github.com/proximile/proxiport/share/enums"
)

type UserInput struct {
	Username    string
	Password    string
	Groups      []string
	TwoFASendTo string
}

type UserService interface {
	Create(input UserInput) error
	Change(input UserInput) error
	Delete(username string) error
	ProviderType() enums.ProviderSource
}

func NewUserService(cfg *chconfig.Config) (UserService, error) {
	var authDB *sqlx.DB
	var err error
	if cfg.Database.Driver != "" {
		authDB, err = sqlx.Connect(cfg.Database.Driver, cfg.Database.Dsn)
		if err != nil {
			//nolint:staticcheck // ST1005: printed to the operator as a sentence of its own
			return nil, fmt.Errorf("Could not connect to user database: %w", err)
		}
	}

	apiUserService, err := users.NewAPIServiceFromConfig(authDB, cfg)
	if err != nil {
		//nolint:staticcheck // ST1005: printed to the operator as a sentence of its own
		return nil, fmt.Errorf("Could not initialize user service: %w", err)
	}

	tokens, err := openAPITokens(cfg)
	if err != nil {
		return nil, err
	}
	if tokens != nil {
		apiUserService.AddCredentialStore(tokens)
	}

	sessions, err := openAPISessions(cfg)
	if err != nil {
		return nil, err
	}
	if sessions != nil {
		apiUserService.AddCredentialStore(sessions)
	}

	return &userServiceImpl{
		apiUserService: apiUserService,
	}, nil
}

// openAPITokens opens the server's API token database, so that deleting a user
// here deletes their tokens too. A data directory the server has not created a
// token database in yet has no tokens to delete, and gets none created: this
// command may run as another account than the server.
func openAPITokens(cfg *chconfig.Config) (*authorization.SqliteProvider, error) {
	dbPath := filepath.Join(cfg.Server.DataDir, "api_token.db")
	if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		//nolint:staticcheck // ST1005: printed to the operator as a sentence of its own
		return nil, fmt.Errorf("Could not open the API token database: %w", err)
	}

	db, err := sqlite.New(dbPath, api_token.AssetNames(), api_token.Asset, cfg.Server.GetSQLiteDataSourceOptions())
	if err != nil {
		//nolint:staticcheck // ST1005: printed to the operator as a sentence of its own
		return nil, fmt.Errorf("Could not open the API token database: %w", err)
	}

	return authorization.NewSqliteProvider(db), nil
}

// openAPISessions opens the server's stored API sessions, under the same rule
// as openAPITokens. A running server also holds its sessions in memory, which
// this command cannot reach: deleting them here keeps them from being loaded
// again at the next start.
func openAPISessions(cfg *chconfig.Config) (*session.SqliteProvider, error) {
	dbPath := filepath.Join(cfg.Server.DataDir, "api_sessions.db")
	if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		//nolint:staticcheck // ST1005: printed to the operator as a sentence of its own
		return nil, fmt.Errorf("Could not open the API session database: %w", err)
	}

	sessions, err := session.NewSqliteProvider(dbPath, cfg.Server.GetSQLiteDataSourceOptions())
	if err != nil {
		//nolint:staticcheck // ST1005: printed to the operator as a sentence of its own
		return nil, fmt.Errorf("Could not open the API session database: %w", err)
	}

	return sessions, nil
}

type userServiceImpl struct {
	apiUserService *users.APIService
}

func (u *userServiceImpl) Create(input UserInput) error {
	return u.apiUserService.Change(&users.User{
		Username:    input.Username,
		Password:    input.Password,
		Groups:      input.Groups,
		TwoFASendTo: input.TwoFASendTo,
	}, "")
}

func (u *userServiceImpl) Change(input UserInput) error {
	return u.apiUserService.Change(&users.User{
		Password:    input.Password,
		Groups:      input.Groups,
		TwoFASendTo: input.TwoFASendTo,
	}, input.Username)
}

func (u *userServiceImpl) Delete(username string) error {
	return u.apiUserService.Delete(username)
}

func (u *userServiceImpl) ProviderType() enums.ProviderSource {
	return u.apiUserService.GetProviderType()
}
