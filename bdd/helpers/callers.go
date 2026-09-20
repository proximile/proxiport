package helpers

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/stretchr/testify/suite"
)

// The [api] auth pair in every bdd/**/proxiportd.conf. These are not the
// published example-config placeholders: proxiportd refuses to start on those
// (see validateNoPlaceholderSecrets), which is what silently killed this whole
// suite once. Change these and the configs together.
const (
	apiUser = "bdd-admin"
	apiPass = "bdd-admin-secret"
)

func CheckOperationHTTPStatus(suite *suite.Suite, requestURL string, method string, content []byte, expectedStatus int) {

	client := &http.Client{
		Timeout: time.Second * 10,
	}

	req, err := http.NewRequest(method, requestURL, bytes.NewReader(content))
	suite.NoError(err)
	req.SetBasicAuth(apiUser, apiPass)
	res, err := client.Do(req)
	suite.NoError(err)

	suite.Equal(expectedStatus, res.StatusCode)

	rawBody, err := io.ReadAll(res.Body)
	suite.NoError(err)

	body := string(rawBody)
	suite.T().Log(body)
}

func CallURL[T any](suite *suite.Suite, requestURL string) T {

	client := &http.Client{
		Timeout: time.Second * 10,
	}

	req, err := http.NewRequest(http.MethodGet, requestURL, nil)
	suite.NoError(err)
	req.SetBasicAuth(apiUser, apiPass)
	res, err := client.Do(req)
	suite.NoError(err)
	suite.Equal(http.StatusOK, res.StatusCode)

	rawBody, err := io.ReadAll(res.Body)
	suite.NoError(err)

	body := string(rawBody)
	suite.T().Log(body)

	var structured T
	suite.NoError(json.Unmarshal([]byte(body), &structured))
	return structured
}
