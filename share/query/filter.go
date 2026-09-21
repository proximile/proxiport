package query

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"

	errors2 "github.com/proximile/proxiport/server/api/errors"
)

var filterRegex = regexp.MustCompile(`^filter\[([\w|*]+)](\[(\w+)])?`)
var valuesLogicalOpsblock = regexp.MustCompile(`^(and|or){1}\((.+)\)`)

type FilterOperatorType string
type FilterLogicalOperator string

const (
	FilterOperatorTypeEQ    FilterOperatorType = "eq"
	FilterOperatorTypeGT    FilterOperatorType = "gt"
	FilterOperatorTypeLT    FilterOperatorType = "lt"
	FilterOperatorTypeSince FilterOperatorType = "since"
	FilterOperatorTypeUntil FilterOperatorType = "until"
)
const (
	FilterLogicalOperatorTypeOR  FilterLogicalOperator = "or"
	FilterLogicalOperatorTypeAND FilterLogicalOperator = "and"
)

func (fot FilterOperatorType) Code() string {
	code, ok := map[FilterOperatorType]string{
		"eq":    "=",
		"gt":    ">",
		"lt":    "<",
		"since": ">=",
		"until": "<=",
	}[fot]
	if !ok {
		return "="
	}
	return code
}

type FilterOption struct {
	Column                []string
	Operator              FilterOperatorType
	Values                []string // Values are [ValuesLogicalOperator]ed together (only AND, OR, default OR)
	ValuesLogicalOperator FilterLogicalOperator

	// CompareFunc names a SQL function applied to BOTH sides of the comparison
	// before they are compared, so a column whose stored text is not
	// byte-comparable with the caller's value is still compared as a value.
	// The audit log uses DATETIME for exactly that reason: its rows carry
	// whatever UTC offset the server had when each one was written, and a byte
	// compare against an ISO-8601 value is meaningless.
	//
	// It is interpolated into the statement, so it must never be derived from
	// request input. Nothing parses it out of a URL; every assignment in this
	// tree is a compile-time constant.
	CompareFunc string
}

func (fo FilterOption) String() string {
	s := fmt.Sprintf("filter[%s]", strings.Join(fo.Column, "|"))
	if fo.Operator != "" {
		s += fmt.Sprintf("[%s]", fo.Operator)
	}
	return s
}

func (fo FilterOption) isSupported(supportedFields map[string]bool) bool {
	for _, col := range fo.Column {
		if fo.Operator == "" && supportedFields[col] {
			continue
		}
		if supportedFields[fmt.Sprintf("%s[%s]", col, fo.Operator)] {
			continue
		}
		return false
	}
	return true
}

// setWildcardColumns expands filter[*] into the supported columns it can
// legitimately stand for.
//
// The supported-field maps are keyed by the *filter* name, and a range filter
// carries its operator in that name: "timestamp[gt]". Copying every key
// verbatim put "timestamp[gt]" into the SQL as a column name, which SQLite
// reads as the identifier timestamp followed by the bracket-quoted identifier
// [gt] -- a syntax error at prepare time. So filter[*] answered HTTP 500 on
// every endpoint family whose map declares a range filter, and worked on every
// one whose map does not, which is why the API behaved inconsistently rather
// than visibly wrongly.
//
// Operator-suffixed keys are dropped rather than trimmed. "timestamp[gt]"
// declares that the field is filterable with a *comparison*; an equality
// wildcard against a timestamp is not a search anyone means, and trimming would
// also make the expanded column fail isSupported, which looks up the key as the
// map holds it. What is left is exactly the set of plain, equality-filterable
// fields -- which is what "filter on any field" means.
func (fo *FilterOption) setWildcardColumns(supportedFields map[string]bool) {
	columns := make([]string, 0, len(supportedFields))
	for field := range supportedFields {
		if strings.Contains(field, "[") {
			continue
		}
		columns = append(columns, field)
	}

	// Map iteration order is random and the expansion decides both the SQL text
	// and the order of its bound parameters, so sort it.
	sort.Strings(columns)
	fo.Column = columns
}

func ValidateFilterOptions(fo []FilterOption, supportedFields map[string]bool) errors2.APIErrors {
	errs := errors2.APIErrors{}
	for i := range fo {
		if len(fo[i].Column) == 1 && fo[i].Column[0] == "*" {
			fo[i].setWildcardColumns(supportedFields)
			if len(fo[i].Column) == 0 {
				// Nothing on this endpoint is filterable by equality -- the
				// monitoring families declare only timestamp ranges. Say so,
				// rather than handing an empty column list to the SQL builder.
				errs = append(errs, errors2.APIError{
					Message:    "unsupported filter field 'filter[*]'",
					HTTPStatus: http.StatusBadRequest,
				})
				continue
			}
		}
		ok := fo[i].isSupported(supportedFields)
		if !ok {
			errs = append(errs, errors2.APIError{
				Message:    fmt.Sprintf("unsupported filter field '%s'", fo[i]),
				HTTPStatus: http.StatusBadRequest,
			})
		}
	}

	if len(errs) > 0 {
		return errs
	}

	return nil
}

func ParseFilterOptions(values url.Values) []FilterOption {
	res := make([]FilterOption, 0)
	for filterKey, filterValues := range values {
		if !strings.HasPrefix(filterKey, "filter") || len(filterValues) == 0 {
			continue
		}

		operands, logicalOperator := getFilterValues(filterValues)

		if len(operands) == 0 {
			continue
		}

		matches := filterRegex.FindStringSubmatch(filterKey)
		if len(matches) < 4 {
			continue
		}

		filterColumn := matches[1]
		filterColumn = strings.TrimSpace(filterColumn)
		if filterColumn == "" {
			continue
		}
		filterColumns := strings.Split(filterColumn, "|")

		filterOperator := matches[3]
		filterOperator = strings.TrimSpace(filterOperator)

		fo := FilterOption{
			Column:                filterColumns,
			Operator:              FilterOperatorType(filterOperator),
			ValuesLogicalOperator: logicalOperator,
			Values:                operands,
		}

		res = append(res, fo)
	}

	return res
}

func SortFiltersByOperator(a []FilterOption) {
	sort.Slice(a, func(i, j int) bool {
		return a[i].Operator < a[j].Operator
	})
}

func SplitFilters(options []FilterOption, keys map[string]bool) ([]FilterOption, []FilterOption) {
	var these, other []FilterOption
	for _, o := range options {
		isThese := false
		for _, c := range o.Column {
			if _, ok := keys[c]; ok {
				isThese = true
			}
		}
		if isThese {
			these = append(these, o)
		} else {
			other = append(other, o)
		}
	}
	return these, other
}
