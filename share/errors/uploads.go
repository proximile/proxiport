package errors

import "errors"

var ErrUploadsDisabled = errors.New("uploads are disabled on this client, check [file-reception] enabled option")

// ErrClientNotConnected is returned when a file push names a client that is not
// currently connected. The target list is built with GetByID, which filters
// obsolete clients but not disconnected ones, and every client is disconnected
// immediately after a server restart -- so this is a routine outcome, not an
// edge case.
var ErrClientNotConnected = errors.New("client is not connected")
