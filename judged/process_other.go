//go:build !linux

package main

import (
	"os/exec"
)

// This is the non-Linux implementation. It's a no-op.
func setResourceLimits(cmd *exec.Cmd, cfg *Config) error {
	AppLogger.Println("WARN: Resource limits (rlimit) are not supported on this OS. Running without restrictions.")
	return nil
}