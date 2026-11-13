package clients

import (
	"context"
	"net"
	"testing"
	"time"

	mapset "github.com/deckarep/golang-set"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/server/clients/clientdata"
	"github.com/proximile/proxiport/server/clients/clienttunnel"
	"github.com/proximile/proxiport/server/ports"
	chshare "github.com/proximile/proxiport/share"
	"github.com/proximile/proxiport/share/test"
)

func bindingTestService(initial ...*clientdata.Client) *ClientServiceProvider {
	return NewClientService(
		&clienttunnel.InternalTunnelProxyConfig{},
		ports.NewPortDistributor(mapset.NewSet()),
		NewClientRepository(initial, &hour, testLog),
		testLog,
		nil,
	)
}

func connectAs(cs *ClientServiceProvider, credential, clientID, name string, multiuse bool) (*clientdata.Client, error) {
	conn := test.NewConnMock()
	conn.ReturnRemoteAddr = &net.TCPAddr{IP: net.IPv4(192, 0, 2, 7), Port: 2345}
	conn.ReturnOk = true
	conn.ReturnResponsePayload = []byte(`{"IsAllowed":true}`)
	return cs.StartClient(context.Background(), credential, clientID, conn, multiuse,
		&chshare.ConnectionRequest{Name: name, Version: "0.7.0"}, testLog)
}

// A client id belongs to the credential that registered it, whether or not
// credentials may be shared between agents. Otherwise the holder of any valid
// agent credential could connect under the id of a client that is offline
// and take over what the server keeps for it: the user groups allowed to
// reach it, its tunnels, the commands and scripts sent to it.
func TestClientIDStaysWithItsCredentialWhenCredentialsMayBeShared(t *testing.T) {
	for _, multiuse := range []bool{true, false} {
		offline := time.Now().Add(-time.Minute)
		cs := bindingTestService(&clientdata.Client{
			ID:                "host01",
			ClientAuthID:      "credential-a",
			Name:              "host01",
			AllowedUserGroups: []string{"ops"},
			DisconnectedAt:    &offline,
		})

		_, err := connectAs(cs, "credential-b", "host01", "someone-else", multiuse)
		require.Error(t, err, "multiuse=%v: another credential connected as host01", multiuse)
		assert.Contains(t, err.Error(), "registered to a different credential")

		kept, err := cs.GetByID("host01")
		require.NoError(t, err)
		assert.Equal(t, "credential-a", kept.GetClientAuthID(), "multiuse=%v", multiuse)
		assert.Equal(t, "host01", kept.GetName(), "multiuse=%v: the refused connection changed the record", multiuse)
		assert.Equal(t, []string{"ops"}, kept.GetAllowedUserGroups())

		// The credential that registered the id still connects.
		back, err := connectAs(cs, "credential-a", "host01", "host01", multiuse)
		require.NoError(t, err, "multiuse=%v", multiuse)
		assert.Equal(t, []string{"ops"}, back.GetAllowedUserGroups())
	}
}

// Sharing one credential between agents keeps working: each connects under
// its own id.
func TestAgentsSharingACredentialConnectUnderTheirOwnIDs(t *testing.T) {
	cs := bindingTestService()
	_, err := connectAs(cs, "shared-credential", "host01", "host01", true)
	require.NoError(t, err)
	_, err = connectAs(cs, "shared-credential", "host02", "host02", true)
	require.NoError(t, err)
}

// A record from before client ids were bound has no credential stored. The
// first agent to connect under that id binds it.
func TestAClientWithoutAStoredCredentialIsBoundOnItsNextConnection(t *testing.T) {
	offline := time.Now().Add(-time.Minute)
	cs := bindingTestService(&clientdata.Client{ID: "host01", DisconnectedAt: &offline})

	c, err := connectAs(cs, "credential-a", "host01", "host01", true)
	require.NoError(t, err)
	require.Equal(t, "credential-a", c.GetClientAuthID())
	c.SetDisconnectedNow()

	_, err = connectAs(cs, "credential-b", "host01", "someone-else", true)
	assert.Error(t, err)
}

// To let a host register under its id with a new credential, an
// administrator deletes the client. The new record starts empty: nothing
// that was granted to the old one carries over.
func TestDeletingAClientReleasesItsID(t *testing.T) {
	offline := time.Now().Add(-time.Minute)
	cs := bindingTestService(&clientdata.Client{
		ID:                "host01",
		ClientAuthID:      "credential-a",
		AllowedUserGroups: []string{"ops"},
		DisconnectedAt:    &offline,
	})

	require.NoError(t, cs.DeleteOffline("host01"))

	c, err := connectAs(cs, "credential-b", "host01", "host01", true)
	require.NoError(t, err)
	assert.Equal(t, "credential-b", c.GetClientAuthID())
	assert.Empty(t, c.GetAllowedUserGroups())
	assert.Empty(t, c.GetTunnels())
}

func TestCredentialsSharedByClients(t *testing.T) {
	shared := credentialsSharedByClients([]*clientdata.Client{
		{ID: "host01", ClientAuthID: "shared"},
		{ID: "host02", ClientAuthID: "shared"},
		{ID: "host03", ClientAuthID: "shared"},
		{ID: "host04", ClientAuthID: "own"},
		{ID: "host05"},
		{ID: "host06"},
	})
	assert.Equal(t, map[string]int{"shared": 3}, shared)
}
