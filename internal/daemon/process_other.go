//go:build !linux

package daemon

import (
	"log/slog"
	"os/exec"
)

// This is the non-Linux implementation. It's a no-op.
func setResourceLimits(cmd *exec.Cmd, cfg *Config) error {
	slog.Warn("Resource limits (rlimit) are not supported on this OS. Running without restrictions.")
	return nil
}
