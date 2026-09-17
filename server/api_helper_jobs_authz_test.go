package chserver

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	errors2 "github.com/proximile/proxiport/server/api/errors"
	"github.com/proximile/proxiport/server/api/jobs"
	"github.com/proximile/proxiport/server/api/users"
	"github.com/proximile/proxiport/server/cgroups"
	"github.com/proximile/proxiport/server/clients"
	"github.com/proximile/proxiport/server/clients/clientdata"
)

type stubUserLookup struct {
	UserService
	user *users.User
	// permissions, when non-nil, makes the stub behave like an auth backend
	// that has group permissions to check, granting exactly these. Left nil it
	// behaves like the file and single-static-user backends, which have none.
	permissions map[string]bool
}

func (s *stubUserLookup) GetByUsername(username string) (*users.User, error) {
	if s.user != nil && s.user.Username == username {
		return s.user, nil
	}
	return nil, nil
}

func (s *stubUserLookup) SupportsGroupPermissions() bool {
	return s.permissions != nil
}

func (s *stubUserLookup) CheckPermission(_ *users.User, permission string) error {
	if s.permissions[permission] {
		return nil
	}
	return errors2.APIError{
		Message:    fmt.Sprintf("user does not have %q permission", permission),
		HTTPStatus: http.StatusForbidden,
	}
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

// The dispatch-time re-check resolved the creator's *client* access and stopped
// there, so revoking "commands" -- the documented, non-destructive way to
// withdraw command execution from an account you are investigating -- left
// every schedule that account had already stored running on its cron. Worse,
// revoking "scheduler" alongside it meant the owner could no longer see or
// delete the schedule either, so only an admin who thought to look could stop
// it.
func TestCheckMultiJobAccessRefusesACreatorWhoseCommandsPermissionWasRevoked(t *testing.T) {
	creator := &users.User{Username: "alice", Groups: []string{"ops"}}
	clientService := &recordingAccessClientService{SimpleMockClientService: &SimpleMockClientService{}}

	al := APIListener{
		Logger: testLog,
		userService: &stubUserLookup{
			user: creator,
			// The group kept its client access; only the execution grants went.
			permissions: map[string]bool{},
		},
		Server: &Server{
			clientService:       clientService,
			clientGroupProvider: mockClientGroupProvider{},
		},
	}

	err := al.checkMultiJobAccess(context.Background(), &jobs.MultiJobRequest{
		Username:       "alice",
		OrderedClients: []*clientdata.Client{{ID: "client-1"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `"commands" permission`)
	assert.False(t, clientService.called, "the job must be refused before its targets are even considered")
}

func TestCheckMultiJobAccessRefusesACreatorWhoseScriptsPermissionWasRevoked(t *testing.T) {
	creator := &users.User{Username: "alice", Groups: []string{"ops"}}

	al := APIListener{
		Logger: testLog,
		userService: &stubUserLookup{
			user: creator,
			// Commands are still granted -- a script needs its own permission.
			permissions: map[string]bool{users.PermissionCommands: true},
		},
		Server: &Server{
			clientService:       &recordingAccessClientService{SimpleMockClientService: &SimpleMockClientService{}},
			clientGroupProvider: mockClientGroupProvider{},
		},
	}

	err := al.checkMultiJobAccess(context.Background(), &jobs.MultiJobRequest{
		Username:       "alice",
		IsScript:       true,
		OrderedClients: []*clientdata.Client{{ID: "client-1"}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `"scripts" permission`)
}

func TestCheckMultiJobAccessAllowsACreatorWhoStillHoldsThePermission(t *testing.T) {
	creator := &users.User{Username: "alice", Groups: []string{"ops"}}
	clientService := &recordingAccessClientService{SimpleMockClientService: &SimpleMockClientService{}}

	al := APIListener{
		Logger: testLog,
		userService: &stubUserLookup{
			user:        creator,
			permissions: map[string]bool{users.PermissionCommands: true},
		},
		Server: &Server{
			clientService:       clientService,
			clientGroupProvider: mockClientGroupProvider{},
		},
	}

	require.NoError(t, al.checkMultiJobAccess(context.Background(), &jobs.MultiJobRequest{
		Username:       "alice",
		OrderedClients: []*clientdata.Client{{ID: "client-1"}},
	}))
	assert.True(t, clientService.called)
}

// An auth backend with no group permissions at all must keep behaving exactly
// as it did: there is nothing to check, and failing closed would lock every
// file-auth and single-user deployment out of its own schedules.
func TestCheckMultiJobAccessSkipsThePermissionCheckWhenTheBackendHasNone(t *testing.T) {
	creator := &users.User{Username: "alice"}
	clientService := &recordingAccessClientService{SimpleMockClientService: &SimpleMockClientService{}}

	al := APIListener{
		Logger:      testLog,
		userService: &stubUserLookup{user: creator}, // permissions nil
		Server: &Server{
			clientService:       clientService,
			clientGroupProvider: mockClientGroupProvider{},
		},
	}

	require.NoError(t, al.checkMultiJobAccess(context.Background(), &jobs.MultiJobRequest{
		Username:       "alice",
		OrderedClients: []*clientdata.Client{{ID: "client-1"}},
	}))
	assert.True(t, clientService.called)
}
