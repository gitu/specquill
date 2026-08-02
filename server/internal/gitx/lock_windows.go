//go:build windows

package gitx

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFile takes a non-blocking exclusive lock, held until f is closed.
func lockFile(f *os.File) error {
	return windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, &windows.Overlapped{})
}
