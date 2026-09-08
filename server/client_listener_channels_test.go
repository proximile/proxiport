package chserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pkg/sftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

type fakeNewChannel struct {
	channelType  string
	extraData    []byte
	accepted     bool
	rejected     bool
	rejectReason ssh.RejectionReason
}

func (c *fakeNewChannel) Accept() (ssh.Channel, <-chan *ssh.Request, error) {
	c.accepted = true
	return nil, nil, assert.AnError
}

func (c *fakeNewChannel) Reject(reason ssh.RejectionReason, _ string) error {
	c.rejected = true
	c.rejectReason = reason
	return nil
}

func (c *fakeNewChannel) ChannelType() string { return c.channelType }
func (c *fakeNewChannel) ExtraData() []byte   { return c.extraData }

// The default arm of this switch used to hand the channel to
// chshare.HandleTCPStream, which dials the address in the channel's ExtraData
// and pipes bytes to it. Any agent could therefore make the control plane open
// a TCP connection anywhere and speak through it -- into other tenants'
// loopback-ACL'd tunnels, and into the API, which the shipped config binds to
// 127.0.0.1 behind a reverse proxy.
func TestHandleSSHChannelsRejectsUnsupportedTypes(t *testing.T) {
	hostile := []*fakeNewChannel{
		{channelType: "direct-tcpip", extraData: []byte("169.254.169.254:80")},
		{channelType: "forwarded-tcpip", extraData: []byte("127.0.0.1:3000")},
		{channelType: "", extraData: []byte("10.0.0.1:22")},
		{channelType: "x11", extraData: []byte("127.0.0.1:6000")},
	}

	chans := make(chan ssh.NewChannel, len(hostile))
	for _, ch := range hostile {
		chans <- ch
	}
	close(chans)

	cl := &ClientListener{logger: testLog, server: &Server{}}
	cl.handleSSHChannels(testLog, "client-1", chans)

	for _, ch := range hostile {
		assert.True(t, ch.rejected, "channel type %q must be rejected", ch.channelType)
		assert.False(t, ch.accepted, "channel type %q must never be accepted", ch.channelType)
		assert.Equal(t, ssh.UnknownChannelType, ch.rejectReason)
	}
}

func stagedFS(t *testing.T) (*stagedUploadFS, string, func()) {
	t.Helper()

	dir := t.TempDir()
	staged := filepath.Join(dir, "abc123_proxiport_filepush")
	require.NoError(t, os.WriteFile(staged, []byte("payload"), 0o600))

	registry := newStagedUploadRegistry()
	release := registry.Allow("client-1", staged)

	return &stagedUploadFS{clientID: "client-1", registry: registry, log: testLog}, staged, release
}

// An agent opening a session channel used to get an sftp server backed by the
// real filesystem with no root, so it could read proxiportd.conf -- key_seed
// and jwt_secret -- and every database under the data directory, as the
// daemon's own user.
func TestStagedUploadFSRefusesAnythingNotStagedForThisAgent(t *testing.T) {
	fs, staged, release := stagedFS(t)
	defer release()

	// The one file it was asked to collect.
	reader, err := fs.Fileread(&sftp.Request{Filepath: staged})
	require.NoError(t, err)
	require.NotNil(t, reader)
	if closer, ok := reader.(interface{ Close() error }); ok {
		require.NoError(t, closer.Close())
	}

	for _, forbidden := range []string{
		"/etc/proxiport/proxiportd.conf",
		"/var/lib/proxiport/clients.db",
		"/proc/self/environ",
		"/etc/passwd",
		filepath.Join(filepath.Dir(staged), "someone-elses_proxiport_filepush"),
		filepath.Dir(staged),
		staged + "/../../etc/shadow",
		"relative/path",
		"",
	} {
		_, err := fs.Fileread(&sftp.Request{Filepath: forbidden})
		assert.ErrorIs(t, err, errNotEntitled, "reading %q must be refused", forbidden)
	}

	// Listing the staging directory would name other agents' files.
	_, err = fs.Filelist(&sftp.Request{Method: "List", Filepath: filepath.Dir(staged)})
	assert.ErrorIs(t, err, errNotEntitled)

	// Stat of the entitled file is allowed, so the client can size it.
	lister, err := fs.Filelist(&sftp.Request{Method: "Stat", Filepath: staged})
	require.NoError(t, err)
	entries := make([]os.FileInfo, 1)
	n, _ := lister.ListAt(entries, 0)
	require.Equal(t, 1, n)
	assert.EqualValues(t, len("payload"), entries[0].Size())

	// Nothing may be written or changed.
	_, err = fs.Filewrite(&sftp.Request{Filepath: staged})
	assert.ErrorIs(t, err, errNotEntitled)
	assert.ErrorIs(t, fs.Filecmd(&sftp.Request{Method: "Remove", Filepath: staged}), errNotEntitled)
}

// The entitlement lasts exactly as long as the upload it belongs to.
func TestStagedUploadEntitlementIsReleased(t *testing.T) {
	fs, staged, release := stagedFS(t)

	_, err := fs.Fileread(&sftp.Request{Filepath: staged})
	require.NoError(t, err)

	release()

	_, err = fs.Fileread(&sftp.Request{Filepath: staged})
	assert.ErrorIs(t, err, errNotEntitled, "the file stops being readable once the upload is done")
}

func TestStagedUploadRegistryIsPerClient(t *testing.T) {
	registry := newStagedUploadRegistry()
	release := registry.Allow("client-1", "/var/lib/proxiport/filepush/a")
	defer release()

	assert.True(t, registry.IsAllowed("client-1", "/var/lib/proxiport/filepush/a"))
	assert.False(t, registry.IsAllowed("client-2", "/var/lib/proxiport/filepush/a"),
		"one agent must not read a file staged for another")
	assert.False(t, registry.IsAllowed("client-1", "/var/lib/proxiport/filepush/b"))
}
