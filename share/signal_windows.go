//go:build windows
// +build windows

package chshare

import "time"

// SleepSignal sleeps for the given duration. Windows has no SIGHUP to cut
// the sleep short.
func SleepSignal(d time.Duration) {
	time.Sleep(d) //not supported
}
