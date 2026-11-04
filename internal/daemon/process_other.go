//go:build !linux

package daemon

import (
	"log/slog"
	"os/exec"
)

// configureChildProcess 在非 Linux 平台上没有额外属性要设置。
func configureChildProcess(cmd *exec.Cmd) {}

// applyResourceLimits 在非 Linux 平台上不做限制。
func applyResourceLimits(pid int, cfg *DaemonConfig) error {
	slog.Warn("Resource limits (rlimit) are not supported on this OS. Running without restrictions.")
	return nil
}
