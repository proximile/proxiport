//go:build !windows
// +build !windows

package processes

import (
	"io"
	"strconv"
	"testing"

	"github.com/shirou/gopsutil/v3/process"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/proximile/proxiport/share/logger"
)

func TestProcPidStatSplit(t *testing.T) {
	var l1 = "0 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30 31 32 33 34 35 36 37 38 39 40 41 42 43 44 45 46 47 48 49 50 51"
	var l2 = "36101 ((sd-pam)) S 36099 36099 36099 0 -1 1077944640 27 0 0 0 0 0 0 0 20 0 1 0 319121869 56594432 984 18446744073709551615 1 1 0 0 0 0 0 4096 0 18446744073709551615 0 0 17 19 0 0 0 0 0 0 0 0 0 w x y z"
	var l3 = "36099 (systemd) S 1 36099 36099 0 -1 4202752 895 22 0 0 1 1 0 0 20 0 1 0 319121869 28123136 964 18446744073709551615 1 1 0 0 0 0 671173123 4096 0 18446744073709551615 0 0 17 2 0 0 0 0 0 0 0 0 0 0 0 0 0"
	var l4 = "17974 ([celeryd: celer) S 44582 44581 44581 0 -1 4202560 10130 0 0 0 59 13 0 0 20 0 3 0 317969348 965685248 19771 18446744073709551615 1 1 0 0 0 0 0 16781314 18949 18446744073709551615 0 0 17 2 0 0 0 0 0 0 0 0 0 0 0 0 0"
	var l5 = "17974 ([celeryd:) celer) S 44582 44581 44581 0 -1 4202560 10130 0 0 0 59 13 0 0 20 0 3 0 317969348 965685248 19771 18446744073709551615 1 1 0 0 0 0 0 16781314 18949 18446744073709551615 0 0 17 2 0 0 0 0 0 0 0 0 0 0 0 0 0"
	var l6 = "17974 ([celeryd: celer S 44582 44581 44581 0 -1 4202560 10130 0 0 0 59 13 0 0 20 0 3 0 317969348 965685248 19771 18446744073709551615 1 1 0 0 0 0 0 16781314 18949 18446744073709551615 0 0 17 2 0 0 0 0 0 0 0 0 0 0 0 0 0"

	parts1 := procPidStatSplit(l1)
	for i, part := range parts1 {
		val, err := strconv.ParseInt(part, 10, 32)
		if err != nil {
			t.Error("parsing number", part, err)
		}
		if i != int(val) {
			t.Error("field contents mismatch", i, part)
		}
	}
	if len(parts1) != 52 {
		t.Error("l1 split returned incorrect field count", 52)
	}

	parts2 := procPidStatSplit(l2)
	if len(parts2) != 52 {
		t.Error("l2 split returned incorrect field count", 52)
	}

	parts3 := procPidStatSplit(l3)
	if len(parts3) != 52 {
		t.Error("l3 split returned incorrect field count", 52)
	}

	parts4 := procPidStatSplit(l4)
	if len(parts4) != 52 {
		t.Error("l4 split returned incorrect field count", 52)
	}

	parts5 := procPidStatSplit(l5)
	if len(parts5) != 52 {
		t.Error("l5 split returned incorrect field count", 52)
	}

	parts6 := procPidStatSplit(l6)
	if len(parts6) != 52 {
		t.Error("l6 split returned incorrect field count", 52)
	}
	if parts6[0] != "17974" {
		t.Error("field 0 should be 17974 but is", parts6[0])
	}
	if parts6[1] != "" {
		t.Error("field 1 should be empty but is", parts6[1])
	}
	for i := 2; i <= 51; i++ {
		if parts6[i] != "" {
			t.Error("field ", i, "should be empty but is", parts6[i])
		}
	}
}

