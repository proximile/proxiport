package schedule

import (
	stderrors "errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/api/errors"
)

func TestValidate(t *testing.T) {
	manager := &Manager{
		cron: newCron(),
	}

	testCases := []struct {
		Name          string
		Schedule      *Schedule
		ExpectedError string
	}{
		{
			Name: "invalid type",
			Schedule: &Schedule{
				Base: Base{
					Type: "invalid",
				},
			},
			ExpectedError: "type must be 'command' or 'script'",
		},
		{
			Name: "invalid schedule",
			Schedule: &Schedule{
				Base: Base{
					Type:     TypeCommand,
					Schedule: "* * *",
				},
			},
			ExpectedError: "expected exactly 5 fields, found 3: [* * *]",
		},
		{
			Name: "empty command",
			Schedule: &Schedule{
				Base: Base{
					Type:     TypeCommand,
					Schedule: "* * * * *",
				},
				Details: Details{
					ClientIDs: []string{"id-1"},
				},
			},
			ExpectedError: "command cannot be empty",
		},
		{
			Name: "negative timeout",
			Schedule: &Schedule{
				Base: Base{
					Type:     TypeCommand,
					Schedule: "* * * * *",
				},
				Details: Details{
					ClientIDs:  []string{"id-1"},
					Command:    "/bin/true",
					TimeoutSec: -5,
				},
			},
			ExpectedError: "timeout_sec must not be negative",
		},
		{
			Name: "ok command",
			Schedule: &Schedule{
				Base: Base{
					Type:     TypeCommand,
					Schedule: "* * * * *",
				},
				Details: Details{
					ClientIDs: []string{"id-1"},
					Command:   "/bin/true",
				},
			},
			ExpectedError: "",
		},
		{
			Name: "empty script",
			Schedule: &Schedule{
				Base: Base{
					Type:     TypeScript,
					Schedule: "* * * * *",
				},
				Details: Details{
					ClientIDs: []string{"id-1"},
				},
			},
			ExpectedError: "script cannot be empty",
		},
		{
			Name: "invalid script",
			Schedule: &Schedule{
				Base: Base{
					Type:     TypeScript,
					Schedule: "* * * * *",
				},
				Details: Details{
					ClientIDs: []string{"id-1"},
					Script:    "invalid",
				},
			},
			ExpectedError: "illegal base64 data at input byte 4",
		},
		{
			Name: "ok script",
			Schedule: &Schedule{
				Base: Base{
					Type:     TypeScript,
					Schedule: "* * * * *",
				},
				Details: Details{
					GroupIDs: []string{"id-1"},
					Script:   "ZWNobyAndGVzdCc=",
				},
			},
			ExpectedError: "",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			err := manager.validate(tc.Schedule)

			if tc.ExpectedError == "" {
				assert.NoError(t, err)
			} else {
				assert.Equal(t, tc.ExpectedError, err.Error())
				// The handler maps the error to its status only when it is
				// an errors.APIError value.
				var apiErr errors.APIError
				require.True(t, stderrors.As(err, &apiErr))
				assert.Equal(t, http.StatusBadRequest, apiErr.HTTPStatus)
			}
		})
	}
}
