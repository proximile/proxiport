package client_labels_test

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/proximile/proxiport/bdd/helpers"
)

type TagsAndLabels struct {
	Tags   []string          `json:"tags"`
	Labels map[string]string `json:"labels"`
}
type ID struct {
	ID string `json:"id"`
}

type Rsp struct {
	Data []TagsAndLabels `json:"data"`
}

type RspID struct {
	Data []ID `json:"data"`
}

const apiHost = "http://localhost:4011"

type UpdateAttributesTestSuite struct {
	suite.Suite
	serverProcess *exec.Cmd
	clientProcess *exec.Cmd
	ctx           context.Context
	clientID      string
}

// attributesFixture is the committed content of client_attributes.json. The
// agent rewrites that file when the test updates attributes through the API,
// and the file is tracked -- so without restoring it the suite left a modified
// tracked file behind, dirtying the working tree and risking the test's own
// output being committed as the fixture.
const attributesFixture = `{"tags":["vm"],"labels":{}}`

func (suite *UpdateAttributesTestSuite) writeAttributesFixture() {
	suite.NoError(os.WriteFile("./client_attributes.json", []byte(attributesFixture), 0600))
}

func (suite *UpdateAttributesTestSuite) SetupTest() {
	helpers.CleanUp(suite.T(), "./rc-test-resources", "./rd-test-resources")
	suite.writeAttributesFixture()
	suite.ctx = context.Background()
	ctx, cancel := context.WithTimeout(suite.ctx, time.Minute*5)
	defer cancel()
	suite.serverProcess, suite.clientProcess = helpers.StartClientAndServerAndWaitForConnection(ctx, suite.T(), helpers.FindProjectRoot(suite.T()))

	suite.clientID = helpers.CallURL[RspID](&suite.Suite, apiHost+"/api/v1/clients?fields[clients]=id").Data[0].ID
}

func (suite *UpdateAttributesTestSuite) TearDownTest() {
	helpers.LogAndIgnore(suite.clientProcess.Process.Kill())
	helpers.LogAndIgnore(suite.serverProcess.Process.Kill())
	// Put the tracked fixture back the way it is committed.
	suite.writeAttributesFixture()
	log.Println("done")
}

type Attributes struct {
	Tags   []string          `json:"tags"`
	Labels map[string]string `json:"labels"`
}

func (suite *UpdateAttributesTestSuite) TestClientAttributesIsUpdated() {

	requestURL := fmt.Sprintf(apiHost+"/api/v1/clients/%v/attributes", suite.clientID)

	data, err := json.Marshal(Attributes{Tags: []string{"test"}, Labels: map[string]string{"test": "test"}})
	suite.NoError(err)

	helpers.CheckOperationHTTPStatus(&suite.Suite, requestURL, http.MethodPut, data, http.StatusOK)

	requestURL = apiHost + "/api/v1/clients?fields[clients]=tags,labels&filter[labels]=test:%20test"

	expected := []TagsAndLabels{{Tags: []string{"test"}, Labels: map[string]string{"test": "test"}}}

	suite.ExpectAnswer(requestURL, expected)

	suite.TearDownTest()
}

func (suite *UpdateAttributesTestSuite) ExpectAnswer(requestURL string, expected []TagsAndLabels) bool {
	structured := helpers.CallURL[Rsp](&suite.Suite, requestURL)
	return suite.Equal(Rsp{Data: expected}, structured)
}

// In order for 'go test' to run this suite, we need to create
// a normal test function and pass our suite to suite.Run
func TestUpdateAttributesTestSuite(t *testing.T) {
	suite.Run(t, new(UpdateAttributesTestSuite))
}
