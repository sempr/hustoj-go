//go:build linux

package main

import (
	"fmt"
	"os/exec"
	"golang.org/x/sys/unix"
)

// This is the Linux-specific implementation of setResourceLimits.
func setResourceLimits(cmd *exec.Cmd, cfg *Config) error {
	// Pdeathsig ensures the child process is killed if the parent (judged) dies.
	cmd.SysProcAttr = &unix.SysProcAttr{
		Pdeathsig: unix.SIGKILL,
	}

	// Wrapper function to simplify setting a resource limit.
	setRlimit := func(resource int, cur, max uint64) error {
		return unix.Setrlimit(resource, &unix.Rlimit{Cur: cur, Max: max})
	}

	var err error
	// RLIMIT_CPU
	if err = setRlimit(unix.RLIMIT_CPU, 800, 800); err != nil {
		return fmt.Errorf("failed to set RLIMIT_CPU: %w", err)
	}
	// RLIMIT_FSIZE
	if err = setRlimit(unix.RLIMIT_FSIZE, 1024*STD_MB, 1024*STD_MB); err != nil {
		return fmt.Errorf("failed to set RLIMIT_FSIZE: %w", err)
	}
	// RLIMIT_NPROC
	if err = setRlimit(unix.RLIMIT_NPROC, uint64(800*cfg.MaxRunning), uint64(800*cfg.MaxRunning)); err != nil {
		return fmt.Errorf("failed to set RLIMIT_NPROC: %w", err)
	}
	// RLIMIT_AS (Memory)
	memLimit := uint64(STD_MB << 15) // 32 GB for x86_64
	if err = setRlimit(unix.RLIMIT_AS, memLimit, memLimit); err != nil {
		return fmt.Errorf("failed to set RLIMIT_AS: %w", err)
	}

	return nil
}