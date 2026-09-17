package chserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/api"
	"github.com/proximile/proxiport/server/api/jobs/schedule"
	"github.com/proximile/proxiport/server/api/users"
	"github.com/proximile/proxiport/server/clients"
	"github.com/proximile/proxiport/server/clients/clientdata"
)

func scheduleAuthzListener(t *testing.T, user *users.User, permissions map[string]bool, clientList []*clientdata.Client) *APIListener {
	t.Helper()

	return &APIListener{
		Logger:      testLog,
		userService: &stubUserLookup{user: user, permissions: permissions},
		Server: &Server{
			clientService:       clients.NewClientService(nil, nil, clients.NewClientRepository(clientList, &hour, testLog), testLog, nil),
			clientGroupProvider: mockClientGroupProvider{},
		},
	}
}

// The create-path tests all run as alice; the lockout tests below call
// checkScheduleAccess directly and need no request at all.
func postScheduleRequest(t *testing.T, body string) *http.Request {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/schedules", strings.NewReader(body))
	return req.WithContext(api.WithUser(context.Background(), "alice"))
}

// The /schedules subrouter is gated by the "scheduler" permission alone, so
// "scheduler" was a strict superset of "commands" and "scripts": a user
// deliberately denied both could still run either, on every client they could
// address, by wrapping it in a cron expression. The same command sent straight
// to POST /commands would have been refused.
func TestCreatingACommandScheduleRequiresTheCommandsPermission(t *testing.T) {
	alice := &users.User{Username: "alice", Groups: []string{"backup-ops"}}
	c1 := clients.New(t).ID("client-1").Logger(testLog).Build()
	c1.SetAllowedUserGroups([]string{"backup-ops"})

	al := scheduleAuthzListener(t, alice,
		// The group the operator meant to hand out: schedules, nothing else.
		map[string]bool{users.PermissionScheduler: true},
		[]*clientdata.Client{c1})

	body := `{"name":"x","type":"command","schedule":"@every 1s","client_ids":["client-1"],"command":"id","is_sudo":true}`
	_, _, _, err := al.prepareHandleSchedules(postScheduleRequest(t, body))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `"commands" permission`)
}

func TestCreatingAScriptScheduleRequiresTheScriptsPermission(t *testing.T) {
	alice := &users.User{Username: "alice", Groups: []string{"backup-ops"}}
	c1 := clients.New(t).ID("client-1").Logger(testLog).Build()
	c1.SetAllowedUserGroups([]string{"backup-ops"})

	al := scheduleAuthzListener(t, alice,
		// Commands granted; a script schedule still needs the scripts grant.
		map[string]bool{users.PermissionScheduler: true, users.PermissionCommands: true},
		[]*clientdata.Client{c1})

	body := `{"name":"x","type":"script","schedule":"@every 1s","client_ids":["client-1"],"script":"aWQK"}`
	_, _, _, err := al.prepareHandleSchedules(postScheduleRequest(t, body))

	require.Error(t, err)
	assert.Contains(t, err.Error(), `"scripts" permission`)
}

func TestCreatingACommandScheduleIsAllowedWithBothPermissions(t *testing.T) {
	alice := &users.User{Username: "alice", Groups: []string{"backup-ops"}}
	c1 := clients.New(t).ID("client-1").Logger(testLog).Build()
	c1.SetAllowedUserGroups([]string{"backup-ops"})

	al := scheduleAuthzListener(t, alice,
		map[string]bool{users.PermissionScheduler: true, users.PermissionCommands: true},
		[]*clientdata.Client{c1})

	body := `{"name":"x","type":"command","schedule":"@every 1s","client_ids":["client-1"],"command":"id"}`
	scheduleInput, username, orderedClients, err := al.prepareHandleSchedules(postScheduleRequest(t, body))

	require.NoError(t, err)
	assert.Equal(t, "alice", username)
	assert.Equal(t, schedule.TypeCommand, scheduleInput.Type)
	require.Len(t, orderedClients, 1)
}

