//go:build linux

package daemon

import (
	"fmt"
	"os/exec"

	"golang.org/x/sys/unix"
)

// configureChildProcess 设置只能在 Start 之前写入的进程属性。
func configureChildProcess(cmd *exec.Cmd) {
	// Pdeathsig ensures the child process is killed if the parent (judged) dies.
	cmd.SysProcAttr = &unix.SysProcAttr{
		Pdeathsig: unix.SIGKILL,
	}
}

// applyResourceLimits 只对已经启动的 client 子进程（及其后代）设置 rlimit。
//
// 旧实现直接调用 unix.Setrlimit，改的是 daemon 自己的限制：RLIMIT_CPU=800
// 会在长期运行后把 daemon 打死，RLIMIT_AS 等也限制到了错误的进程。这里用
// prlimit 精确作用到子进程；client 随后 fork 的 run/tini/用户程序继承这些值。
func applyResourceLimits(pid int, cfg *DaemonConfig) error {
	setRlimit := func(resource int, cur, max uint64) error {
		return unix.Prlimit(pid, resource, &unix.Rlimit{Cur: cur, Max: max}, nil)
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
