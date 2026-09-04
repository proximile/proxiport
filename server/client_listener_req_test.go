package chserver

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/ssh"

	"github.com/proximile/proxiport/share/logger"
)

// A channel request is agent-controlled input. "subsystem" carries a
// length-prefixed string, and dropping the 4-byte prefix without a bounds check
// panicked on any shorter payload — in a goroutine with no recover, so a single
// agent could take the whole daemon down.
func TestHandleReqSubsystemShortPayload(t *testing.T) {
	log := logger.NewLogger("client-listener-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelDebug)
	cl := &ClientListener{}

	for _, payload := range [][]byte{
		nil,
		{},
		{0},
		{0, 0, 0},
	} {
		assert.NotPanics(t, func() {
			cl.handleReq(&ssh.Request{Type: "subsystem", Payload: payload}, log)
		}, "payload %v", payload)
	}
}

func TestHandleReqSubsystemSftpStillAccepted(t *testing.T) {
	log := logger.NewLogger("client-listener-test", logger.LogOutput{File: os.Stdout}, logger.LogLevelDebug)
	cl := &ClientListener{}

	assert.NotPanics(t, func() {
		cl.handleReq(&ssh.Request{
			Type:    "subsystem",
			Payload: append([]byte{0, 0, 0, 4}, []byte("sftp")...),
		}, log)
	})
}
