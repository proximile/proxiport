package query

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errors2 "github.com/proximile/proxiport/server/api/errors"
)

func TestValidateFilterOptions(t *testing.T) {
	testCases := []struct {
		Name                        string
		FilterOptions               []FilterOption
		SupportedFilterFields       map[string]bool
		ExpectedAPIErrors           errors2.APIErrors
		ExpectedFilterOptionColumns []string
	}{
		{
			Name: "filter fields without sub filter, ok",
			FilterOptions: []FilterOption{
				{
					Column: []string{"name"},
					Values: []string{"val1"},
				},
			},
			SupportedFilterFields: map[string]bool{"name": true},
			ExpectedAPIErrors:     nil,
		},
		{
			Name: "filter fields with sub filter, ok",
			FilterOptions: []FilterOption{
				{
					Column:   []string{"timestamp"},
					Operator: FilterOperatorTypeGT,
				},
				{
					Column:   []string{"timestamp"},
					Operator: FilterOperatorTypeLT,
				},
				{
					Column:   []string{"timestamp"},
					Operator: FilterOperatorTypeSince,
				},
				{
					Column:   []string{"timestamp"},
					Operator: FilterOperatorTypeUntil,
				},
			},
			SupportedFilterFields: map[string]bool{"timestamp[gt]": true, "timestamp[lt]": true, "timestamp[since]": true, "timestamp[until]": true},
			ExpectedAPIErrors:     nil,
		},
		{
			Name: "filter fields without sub filter, not ok",
			FilterOptions: []FilterOption{
				{
					Column: []string{"name"},
					Values: []string{"val1"},
				},
			},
			SupportedFilterFields: map[string]bool{"field1": true},
			ExpectedAPIErrors: errors2.APIErrors{
				errors2.APIError{
					Message:    fmt.Sprintf("unsupported filter field '%s'", "filter[name]"),
					HTTPStatus: http.StatusBadRequest,
				},
			},
		},
		{
			Name: "filter fields with sub filter, not ok",
			FilterOptions: []FilterOption{
				{
					Column:   []string{"timestamp"},
					Operator: "gt",
					Values:   []string{"val1"},
				},
				{
					Column:   []string{"timestamp"},
					Operator: "eq",
					Values:   []string{"value2"},
				},
			},
			SupportedFilterFields: map[string]bool{"timestamp[gt]": true, "timestamp[lt]": true},
			ExpectedAPIErrors: errors2.APIErrors{
				errors2.APIError{
					Message:    fmt.Sprintf("unsupported filter field '%s'", "filter[timestamp][eq]"),
					HTTPStatus: http.StatusBadRequest,
				},
			},
		},
		{
			Name: "wildcard filter",
			FilterOptions: []FilterOption{
				{
					Column: []string{"*"},
					Values: []string{"val1"},
				},
			},
			SupportedFilterFields:       map[string]bool{"field1": true, "field2": true},
			ExpectedAPIErrors:           nil,
			ExpectedFilterOptionColumns: []string{"field1", "field2"},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			errs := ValidateFilterOptions(tc.FilterOptions, tc.SupportedFilterFields)

			assert.Equal(t, tc.ExpectedAPIErrors, errs)
			if tc.ExpectedFilterOptionColumns != nil {
				assert.ElementsMatch(t, tc.ExpectedFilterOptionColumns, tc.FilterOptions[0].Column)
			}
		})
	}

}

