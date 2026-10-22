package validation

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errors2 "github.com/proximile/proxiport/server/api/errors"
)

func TestResolveIdleTunnelTimeoutValue(t *testing.T) {
	got, err := ResolveIdleTunnelTimeoutValue("10080", false)
	require.NoError(t, err)
	assert.Equal(t, 7*24*time.Hour, got)

	_, err = ResolveIdleTunnelTimeoutValue("10081", false)
	var apiErr errors2.APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusBadRequest, apiErr.HTTPStatus)
	assert.Equal(t, "idle timeout should be between 0 and 10080 minutes (7 days)", apiErr.Message)
}

func TestResolveTunnelAutoCloseValue(t *testing.T) {
	got, err := ResolveTunnelAutoCloseValue("744h")
	require.NoError(t, err)
	assert.Equal(t, 744*time.Hour, got)

	_, err = ResolveTunnelAutoCloseValue("1000h")
	var apiErr errors2.APIError
	require.True(t, errors.As(err, &apiErr))
	assert.Equal(t, http.StatusBadRequest, apiErr.HTTPStatus)
	assert.Equal(t, "tunnel auto-close value should not be more than 31 days (744 hours)", apiErr.Message)
}
