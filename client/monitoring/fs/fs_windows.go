//go:build windows
// +build windows

package fs

import (
	"context"

	"github.com/pkg/errors"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// getPartitions wraps gopsutil/disk.Partitions and also determines fs type of network drives
func getPartitions(onlyUniqueDevices bool) ([]disk.PartitionStat, error) {
	ctx, cancel := context.WithTimeout(context.Background(), fsInfoRequestTimeout)
	defer cancel()

	partitions, err := disk.PartitionsWithContext(ctx, true)
	if err != nil {
		// Ignore warnings. Warning might happen if the network drive is not connected
		if _, ok := err.(*disk.Warnings); !ok {
			return nil, err
		}
	}
	for i, partition := range partitions {
		typepath, _ := windows.UTF16PtrFromString(partition.Mountpoint)
		remoteDriveType, err := tryRetrieveRemoteDriveFSType(typepath)
		// Ignore errors that might happen trying to identify remote drive type
		if err != nil {
			continue
		}
		if remoteDriveType != "" {
			partitions[i].Fstype = remoteDriveType
		}
	}
	return partitions, nil
}

// tryRetrieveRemoteDriveFSType can detect the original network share filesystem.
// If filesystem wasn't recognized, the empty string returned.
//
// All of the buffer handling lives in remoteDriveFSType, which is portable and
// therefore actually tested; the only thing here is the syscall itself. The
// buffer is a []uint16 and the length handed to the kernel is len() of that same
// slice, because ucchMax counts UTF-16 code units -- see dosDeviceQuery.
func tryRetrieveRemoteDriveFSType(drivePath *uint16) (string, error) {
	fsType, err := remoteDriveFSType(func(buf []uint16) (uint32, error) {
		//nolint:gosec // len(buf) is bounded by dosDeviceMaxBufLen; see dosDeviceQuery
		n, err := windows.QueryDosDevice(drivePath, &buf[0], uint32(len(buf)))
		if errors.Is(err, windows.ERROR_INSUFFICIENT_BUFFER) {
			return 0, errBufferTooSmall
		}
		return n, err
	})
	if err != nil {
		return "", errors.Wrapf(err, "while QueryDosDevice call")
	}
	return fsType, nil
}

// enablePerformanceCounters will enable performance counters by adding the EnableCounterForIoctl registry key
func enablePerformanceCounters() error {
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, "SYSTEM\\CurrentControlSet\\Services\\partmgr", registry.READ|registry.WRITE)
	if err != nil {
		return errors.Errorf("cannot open new key in the registry in order to enable the performance counters: %s", err)
	}
	val, _, err := key.GetIntegerValue("EnableCounterForIoctl")
	if val != 1 || err != nil {
		if err = key.SetDWordValue("EnableCounterForIoctl", 1); err != nil {
			return errors.Errorf("cannot create HKLM:SYSTEM\\CurrentControlSet\\Services\\Partmgr\\EnableCounterForIoctl key in the registry in order to enable the performance counters: %s", err)
		}
		logrus.Info("The registry key EnableCounterForIoctl at HKLM:SYSTEM\\CurrentControlSet\\Services\\Partmgr has been created in order to enable the performance counters")
	}
	return nil
}
