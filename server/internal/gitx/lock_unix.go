//go:build !windows

package gitx

import (
	"os"
	"syscall"
)

// lockFile takes a non-blocking exclusive lock, held until f is closed.
func lockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}
