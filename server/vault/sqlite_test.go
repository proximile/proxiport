package vault

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	chshare "github.com/proximile/proxiport/share/logger"
	"github.com/proximile/proxiport/share/query"

	"github.com/jmoiron/sqlx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/test"
)

var testLog = chshare.NewLogger("vault", chshare.LogOutput{File: os.Stdout}, chshare.LogLevelDebug)

type configMock struct {
}

func (cm configMock) GetVaultDBPath() string {
	return ":memory:"
}

func TestSetStatus(t *testing.T) {
	dbProv, err := NewSqliteProvider(configMock{}, testLog)
	require.NoError(t, err)

	defer dbProv.Close()

	statusToSet := DbStatus{
		StatusName:    DbStatusInit,
		EncCheckValue: "123",
		KDF:           "argon2id$m=1,t=1,p=1$c2FsdA==",
	}
	err = dbProv.SetStatus(context.Background(), statusToSet)
	require.NoError(t, err)

	expectedRows := []map[string]interface{}{
		{
			"db_status": DbStatusInit,
			"enc_check": "123",
			"kdf":       "argon2id$m=1,t=1,p=1$c2FsdA==",
		},
	}
	query := "SELECT `db_status`, `enc_check`, `kdf` FROM `status`"
	test.AssertRowsEqual(t, dbProv.db, expectedRows, query, []interface{}{})

	statusToSet.EncCheckValue = "678"
	statusToSet.KDF = "argon2id$m=2,t=2,p=2$c2FsdA=="
	statusToSet.StatusName = DbStatusNotInit

	err = dbProv.SetStatus(context.Background(), statusToSet)
	require.NoError(t, err)

	expectedRows2 := []map[string]interface{}{
		{
			"db_status": DbStatusNotInit,
			"enc_check": "678",
			"kdf":       "argon2id$m=2,t=2,p=2$c2FsdA==",
		},
	}
	test.AssertRowsEqual(t, dbProv.db, expectedRows2, query, []interface{}{})
}

func TestGetStatus(t *testing.T) {
	dbProv, err := NewSqliteProvider(configMock{}, testLog)
	require.NoError(t, err)
	defer dbProv.Close()

	dbStatus, err := dbProv.GetStatus(context.Background())
	require.NoError(t, err)
	assert.Equal(
		t,
		DbStatus{
			ID:            0,
			StatusName:    "",
			EncCheckValue: "",
			KDF:           "",
		},
		dbStatus,
	)

	_, err = dbProv.db.Exec("INSERT INTO `status` (`db_status`, `enc_check`, `kdf`) VALUES ('someStatus', 'someEnc', 'someKdf')")
	require.NoError(t, err)

	dbStatus, err = dbProv.GetStatus(context.Background())
	require.NoError(t, err)

	assert.Equal(
		t,
		DbStatus{
			ID:            1,
			StatusName:    "someStatus",
			EncCheckValue: "someEnc",
			KDF:           "someKdf",
		},
		dbStatus,
	)
}

func TestReKey(t *testing.T) {
	dbProv, err := NewSqliteProvider(configMock{}, testLog)
	require.NoError(t, err)
	defer func() { _ = dbProv.Close() }()

	ctx := context.Background()

	require.NoError(t, dbProv.SetStatus(ctx, DbStatus{
		StatusName:    DbStatusInit,
		EncCheckValue: "old-enc",
		KDF:           "",
	}))
	require.NoError(t, addDemoData(dbProv.db))

	const newKDF = "argon2id$m=65536,t=3,p=4$c2FsdA=="
	transform := func(old string) (string, error) { return "reenc:" + old, nil }

	require.NoError(t, dbProv.ReKey(ctx, transform, newKDF, "new-enc"))

	v1, _, err := dbProv.GetByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, "reenc:val1", v1.Value)
	v2, _, err := dbProv.GetByID(ctx, 2)
	require.NoError(t, err)
	assert.Equal(t, "reenc:val2", v2.Value)

	st, err := dbProv.GetStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, newKDF, st.KDF)
	assert.Equal(t, "new-enc", st.EncCheckValue)
	assert.Equal(t, DbStatusInit, st.StatusName)
}

