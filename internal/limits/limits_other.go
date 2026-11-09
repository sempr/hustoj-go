//go:build !linux

// Package limits 在非 Linux 平台只保留编译所需的空实现。
package limits

import "fmt"

const STD_MB = 1048576

type Limits struct {
	CPU   uint64
	FSize uint64
	NProc uint64
	AS    uint64
}

func Default(maxRunning int) Limits { return Limits{} }

func (l Limits) ApplySelf() error { return fmt.Errorf("resource limits are only supported on linux") }

func (l Limits) ApplyPID(pid int) error {
	return fmt.Errorf("resource limits are only supported on linux")
}
