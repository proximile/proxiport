package authorization

import (
	"context"
	"fmt"
	"io"
	"net/http"

	errors2 "github.com/proximile/proxiport/server/api/errors"
)

type DbProvider interface {
	Get(ctx context.Context, username, prefix string) (*APIToken, error)
	GetByName(ctx context.Context, username, name string) (*APIToken, error)
	GetAll(ctx context.Context, username string) ([]*APIToken, error)
	Save(ctx context.Context, tokenLine *APIToken) error
	Delete(ctx context.Context, username, prefix string) error
	DeleteAllByUser(ctx context.Context, username string) error
	RenameUser(ctx context.Context, from, to string) error
	io.Closer
}

type Manager struct {
	db DbProvider
}

func NewManager(db DbProvider) *Manager {
	return &Manager{
		db: db,
	}
}

func (m *Manager) Get(ctx context.Context, username, prefix string) (*APIToken, error) {
	return m.db.Get(ctx, username, prefix)
}

func (m *Manager) GetAll(ctx context.Context, username string) ([]*APIToken, error) {
	val, err := m.db.GetAll(ctx, username)
	if err != nil {
		return nil, err
	}

	return val, nil
}

func (m *Manager) Create(ctx context.Context, tokenLine *APIToken) error {
	err := checkNameExist(ctx, m.db, tokenLine.Username, tokenLine.Name)
	if err != nil {
		return err
	}

	err = m.db.Save(ctx, tokenLine)
	if err != nil {
		return err
	}

	return nil
}

func (m *Manager) Save(ctx context.Context, tokenLine *APIToken) error {
	err := checkNameExist(ctx, m.db, tokenLine.Username, tokenLine.Name)
	if err != nil {
		return err
	}

	err = m.db.Save(ctx, tokenLine)
	if err != nil {
		return err
	}

	return nil
}

func checkNameExist(ctx context.Context, db DbProvider, username, name string) error {
	if name != "" {
		item, err := db.GetByName(ctx, username, name)
		if err != nil {
			return err
		}
		if item != nil {
			//nolint:staticcheck // ST1005: API clients are shown this text as it is
			return fmt.Errorf("A token with the same name already exists")
		}
	}
	return nil
}

func (m *Manager) Delete(ctx context.Context, username, prefix string) error {
	err := m.db.Delete(ctx, username, prefix)
	if err != nil {
		return errors2.APIError{
			Err:        err,
			HTTPStatus: http.StatusInternalServerError,
		}
	}

	return nil
}

// DeleteAllByUser removes every token stored for the username.
func (m *Manager) DeleteAllByUser(ctx context.Context, username string) error {
	return m.db.DeleteAllByUser(ctx, username)
}

// RenameUser moves every token stored for one username to another.
func (m *Manager) RenameUser(ctx context.Context, from, to string) error {
	return m.db.RenameUser(ctx, from, to)
}

func (m *Manager) Close() error {
	return m.db.Close()
}