func TestReKeyTransformErrorRollsBack(t *testing.T) {
	dbProv, err := NewSqliteProvider(configMock{}, testLog)
	require.NoError(t, err)
	defer func() { _ = dbProv.Close() }()

	ctx := context.Background()

	require.NoError(t, dbProv.SetStatus(ctx, DbStatus{
		StatusName:    DbStatusInit,
		EncCheckValue: "old-enc",
		KDF:           "",
	}))
	require.NoError(t, addDemoData(dbProv.db))

	transform := func(old string) (string, error) { return "", errors.New("boom") }
	err = dbProv.ReKey(ctx, transform, "some-kdf", "new-enc")
	require.EqualError(t, err, "boom")

	// The transaction must have rolled back: values and status are unchanged.
	v1, _, err := dbProv.GetByID(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, "val1", v1.Value)

	st, err := dbProv.GetStatus(ctx)
	require.NoError(t, err)
	assert.Equal(t, "", st.KDF)
	assert.Equal(t, "old-enc", st.EncCheckValue)
}

func TestGetByID(t *testing.T) {
	dbProv, err := NewSqliteProvider(configMock{}, testLog)
	require.NoError(t, err)
	defer dbProv.Close()

	ctx := context.Background()

	err = addDemoData(dbProv.db)
	require.NoError(t, err)

	val, found, err := dbProv.GetByID(ctx, 1)

	require.NoError(t, err)
	require.True(t, found)
	expectedCreatedAt, err := time.Parse("2006-01-02 15:04:05", "2001-01-01 00:00:00")
	require.NoError(t, err)
	assert.Equal(
		t,
		StoredValue{
			InputValue: InputValue{
				ClientID:      "client1",
				RequiredGroup: "group1",
				Key:           "key1",
				Value:         "val1",
				Type:          "type1",
			},
			ID:        1,
			CreatedAt: expectedCreatedAt,
			UpdatedAt: expectedCreatedAt,
			CreatedBy: "user1",
			UpdatedBy: nil,
		},
		val,
	)

	_, found, err = dbProv.GetByID(ctx, -2)
	require.NoError(t, err)
	require.False(t, found)
}

