package chserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsStagedSeesAnyOwnersEntitlement is the other half of M12. The staging
// filename comes from a caller-chosen upload id, so two operators can name the
// same path. Truncating on write stops the splice, but a second push would
// still destroy a payload the first operator's agents are mid-collection on --
// and hand the second operator's agents whatever is left at that path. The
// registry has to be able to answer "is anyone collecting this?", not only "is
// THIS client collecting it?".
func TestIsStagedSeesAnyOwnersEntitlement(t *testing.T) {
	registry := newStagedUploadRegistry()
	const path = "/var/lib/proxiport/filepush/nginx-conf_proxiport_filepush"

	assert.False(t, registry.IsStaged(path), "nothing is staged yet")

	release := registry.Allow("client-a", path)
	assert.True(t, registry.IsStaged(path))
	assert.True(t, registry.IsAllowed("client-a", path))
	assert.False(t, registry.IsAllowed("client-b", path),
		"the per-client entitlement must stay per-client")

	release()
	assert.False(t, registry.IsStaged(path), "the path is free again once collection finishes")
}

func TestIsStagedCountsConcurrentPushesOfTheSamePath(t *testing.T) {
	registry := newStagedUploadRegistry()
	const path = "/tmp/shared_proxiport_filepush"

	releaseA := registry.Allow("client-a", path)
	releaseB := registry.Allow("client-b", path)
	require.True(t, registry.IsStaged(path))

	releaseA()
	assert.True(t, registry.IsStaged(path), "client-b is still collecting it")

	releaseB()
	assert.False(t, registry.IsStaged(path))
}

func TestIsStagedOnANilRegistryIsSafe(t *testing.T) {
	var registry *stagedUploadRegistry
	assert.False(t, registry.IsStaged("/anything"))
	assert.False(t, registry.IsStaged(""))
}
