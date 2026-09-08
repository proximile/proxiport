package clienttunnel

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A Tunnel that came back from client storage has no TunnelProtocol: the field
// is an interface tagged `json:"-"`, so unmarshalling leaves it nil. Every
// stored tunnel is in this state after a daemon restart, and the reconnect path
// terminates a client's tunnels before rebuilding them, so terminating one must
// not panic.
func TestTunnelTerminateWithoutLiveProtocol(t *testing.T) {
	var tunnel Tunnel
	require.NoError(t, json.Unmarshal([]byte(`{"id":"1","lhost":"0.0.0.0","lport":"3000","rhost":"127.0.0.1","rport":"22"}`), &tunnel))

	// Precondition: this is what deserialization actually produces.
	require.Nil(t, tunnel.TunnelProtocol)

	assert.NotPanics(t, func() {
		assert.NoError(t, tunnel.Terminate(true))
		assert.NoError(t, tunnel.Terminate(false))
	})
}

// TestTunnelPromotedMethodsAreNilSafe generalises the case above.
//
// Terminate was guarded after it took the fleet down, but the guard was written
// per method, so the next promoted method to be called on a stored tunnel would
// panic the same way. This walks the TunnelProtocol interface by reflection and
// calls every one of its methods through a stored Tunnel, so a method added to
// the interface is covered the day it is added rather than the day it crashes.
func TestTunnelPromotedMethodsAreNilSafe(t *testing.T) {
	var tunnel Tunnel
	require.NoError(t, json.Unmarshal([]byte(`{"id":"1","lhost":"0.0.0.0","lport":"3000","rhost":"127.0.0.1","rport":"22"}`), &tunnel))
	require.Nil(t, tunnel.TunnelProtocol, "precondition: this is what deserialization produces")

	protocol := reflect.TypeOf((*TunnelProtocol)(nil)).Elem()
	stored := reflect.ValueOf(&tunnel)

	require.NotZero(t, protocol.NumMethod())
	for i := 0; i < protocol.NumMethod(); i++ {
		name := protocol.Method(i).Name
		method := stored.MethodByName(name)
		require.True(t, method.IsValid(), "Tunnel should expose %s", name)

		args := make([]reflect.Value, method.Type().NumIn())
		for j := range args {
			args[j] = reflect.New(method.Type().In(j)).Elem()
		}

		assert.NotPanics(t, func() { method.Call(args) },
			"%s panics on a Tunnel restored from storage. Define it on *Tunnel so it "+
				"shadows the promoted TunnelProtocol method and returns something sane "+
				"when there is no live protocol.", name)
	}
}

// TestTunnelLastActiveWithoutLiveProtocol pins the value, not just the absence
// of a panic: the zero time would read as idle since year 1 and make an
// idle-timeout comparison collect a tunnel the moment it is restored.
func TestTunnelLastActiveWithoutLiveProtocol(t *testing.T) {
	created := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	tunnel := Tunnel{CreatedAt: created}

	assert.Equal(t, created, tunnel.LastActive())
}