func TestList(t *testing.T) {
	dbProv, err := NewSqliteProvider(configMock{}, testLog)
	require.NoError(t, err)
	defer dbProv.Close()

	err = addDemoData(dbProv.db)
	require.NoError(t, err)
	expectedCreatedAt, err := time.Parse("2006-01-02 15:04:05", "2001-01-01 00:00:00")
	require.NoError(t, err)

	vals, err := dbProv.List(context.Background(), &query.ListOptions{})
	require.NoError(t, err)
	assert.Equal(
		t,
		[]ValueKey{
			{
				ID:        1,
				ClientID:  "client1",
				CreatedBy: "user1",
				CreatedAt: expectedCreatedAt,
				Key:       "key1",
			},
			{
				ID:        2,
				ClientID:  "client2",
				CreatedBy: "user1",
				CreatedAt: expectedCreatedAt,
				Key:       "key2",
			},
		},
		vals,
	)

	vals, err = dbProv.List(context.Background(), &query.ListOptions{
		Sorts: []query.SortOption{
			{
				Column: "client_id",
				IsASC:  false,
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(
		t,
		[]ValueKey{
			{
				ID:        2,
				ClientID:  "client2",
				CreatedBy: "user1",
				CreatedAt: expectedCreatedAt,
				Key:       "key2",
			},
			{
				ID:        1,
				ClientID:  "client1",
				CreatedBy: "user1",
				CreatedAt: expectedCreatedAt,
				Key:       "key1",
			},
		},
		vals,
	)

	vals, err = dbProv.List(context.Background(), &query.ListOptions{
		Sorts: []query.SortOption{
			{
				Column: "key",
				IsASC:  true,
			},
		},
		Filters: []query.FilterOption{
			{
				Column: []string{"created_by"},
				Values: []string{"user1"},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(
		t,
		[]ValueKey{
			{
				ID:        1,
				ClientID:  "client1",
				CreatedBy: "user1",
				CreatedAt: expectedCreatedAt,
				Key:       "key1",
			},
			{
				ID:        2,
				ClientID:  "client2",
				CreatedBy: "user1",
				CreatedAt: expectedCreatedAt,
				Key:       "key2",
			},
		},
		vals,
	)

	vals, err = dbProv.List(context.Background(), &query.ListOptions{
		Filters: []query.FilterOption{
			{
				Column: []string{"client_id"},
				Values: []string{"notExistingClient"},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []ValueKey{}, vals)

	vals, err = dbProv.List(context.Background(), &query.ListOptions{
		Filters: []query.FilterOption{
			{
				Column: []string{"key"},
				Values: []string{"key1"},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(
		t,
		[]ValueKey{
			{
				ID:        1,
				ClientID:  "client1",
				CreatedBy: "user1",
				CreatedAt: expectedCreatedAt,
				Key:       "key1",
			},
		},
		vals,
	)

	vals, err = dbProv.List(context.Background(), &query.ListOptions{
		Sorts: []query.SortOption{
			{
				Column: "key",
				IsASC:  true,
			},
		},
		Filters: []query.FilterOption{
			{
				Column: []string{"key"},
				Values: []string{"key1", "key2"},
			},
			{
				Column: []string{"created_by"},
				Values: []string{"user1"},
			},
		},
	})
	require.NoError(t, err)
	assert.Equal(
		t,
		[]ValueKey{
			{
				ID:        1,
				ClientID:  "client1",
				CreatedBy: "user1",
				CreatedAt: expectedCreatedAt,
				Key:       "key1",
			},
			{
				ID:        2,
				ClientID:  "client2",
				CreatedBy: "user1",
				CreatedAt: expectedCreatedAt,
				Key:       "key2",
			},
		},
		vals,
	)
}

func TestCreate(t *testing.T) {
	dbProv, err := NewSqliteProvider(configMock{}, testLog)
	require.NoError(t, err)
	defer dbProv.Close()

	expectedCreatedAt, err := time.Parse("2006-01-02 15:04:05", "2001-01-01 00:00:00")
	require.NoError(t, err)

	ctx := context.Background()

	id, err := dbProv.Save(
		ctx,
		"user123",
		0,
		&InputValue{
			ClientID:      "client123",
			RequiredGroup: "group123",
			Key:           "key123",
			Value:         "value123",
			Type:          "typ123",
		},
		expectedCreatedAt,
	)
	require.NoError(t, err)
	assert.True(t, id > 0)

	expectedRows := []map[string]interface{}{
		{
			"id":             int64(1),
			"client_id":      "client123",
			"required_group": "group123",
			"created_at":     expectedCreatedAt,
			"created_by":     "user123",
			"updated_at":     expectedCreatedAt,
			"updated_by":     "user123",
			"key":            "key123",
			"value":          "value123",
			"type":           "typ123",
		},
	}
	query := "SELECT * FROM `values`"
	test.AssertRowsEqual(t, dbProv.db, expectedRows, query, []interface{}{})
}

func TestUpdate(t *testing.T) {
	dbProv, err := NewSqliteProvider(configMock{}, testLog)
	require.NoError(t, err)
	defer dbProv.Close()

	expectedCreatedAt, err := time.Parse("2006-01-02 15:04:05", "2001-01-01 00:00:00")
	require.NoError(t, err)

	expectedUpdatedAt, err := time.Parse("2006-01-02 15:04:05", "2001-01-02 00:00:00")
	require.NoError(t, err)

	ctx := context.Background()

	err = addDemoData(dbProv.db)
	require.NoError(t, err)

	id, err := dbProv.Save(
		ctx,
		"user123",
		1,
		&InputValue{
			ClientID:      "client123",
			RequiredGroup: "group123",
			Key:           "key123",
			Value:         "value123",
			Type:          "typ123",
		},
		expectedUpdatedAt,
	)
	require.NoError(t, err)
	assert.True(t, id > 0)

	expectedRows := []map[string]interface{}{
		{
			"id":             int64(1),
			"client_id":      "client123",
			"required_group": "group123",
			"created_at":     expectedCreatedAt,
			"created_by":     "user1",
			"updated_at":     expectedUpdatedAt,
			"updated_by":     "user123",
			"key":            "key123",
			"value":          "value123",
			"type":           "typ123",
		},
	}
	query := "SELECT * FROM `values` where id = 1"
	test.AssertRowsEqual(t, dbProv.db, expectedRows, query, []interface{}{})
}

func TestFindByKeyAndClientID(t *testing.T) {
	dbProv, err := NewSqliteProvider(configMock{}, testLog)
	require.NoError(t, err)
	defer dbProv.Close()

	expectedCreatedAt, err := time.Parse("2006-01-02 15:04:05", "2001-01-01 00:00:00")
	require.NoError(t, err)

	ctx := context.Background()

	err = addDemoData(dbProv.db)
	require.NoError(t, err)

	_, found, err := dbProv.FindByKeyAndClientID(ctx, "key1", "unknownClient")
	require.NoError(t, err)
	assert.False(t, found)

	_, found, err = dbProv.FindByKeyAndClientID(ctx, "unknownKey", "client1")
	require.NoError(t, err)
	assert.False(t, found)

	val, found, err := dbProv.FindByKeyAndClientID(ctx, "key1", "client1")
	require.NoError(t, err)
	assert.True(t, found)

	assert.Equal(
		t,
		StoredValue{
			InputValue: InputValue{
				ClientID:      "client1",
				RequiredGroup: "group1",
				Key:           "key1",
				Value:         "val1",
				Type:          "type1",
			},
			ID:        1,
			CreatedAt: expectedCreatedAt,
			UpdatedAt: expectedCreatedAt,
			CreatedBy: "user1",
			UpdatedBy: nil,
		},
		val,
	)
}

func TestDelete(t *testing.T) {
	dbProv, err := NewSqliteProvider(configMock{}, testLog)
	require.NoError(t, err)
	defer dbProv.Close()

	ctx := context.Background()

	expectedCreatedAt, err := time.Parse("2006-01-02 15:04:05", "2001-01-01 00:00:00")
	require.NoError(t, err)

	err = addDemoData(dbProv.db)
	require.NoError(t, err)

	err = dbProv.Delete(ctx, -2)
	assert.EqualError(t, err, "cannot find entry by id -2")

	err = dbProv.Delete(ctx, 1)
	require.NoError(t, err)

	expectedRows := []map[string]interface{}{
		{
			"id":             int64(2),
			"client_id":      "client2",
			"required_group": "group1",
			"created_at":     expectedCreatedAt,
			"created_by":     "user1",
			"updated_at":     expectedCreatedAt,
			"updated_by":     nil,
			"key":            "key2",
			"value":          "val2",
			"type":           "type2",
		},
	}
	query := "SELECT * FROM `values`"
	test.AssertRowsEqual(t, dbProv.db, expectedRows, query, []interface{}{})
}

func addDemoData(db *sqlx.DB) error {
	demoDate, err := time.Parse("2006-01-02 15:04:05", "2001-01-01 00:00:00")
	if err != nil {
		return err
	}
	demoData := []StoredValue{
		{
			InputValue: InputValue{
				ClientID:      "client1",
				RequiredGroup: "group1",
				Key:           "key1",
				Value:         "val1",
				Type:          "type1",
			},
			CreatedAt: demoDate,
			UpdatedAt: demoDate,
			CreatedBy: "user1",
			UpdatedBy: nil,
		},
		{
			InputValue: InputValue{
				ClientID:      "client2",
				RequiredGroup: "group1",
				Key:           "key2",
				Value:         "val2",
				Type:          "type2",
			},
			CreatedAt: demoDate,
			UpdatedAt: demoDate,
			CreatedBy: "user1",
			UpdatedBy: nil,
		},
	}

	for i := range demoData {
		_, err = db.Exec(
			"INSERT INTO `values` (`client_id`, `required_group`, `key`, `value`, `created_at`, `updated_at`, `created_by`, `type`) VALUES (?,?,?,?,?,?,?,?)",
			demoData[i].ClientID,
			demoData[i].RequiredGroup,
			demoData[i].Key,
			demoData[i].Value,
			demoData[i].CreatedAt.Format(time.RFC3339),
			demoData[i].UpdatedAt.Format(time.RFC3339),
			demoData[i].CreatedBy,
			demoData[i].Type,
		)
		if err != nil {
			return err
		}
	}

	return nil
}
