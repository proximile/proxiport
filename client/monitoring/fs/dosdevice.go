package fs

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
)

// The NT target path prefixes that identify a mapped network drive.
const (
	smbTargetMarker = `LanmanRedirector\`
	nfsTargetMarker = `MRxNfs\`
)

// Buffer sizes are counted in UTF-16 code units, which is the unit
// QueryDosDeviceW's ucchMax argument is defined in -- see dosDeviceQuery.
//
// 512 covers every ordinary target ("\Device\HarddiskVolume3" is 23) with room
// for the DFS, WebDAV and deep-SMB targets that motivated this code; the cap
// stops a pathological device list from being retried forever.
const (
	dosDeviceInitialBufLen = 512
	dosDeviceMaxBufLen     = 32768
)

// errBufferTooSmall is what a dosDeviceQuery reports when the kernel said the
// buffer it was handed could not hold the answer (ERROR_INSUFFICIENT_BUFFER).
// It is declared here rather than in the windows-only file so that the growth
// loop, and its tests, build on every platform.
var errBufferTooSmall = errors.New("QueryDosDevice: buffer too small")

// dosDeviceQuery performs one QueryDosDeviceW call into buf and returns the
// number of UTF-16 code units the kernel stored there.
//
// The buffer is passed as a []uint16 and never as a []byte. QueryDosDeviceW's
// third argument, ucchMax, is "the maximum number of TCHARs that can be stored
// into the buffer" -- a count of UTF-16 code units, not of bytes. Handing the
// kernel a 256-byte allocation and telling it 256 authorizes it to write 512
// bytes, and it will do so for any NT target path longer than 127 characters
// without ever returning ERROR_INSUFFICIENT_BUFFER. Sizing the buffer in the
// same unit the API counts in makes that mismatch unrepresentable: the length
// passed to the kernel is len() of the slice the kernel is given.
type dosDeviceQuery func(buf []uint16) (n uint32, err error)

// queryDosDeviceNames runs query against a buffer it owns, growing the buffer
// while the kernel reports it is too small, and returns the device names the
// kernel wrote.
func queryDosDeviceNames(query dosDeviceQuery) ([]string, error) {
	for bufLen := dosDeviceInitialBufLen; ; bufLen *= 2 {
		buf := make([]uint16, bufLen)

		n, err := query(buf)
		if errors.Is(err, errBufferTooSmall) {
			if bufLen*2 > dosDeviceMaxBufLen {
				return nil, fmt.Errorf("QueryDosDevice needs more than %d UTF-16 code units", dosDeviceMaxBufLen)
			}
			continue
		}
		if err != nil {
			return nil, err
		}

		// The kernel is not supposed to be able to report storing more than it
		// was given room for. If it ever does, the buffer has already been
		// overrun, so refuse the result rather than reading past the write.
		if int(n) > len(buf) {
			return nil, fmt.Errorf("QueryDosDevice reported %d UTF-16 code units in a %d-unit buffer", n, len(buf))
		}

		return splitDosDeviceNames(buf[:n]), nil
	}
}

// splitDosDeviceNames decodes the NUL-separated, NUL-terminated UTF-16 list
// QueryDosDeviceW writes into its output buffer. Empty entries -- including the
// final terminator -- are dropped.
func splitDosDeviceNames(buf []uint16) []string {
	var (
		names []string
		start int
	)
	for i, u := range buf {
		if u != 0 {
			continue
		}
		if i > start {
			names = append(names, string(utf16.Decode(buf[start:i])))
		}
		start = i + 1
	}
	if start < len(buf) {
		names = append(names, string(utf16.Decode(buf[start:])))
	}
	return names
}

// remoteDriveFSType maps the NT target path of a drive letter onto the
// filesystem of the network share behind it, or "" when it is not a share.
// Based on some insights from the cygwin implementation.
func remoteDriveFSType(query dosDeviceQuery) (string, error) {
	names, err := queryDosDeviceNames(query)
	if err != nil {
		return "", err
	}

	for _, name := range names {
		switch {
		case strings.Contains(name, smbTargetMarker):
			return "smbfs", nil
		case strings.Contains(name, nfsTargetMarker):
			return "nfs", nil
		}
	}

	return "", nil
}
