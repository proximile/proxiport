package query_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/proximile/proxiport/share/query"
)

func TestValidatePagination(t *testing.T) {
	testCases := []struct {
		Name          string
		Pagination    *query.Pagination
		ExpectedLimit string
		ExpectedErrs  []string
	}{
		{
			Name: "set default limit",
			Pagination: &query.Pagination{
				Offset: "0",
			},
			ExpectedLimit: "10",
		}, {
			Name: "ok",
			Pagination: &query.Pagination{
				Offset: "0",
				Limit:  "15",
			},
			ExpectedLimit: "15",
		}, {

			Name: "not numbers",
			Pagination: &query.Pagination{
				Offset: "ab",
				Limit:  "cd",
			},
			ExpectedErrs: []string{
				"pagination limit must be a number",
				"pagination offset must be a number",
			},
		}, {
			Name: "negative",
			Pagination: &query.Pagination{
				Offset: "-1",
				Limit:  "0",
			},
			ExpectedErrs: []string{
				"pagination limit must be positive",
				"pagination offset must not be negative",
			},
		}, {
			Name: "too big",
			Pagination: &query.Pagination{
				Offset: "0",
				Limit:  "1000",
			},
			ExpectedErrs: []string{
				"pagination limit too big (1000) maximum is 100",
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			errs := query.ValidatePagination(tc.Pagination, &query.PaginationConfig{
				DefaultLimit: 10,
				MaxLimit:     100,
			})

			assert.Equal(t, len(tc.ExpectedErrs), len(errs))
			for i := range tc.ExpectedErrs {
				assert.Equal(t, tc.ExpectedErrs[i], errs[i].Message)
			}
			if tc.ExpectedLimit != "" {
				assert.Equal(t, tc.ExpectedLimit, tc.Pagination.Limit)
			}
		})
	}
}

func TestGetStartEnd(t *testing.T) {
	testCases := []struct {
		Name          string
		TotalCount    int
		ExpectedStart int
		ExpectedEnd   int
	}{
		{
			Name:          "total greater",
			TotalCount:    30,
			ExpectedStart: 10,
			ExpectedEnd:   20,
		},
		{
			Name:          "total less than end",
			TotalCount:    15,
			ExpectedStart: 10,
			ExpectedEnd:   15,
		},
		{
			Name:          "total less than start",
			TotalCount:    5,
			ExpectedStart: 5,
			ExpectedEnd:   5,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			pagination := &query.Pagination{
				ValidatedOffset: 10,
				ValidatedLimit:  10,
			}
			start, end := pagination.GetStartEnd(tc.TotalCount)

			assert.Equal(t, tc.ExpectedStart, start)
			assert.Equal(t, tc.ExpectedEnd, end)
		})
	}
}

func TestNewPagination(t *testing.T) {
	pagination := query.NewPagination(10, 20)

	assert.Equal(t, pagination.Limit, "10")
	assert.Equal(t, pagination.Offset, "20")
	assert.Equal(t, pagination.ValidatedLimit, 10)
	assert.Equal(t, pagination.ValidatedOffset, 20)
}

// TestGetStartEndNeverReturnsAnInvalidSliceRange is L5. ValidatePagination
// bounds page[offset] only from below, so an offset of MaxInt64 is accepted;
// end was then computed as raw offset + limit, which wraps to a large negative
// number and walks straight past the `end > totalCount` clamp. Every caller
// slices with the result, so one query parameter panicked six list endpoints --
// recovered into a 500, but with a full goroutine stack written to the log on
// every request.
func TestGetStartEndNeverReturnsAnInvalidSliceRange(t *testing.T) {
	testCases := []struct {
		Name       string
		Offset     int
		Limit      int
		TotalCount int
	}{
		{Name: "offset at MaxInt64", Offset: math.MaxInt64, Limit: 20, TotalCount: 5},
		{Name: "offset one below MaxInt64", Offset: math.MaxInt64 - 1, Limit: 1, TotalCount: 100},
		{Name: "offset and limit both large", Offset: math.MaxInt64, Limit: math.MaxInt64, TotalCount: 3},
		{Name: "offset past the end", Offset: 1000, Limit: 10, TotalCount: 5},
		{Name: "empty collection", Offset: math.MaxInt64, Limit: 10, TotalCount: 0},
		{Name: "ordinary page", Offset: 10, Limit: 10, TotalCount: 30},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			pagination := &query.Pagination{ValidatedOffset: tc.Offset, ValidatedLimit: tc.Limit}
			start, end := pagination.GetStartEnd(tc.TotalCount)

			assert.GreaterOrEqual(t, start, 0, "start must not be negative")
			assert.LessOrEqual(t, start, end, "start must not run past end")
			assert.LessOrEqual(t, end, tc.TotalCount, "end must not run past the collection")

			// The assertions above are the contract; this is the thing that
			// actually panicked, so slice for real.
			items := make([]int, tc.TotalCount)
			assert.NotPanics(t, func() { _ = items[start:end] })
		})
	}
}
