//go:build unix

package gitx

import (
	"os"

	"golang.org/x/sys/unix"
)

// lockFile takes a non-blocking exclusive lock, held until f is closed.
func lockFile(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
}
