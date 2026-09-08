package chserver

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/api/jobs"
	"github.com/proximile/proxiport/server/api/users"
	"github.com/proximile/proxiport/server/cgroups"
	"github.com/proximile/proxiport/server/clients"
	"github.com/proximile/proxiport/server/clients/clientdata"
)

type stubUserLookup struct {
	UserService
	user *users.User
}

func (s *stubUserLookup) GetByUsername(username string) (*users.User, error) {
	if s.user != nil && s.user.Username == username {
		return s.user, nil
	}
	return nil, nil
}

type recordingAccessClientService struct {
	*SimpleMockClientService
	called         bool
	checkedGroups  []string
	checkedClients []*clientdata.Client
	returnErr      error
}

func (r *recordingAccessClientService) CheckClientsAccess(
	checked []*clientdata.Client, user clients.User, groups []*cgroups.ClientGroup,
) error {
	r.called = true
	r.checkedGroups = user.GetGroups()
	r.checkedClients = checked
	return r.returnErr
}

// A schedule runs as its creator, long after the creator was authorized. The
// check used to live only in the HTTP handlers, so the scheduler -- which calls
// StartMultiClientJob directly -- ran with no check at all: a schedule outlived
// its creator's account, and because group and tag targets are re-resolved on
// every run it could reach clients the creator never had access to.
func TestCheckMultiJobAccessRefusesADeletedCreator(t *testing.T) {
	al := APIListener{
		Logger:      testLog,
		userService: &stubUserLookup{user: nil}, // the creator is gone
		Server:      &Server{clientGroupProvider: mockClientGroupProvider{}},
	}

	err := al.checkMultiJobAccess(context.Background(), &jobs.MultiJobRequest{
		Username:       "deleted-operator",
		OrderedClients: []*clientdata.Client{{ID: "client-1"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no longer exists")
}

func TestCheckMultiJobAccessRefusesAJobWithNoUser(t *testing.T) {
	al := APIListener{
		Logger:      testLog,
		userService: &stubUserLookup{},
		Server:      &Server{clientGroupProvider: mockClientGroupProvider{}},
	}

	err := al.checkMultiJobAccess(context.Background(), &jobs.MultiJobRequest{
		OrderedClients: []*clientdata.Client{{ID: "client-1"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "names no user")
}

// The freshly-resolved target set is what gets checked, as the job's own user.
func TestCheckMultiJobAccessChecksTheResolvedTargets(t *testing.T) {
	creator := &users.User{Username: "operator", Groups: []string{"operators"}}
	clientService := &recordingAccessClientService{SimpleMockClientService: &SimpleMockClientService{}}

	al := APIListener{
		Logger:      testLog,
		userService: &stubUserLookup{user: creator},
		Server: &Server{
			clientService:       clientService,
			clientGroupProvider: mockClientGroupProvider{},
		},
	}

	targets := []*clientdata.Client{{ID: "client-1"}, {ID: "client-2"}}
	require.NoError(t, al.checkMultiJobAccess(context.Background(), &jobs.MultiJobRequest{
		Username:       "operator",
		OrderedClients: targets,
	}))

	assert.True(t, clientService.called, "the resolved target set must be authorized")
	assert.Equal(t, targets, clientService.checkedClients)
	assert.Equal(t, []string{"operators"}, clientService.checkedGroups, "checked as the job's own user")
}
