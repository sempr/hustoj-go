//go:build linux

// Package limits 集中定义判题进程的资源上限。
//
// 这些值原先散落在 internal/daemon/process_linux.go：daemon 用 prlimit
// 作用到 client 子进程；rootless 的 hustoj-spawn 没有 daemon 可依赖，
// 必须自己对（即将 exec 的）进程设置同一组限制，故抽到这里。
package limits

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// STD_MB 是 1 MiB（RLIMIT_FSIZE/RLIMIT_AS 的单位是字节）。
const STD_MB = 1048576

// Limits 是一组对判题 client 及其后代生效的 rlimit。
type Limits struct {
	CPU   uint64 // 秒
	FSize uint64 // 字节
	NProc uint64 // 进程数
	AS    uint64 // 字节
}

// Default 返回与历史 daemon 行为一致的限制。
//
// maxRunning 是 daemon 的并发数；RLIMIT_NPROC 需要按并发放大，否则多个
// 判题任务共用同一 uid 时会互相挤占。
func Default(maxRunning int) Limits {
	if maxRunning < 1 {
		maxRunning = 1
	}
	return Limits{
		CPU:   800,
		FSize: 1024 * STD_MB,
		NProc: uint64(800 * maxRunning),
		AS:    uint64(STD_MB << 15), // 32 GiB
	}
}

// ApplySelf 把限制设置到当前进程（随后 exec 的进程会继承）。
func (l Limits) ApplySelf() error {
	return l.apply(func(resource int, cur, max uint64) error {
		return unix.Setrlimit(resource, &unix.Rlimit{Cur: cur, Max: max})
	})
}

// ApplyPID 只对指定进程（及其后代）设置限制。
func (l Limits) ApplyPID(pid int) error {
	return l.apply(func(resource int, cur, max uint64) error {
		return unix.Prlimit(pid, resource, &unix.Rlimit{Cur: cur, Max: max}, nil)
	})
}

func (l Limits) apply(set func(resource int, cur, max uint64) error) error {
	if err := set(unix.RLIMIT_CPU, l.CPU, l.CPU); err != nil {
		return fmt.Errorf("failed to set RLIMIT_CPU: %w", err)
	}
	if err := set(unix.RLIMIT_FSIZE, l.FSize, l.FSize); err != nil {
		return fmt.Errorf("failed to set RLIMIT_FSIZE: %w", err)
	}
	if err := set(unix.RLIMIT_NPROC, l.NProc, l.NProc); err != nil {
		return fmt.Errorf("failed to set RLIMIT_NPROC: %w", err)
	}
	if err := set(unix.RLIMIT_AS, l.AS, l.AS); err != nil {
		return fmt.Errorf("failed to set RLIMIT_AS: %w", err)
	}
	return nil
}