func TestParseFilterOptions(t *testing.T) {
	testCases := []struct {
		Name                  string
		Query                 map[string][]string
		ExpectedFilterOptions []FilterOption
	}{
		{
			Name: "filter fields with sub filter, ok",
			Query: map[string][]string{
				"filter[timestamp][gt]": {"1634303188"},
				"filter[timestamp][lt]": {"1634303609"},
			},
			ExpectedFilterOptions: []FilterOption{
				{
					Column:   []string{"timestamp"},
					Operator: FilterOperatorTypeGT,
					Values:   []string{"1634303188"},
				},
				{
					Column:   []string{"timestamp"},
					Operator: FilterOperatorTypeLT,
					Values:   []string{"1634303609"},
				},
			},
		},
		{
			Name: "filter fields with sub filter, not known sub filter",
			Query: map[string][]string{
				"filter[timestamp][xx]": {"1634303188"},
				"filter[timestamp][yy]": {"1634303609"},
			},
			ExpectedFilterOptions: []FilterOption{
				{
					Column:   []string{"timestamp"},
					Operator: "xx",
					Values:   []string{"1634303188"},
				},
				{
					Column:   []string{"timestamp"},
					Operator: "yy",
					Values:   []string{"1634303609"},
				},
			},
		},
		{
			Name: "filter fields without sub filter, ok",
			Query: map[string][]string{
				"filter[name]": {"val1"},
			},
			ExpectedFilterOptions: []FilterOption{
				{
					Column: []string{"name"},
					Values: []string{"val1"},
				},
			},
		},
		{
			Name: "multiple columns, ok",
			Query: map[string][]string{
				"filter[name|other]": {"val1"},
			},
			ExpectedFilterOptions: []FilterOption{
				{
					Column: []string{"name", "other"},
					Values: []string{"val1"},
				},
			},
		},
		{
			Name: "column with underscored",
			Query: map[string][]string{
				"filter[some_column_123]": {"val1"},
			},
			ExpectedFilterOptions: []FilterOption{
				{
					Column: []string{"some_column_123"},
					Values: []string{"val1"},
				},
			},
		},
		{
			Name: "filter without fields, not ok",
			Query: map[string][]string{
				"filter": {"val1"},
			},
			ExpectedFilterOptions: []FilterOption{},
		},
		{
			Name: "and beetween tags",
			Query: map[string][]string{
				"filter[tags]": {"and(val1, val2)"},
			},
			ExpectedFilterOptions: []FilterOption{
				{
					Column:                []string{"tags"},
					ValuesLogicalOperator: FilterLogicalOperatorTypeAND,
					Values:                []string{"val1", "val2"},
				},
			},
		},
		{
			Name: "or beetween tags",
			Query: map[string][]string{
				"filter[tags]": {"or(val1, val2)"},
			},
			ExpectedFilterOptions: []FilterOption{
				{
					Column:                []string{"tags"},
					ValuesLogicalOperator: FilterLogicalOperatorTypeOR,
					Values:                []string{"val1", "val2"},
				},
			},
		},
		{
			Name: "or beetween tags old notation",
			Query: map[string][]string{
				"filter[tags]": {"val1", "val2", "val3"},
			},
			ExpectedFilterOptions: []FilterOption{
				{
					Column: []string{"tags"},
					Values: []string{"val1", "val2", "val3"},
				},
			},
		},
		{
			Name: "and beetween tags new notation, more than two fields",
			Query: map[string][]string{
				"filter[tags]": {"and(val1, val2, val3)"},
			},
			ExpectedFilterOptions: []FilterOption{
				{
					Column:                []string{"tags"},
					Values:                []string{"val1", "val2", "val3"},
					ValuesLogicalOperator: FilterLogicalOperatorTypeAND,
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			filterOptions := ParseFilterOptions(tc.Query)

			assert.ElementsMatch(t, tc.ExpectedFilterOptions, filterOptions)
		})
	}
}

func TestSortFiltersByOperator(t *testing.T) {
	testCases := []struct {
		Name                  string
		FilterOptions         []FilterOption
		ExpectedFilterOptions []FilterOption
	}{
		{
			Name: "filter fields with sub filter",
			FilterOptions: []FilterOption{
				{
					Column:   []string{"timestamp"},
					Operator: "until",
					Values:   []string{"2021-09-29:11:00:00"},
				},
				{
					Column:   []string{"timestamp"},
					Operator: "lt",
					Values:   []string{"1634303609"},
				},
				{
					Column:   []string{"timestamp"},
					Operator: "since",
					Values:   []string{"2021-09-29:10:00:00"},
				},
				{
					Column:   []string{"timestamp"},
					Operator: "gt",
					Values:   []string{"1634303188"},
				},
			},
			ExpectedFilterOptions: []FilterOption{
				{
					Column:   []string{"timestamp"},
					Operator: "gt",
					Values:   []string{"1634303188"},
				},
				{
					Column:   []string{"timestamp"},
					Operator: "lt",
					Values:   []string{"1634303609"},
				},
				{
					Column:   []string{"timestamp"},
					Operator: "since",
					Values:   []string{"2021-09-29:10:00:00"},
				},
				{
					Column:   []string{"timestamp"},
					Operator: "until",
					Values:   []string{"2021-09-29:11:00:00"},
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			filterOptions := tc.FilterOptions
			SortFiltersByOperator(filterOptions)

			assert.Equal(t, tc.ExpectedFilterOptions, filterOptions)
		})
	}
}

func TestSplitFilters(t *testing.T) {
	options := []FilterOption{
		{
			Column: []string{"c1"},
			Values: []string{"v1"},
		},
		{
			Column: []string{"c2"},
			Values: []string{"v1"},
		},
	}

	opt1, opt2 := SplitFilters(options, map[string]bool{"c1": true})

	assert.Equal(t, options[0:1], opt1)
	assert.Equal(t, options[1:2], opt2)
}

// TestWildcardFilterExpandsToUsableColumns is L2. setWildcardColumns copied
// every key of the supported-filter map into the SQL as a column name, and a
// range filter's key carries its operator -- "timestamp[gt]". SQLite reads that
// as the identifier timestamp followed by the bracket-quoted identifier [gt]
// and refuses to prepare the statement, so filter[*] answered HTTP 500 on every
// endpoint family whose map declares a range filter, and worked on every one
// whose map does not.
func TestWildcardFilterExpandsToUsableColumns(t *testing.T) {
	t.Run("operator-suffixed keys never reach the SQL", func(t *testing.T) {
		// The notification-log map, which is the one whose documentation
		// explicitly advertises the wildcard form.
		supported := map[string]bool{
			"state":            true,
			"reference_id":     true,
			"transport":        true,
			"subject":          true,
			"timestamp[gt]":    true,
			"timestamp[lt]":    true,
			"timestamp[since]": true,
			"timestamp[until]": true,
		}

		fo := []FilterOption{{Column: []string{"*"}, Values: []string{"error"}}}
		require.Nil(t, ValidateFilterOptions(fo, supported))

		assert.Equal(t, []string{"reference_id", "state", "subject", "transport"}, fo[0].Column,
			"the expansion must be the equality-filterable fields, in a stable order")

		converter := NewSQLConverter("sqlite")
		q, params := converter.AddWhere(fo, "SELECT * FROM notifications_log", nil)
		assert.NotContains(t, q, "[", "no bracketed identifier may reach the statement")
		assert.Len(t, params, 4)
	})

	t.Run("a map with nothing but range filters is rejected, not mis-expanded", func(t *testing.T) {
		// server/monitoring's metrics, processes and mountpoints families
		// declare only timestamp ranges, so there is no field a wildcard
		// equality match could mean.
		supported := map[string]bool{
			"timestamp[gt]":    true,
			"timestamp[lt]":    true,
			"timestamp[since]": true,
			"timestamp[until]": true,
		}

		fo := []FilterOption{{Column: []string{"*"}, Values: []string{"anything"}}}
		errs := ValidateFilterOptions(fo, supported)
		require.NotNil(t, errs, "an unserveable wildcard must be a 400, not an empty WHERE clause")
		assert.Contains(t, errs.Error(), "unsupported filter field")
	})

	t.Run("an explicit range filter is unaffected", func(t *testing.T) {
		supported := map[string]bool{"timestamp[gt]": true}

		fo := []FilterOption{{
			Column:   []string{"timestamp"},
			Operator: FilterOperatorTypeGT,
			Values:   []string{"2026-09-15T00:00:00Z"},
		}}
		require.Nil(t, ValidateFilterOptions(fo, supported))

		converter := NewSQLConverter("sqlite")
		q, _ := converter.AddWhere(fo, "SELECT * FROM measurements", nil)
		assert.Contains(t, q, "WHERE timestamp > ?")
	})
}
