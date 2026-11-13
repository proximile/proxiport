package test

import (
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ConvertDbRowsToMapSlice reads every row into a map of column name to value.
//
// A column declared BOOLEAN comes back as a bool. SQLite has no boolean
// storage class, so what a generic scan returns for such a column is up to the
// driver: one hands out the stored integer, another converts it. The server
// always scans these columns into bool fields, and expectations written as
// true/false should not depend on the driver either.
func ConvertDbRowsToMapSlice(dbRows *sqlx.Rows) ([]map[string]interface{}, error) {
	columnTypes, err := dbRows.ColumnTypes()
	if err != nil {
		return nil, err
	}
	boolean := map[string]bool{}
	for _, ct := range columnTypes {
		switch strings.ToUpper(ct.DatabaseTypeName()) {
		case "BOOLEAN", "BOOL":
			boolean[ct.Name()] = true
		}
	}

	dataRows := make([]map[string]interface{}, 0)
	for dbRows.Next() {
		dataRow := make(map[string]interface{})
		err := dbRows.MapScan(dataRow)
		if err != nil {
			return nil, err
		}
		for name := range boolean {
			if n, ok := dataRow[name].(int64); ok {
				dataRow[name] = n != 0
			}
		}
		dataRows = append(dataRows, dataRow)
	}

	return dataRows, nil
}

func AssertRowsEqual(t *testing.T, db *sqlx.DB, expectedRows []map[string]interface{}, query string, params []interface{}) {
	dbRows, err := db.Queryx(query, params...)
	require.NoError(t, err)

	actualRows, err := ConvertDbRowsToMapSlice(dbRows)
	require.NoError(t, err)
	assert.Equal(t, expectedRows, actualRows)
}
