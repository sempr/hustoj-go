package daemon

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
)

var lockFile *os.File

// Lock creates and locks a PID file.
func Lock(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return err
	}
	lockFile = f

	// Try to get an exclusive, non-blocking lock.
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		// EAGAIN or EWOULDBLOCK means the lock is already held.
		if err == syscall.EAGAIN {
			return fmt.Errorf("file %s is already locked", path)
		}
		return fmt.Errorf("could not lock file %s: %w", path, err)
	}

	// Write the current PID to the file.
	pid := os.Getpid()
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.WriteString(strconv.Itoa(pid)); err != nil {
		return err
	}
	return f.Sync()
}

// Unlock releases the PID file lock.
func Unlock() {
	if lockFile != nil {
		syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		lockFile.Close()
	}
}
