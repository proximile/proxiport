package query

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	apierrors "github.com/proximile/proxiport/server/api/errors"
)

type Pagination struct {
	Limit  string
	Offset string

	ValidatedLimit  int
	ValidatedOffset int
}

type PaginationConfig struct {
	MaxLimit     int
	DefaultLimit int
}

func NewPagination(limit, offset int) *Pagination {
	return &Pagination{
		Limit:           strconv.Itoa(limit),
		Offset:          strconv.Itoa(offset),
		ValidatedLimit:  limit,
		ValidatedOffset: offset,
	}
}

func ValidatePagination(pagination *Pagination, config *PaginationConfig) apierrors.APIErrors {
	if pagination == nil {
		return nil
	}

	errs := apierrors.APIErrors{}

	if pagination.Limit == "" {
		pagination.Limit = strconv.Itoa(config.DefaultLimit)
	}

	limit, err := strconv.Atoi(pagination.Limit)
	if err != nil {
		errs = append(errs, apierrors.APIError{
			Message:    "pagination limit must be a number",
			Err:        err,
			HTTPStatus: http.StatusBadRequest,
		})
	} else {
		if limit > config.MaxLimit {
			errs = append(errs, apierrors.APIError{
				Message:    fmt.Sprintf("pagination limit too big (%v) maximum is %v", pagination.Limit, config.MaxLimit),
				HTTPStatus: http.StatusBadRequest,
			})
		}
		if limit <= 0 {
			errs = append(errs, apierrors.APIError{
				Message:    "pagination limit must be positive",
				HTTPStatus: http.StatusBadRequest,
			})
		}
	}
	pagination.ValidatedLimit = limit

	offset, err := strconv.Atoi(pagination.Offset)
	if err != nil {
		errs = append(errs, apierrors.APIError{
			Message:    "pagination offset must be a number",
			Err:        err,
			HTTPStatus: http.StatusBadRequest,
		})
	} else {
		if offset < 0 {
			errs = append(errs, apierrors.APIError{
				Message:    "pagination offset must not be negative",
				HTTPStatus: http.StatusBadRequest,
			})
		}
	}
	pagination.ValidatedOffset = offset

	if len(errs) > 0 {
		return errs
	}

	return nil
}

func ParsePagination(values url.Values) *Pagination {
	p := &Pagination{
		Offset: "0",
	}

	limit, ok := values["page[limit]"]
	if ok && len(limit) > 0 {
		p.Limit = limit[0]
	}

	offset, ok := values["page[offset]"]
	if ok && len(offset) > 0 {
		p.Offset = offset[0]
	}

	return p
}

// GetStartEnd returns a slice range that is always valid for a slice of
// totalCount elements: 0 <= start <= end <= totalCount.
//
// end is computed from the CLAMPED start, not from the raw offset. Validation
// bounds the offset only from below, so page[offset]=9223372036854775807 is
// accepted, and raw offset + limit wrapped to a large negative number -- which
// slipped straight past the `end > totalCount` clamp and panicked every caller
// that slices with the result. The router's recovery handler turned that into a
// 500 plus a full goroutine stack in the log, once per request, which made one
// URL a cheap way to fill the control plane's disk.
func (p Pagination) GetStartEnd(totalCount int) (int, int) {
	if totalCount < 0 {
		totalCount = 0
	}

	start := p.ValidatedOffset
	if start < 0 {
		start = 0
	}
	if start > totalCount {
		start = totalCount
	}

	// start is now within [0, totalCount] and the limit is bounded by
	// PaginationConfig.MaxLimit, so this addition cannot overflow. The end <
	// start guard covers a negative limit, which only a caller that ignored a
	// validation error can produce.
	end := start + p.ValidatedLimit
	if end < start || end > totalCount {
		end = totalCount
	}

	return start, end
}