// What `ps axwwo pid,ppid,pgid,state,rss,vsz,pcpu,pmem,command` prints on a
// BSD system.
const psWithUsage = `  PID  PPID  PGID STAT     RSS   VSZ %CPU %MEM COMMAND
    1     0     1 I         96   952  0.0  0.0 /sbin/init
38493     1 38493 Ip       536  1096  1.5  0.2 /usr/sbin/sshd -D -o Option=yes
 6751 38493 38493 Z          0     0  0.0  0.0
   77
`

const psWithoutUsage = `  PID  PPID  PGRP STAT COMMAND
    1     0     1 I    /sbin/init
38493     1 38493 Ip   /usr/sbin/sshd -D
`

func newTestHandler() *ProcessHandler {
	return &ProcessHandler{
		logger:       logger.NewLogger("processes-test", logger.LogOutput{File: io.Discard}, logger.LogLevelDebug),
		processCache: NewProcessCache(),
	}
}

func noUsage(*process.Process, uint64) (uint64, uint64, float32, float32) { return 0, 0, 0, 0 }

// When the process library has no figures, the ones ps printed are used.
func TestPSFiguresAreUsedWhenTheLibraryHasNone(t *testing.T) {
	procs := newTestHandler().processesFromPSOutput([]byte(psWithUsage), 1<<30, noUsage)

	require.Len(t, procs, 3, "the short line is skipped")
	sshd := procs[1]
	assert.Equal(t, 38493, sshd.PID)
	assert.Equal(t, 1, sshd.ParentPID)
	assert.Equal(t, 38493, sshd.ProcessGID)
	assert.Equal(t, "idle", sshd.State)
	assert.Equal(t, "/usr/sbin/sshd -D -o Option=yes", sshd.Cmdline)
	assert.Equal(t, "sshd", sshd.Name)
	assert.Equal(t, uint64(536*1024), sshd.RSS)
	assert.Equal(t, uint64(1096*1024), sshd.VMS)
	assert.InDelta(t, 0.2, sshd.MemoryUsagePercent, 0.001)
	assert.InDelta(t, 1.5, sshd.CPUAverageUsagePercent, 0.001)

	zombie := procs[2]
	assert.Equal(t, 6751, zombie.PID)
	assert.Equal(t, "zombie", zombie.State)
	assert.Equal(t, uint64(0), zombie.RSS)
}

// Figures from the process library win over the ps columns.
func TestLibraryFiguresWinOverPS(t *testing.T) {
	fromLibrary := func(*process.Process, uint64) (uint64, uint64, float32, float32) { return 4096, 8192, 3, 4 }
	procs := newTestHandler().processesFromPSOutput([]byte(psWithUsage), 1<<30, fromLibrary)

	require.Len(t, procs, 3)
	assert.Equal(t, uint64(4096), procs[1].RSS)
	assert.Equal(t, uint64(8192), procs[1].VMS)
	assert.InDelta(t, 3, procs[1].MemoryUsagePercent, 0.001)
	assert.InDelta(t, 4, procs[1].CPUAverageUsagePercent, 0.001)
}

// A ps without the usage columns still gives the process list.
func TestPSWithoutUsageColumns(t *testing.T) {
	procs := newTestHandler().processesFromPSOutput([]byte(psWithoutUsage), 1<<30, noUsage)

	require.Len(t, procs, 2)
	assert.Equal(t, "/usr/sbin/sshd -D", procs[1].Cmdline)
	assert.Equal(t, 38493, procs[1].ProcessGID)
	assert.Equal(t, uint64(0), procs[1].RSS)
}

func TestEmptyPSOutput(t *testing.T) {
	assert.Empty(t, newTestHandler().processesFromPSOutput(nil, 1<<30, noUsage))
}

// A process library that panics must not take the caller down.
func TestResourceUsageSurvivesAPanicInTheLibrary(t *testing.T) {
	var none *process.Process // every call on it dereferences nil
	assert.NotPanics(t, func() {
		rss, vms, mem, cpu := newTestHandler().gatherProcessResourceUsage(none, 1<<30)
		assert.Zero(t, rss)
		assert.Zero(t, vms)
		assert.Zero(t, mem)
		assert.Zero(t, cpu)
	})
}