// An auth backend with no group permissions to check must be unaffected.
func TestCreatingAScheduleIsUnaffectedWhenTheBackendHasNoPermissions(t *testing.T) {
	alice := &users.User{Username: "alice", Groups: []string{"backup-ops"}}
	c1 := clients.New(t).ID("client-1").Logger(testLog).Build()
	c1.SetAllowedUserGroups([]string{"backup-ops"})

	al := scheduleAuthzListener(t, alice, nil, []*clientdata.Client{c1})

	body := `{"name":"x","type":"command","schedule":"@every 1s","client_ids":["client-1"],"command":"id"}`
	_, _, _, err := al.prepareHandleSchedules(postScheduleRequest(t, body))

	require.NoError(t, err)
}

// checkScheduleAccess re-resolved a stored schedule's targets with the strict
// resolution meant for accepting a request, and returned the resolution error
// as the authorization result. So deleting a client a schedule targeted made
// that schedule vanish from its owner's list and answer 404 to GET, PUT and
// DELETE alike -- while the cron entry, which only DELETE removes, went on
// firing and failing forever. Only an admin could clear it.
func TestAScheduleStaysAccessibleToItsOwnerAfterATargetIsDeleted(t *testing.T) {
	bob := &users.User{Username: "bob", Groups: []string{"ops"}}
	alive := clients.New(t).ID("client-alive").Logger(testLog).Build()
	alive.SetAllowedUserGroups([]string{"ops"})

	// The repository holds only the surviving client: "client-gone" was deleted.
	al := scheduleAuthzListener(t, bob, map[string]bool{users.PermissionScheduler: true},
		[]*clientdata.Client{alive})

	stored := &schedule.Schedule{
		Base: schedule.Base{ID: "s1", CreatedBy: "bob", Type: schedule.TypeCommand},
		Details: schedule.Details{
			ClientIDs: []string{"client-gone", "client-alive"},
			Command:   "id",
		},
	}

	require.NoError(t, al.checkScheduleAccess(context.Background(), stored, bob, nil))
}

// The same, once every target is gone: the owner must still be able to reach
// the schedule to delete it.
func TestAScheduleStaysAccessibleToItsOwnerAfterEveryTargetIsDeleted(t *testing.T) {
	bob := &users.User{Username: "bob", Groups: []string{"ops"}}

	al := scheduleAuthzListener(t, bob, map[string]bool{users.PermissionScheduler: true}, nil)

	stored := &schedule.Schedule{
		Base:    schedule.Base{ID: "s1", CreatedBy: "bob", Type: schedule.TypeCommand},
		Details: schedule.Details{ClientIDs: []string{"client-gone"}, Command: "id"},
	}

	require.NoError(t, al.checkScheduleAccess(context.Background(), stored, bob, nil))
}

// Leniency about targets that no longer exist must not leak into the targets
// that do: a client the user has no access to still denies access.
func TestAScheduleIsStillDeniedForATargetTheOwnerCannotReach(t *testing.T) {
	bob := &users.User{Username: "bob", Groups: []string{"ops"}}
	other := clients.New(t).ID("client-other").Logger(testLog).Build()
	other.SetAllowedUserGroups([]string{"someone-else"})

	al := scheduleAuthzListener(t, bob, map[string]bool{users.PermissionScheduler: true},
		[]*clientdata.Client{other})

	stored := &schedule.Schedule{
		Base:    schedule.Base{ID: "s1", CreatedBy: "bob", Type: schedule.TypeCommand},
		Details: schedule.Details{ClientIDs: []string{"client-gone", "client-other"}, Command: "id"},
	}

	require.Error(t, al.checkScheduleAccess(context.Background(), stored, bob, nil))
}

// And a schedule somebody else created is still not this user's business.
func TestAScheduleCreatedByAnotherUserIsStillDenied(t *testing.T) {
	bob := &users.User{Username: "bob", Groups: []string{"ops"}}

	al := scheduleAuthzListener(t, bob, map[string]bool{users.PermissionScheduler: true}, nil)

	stored := &schedule.Schedule{
		Base:    schedule.Base{ID: "s1", CreatedBy: "alice", Type: schedule.TypeCommand},
		Details: schedule.Details{ClientIDs: []string{"client-gone"}, Command: "id"},
	}

	err := al.checkScheduleAccess(context.Background(), stored, bob, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed to access this schedule")
}
