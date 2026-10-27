package run

import (
	"testing"

	"golang.org/x/sys/unix"
)

// 退出码要能区分"正常退出"和"被信号杀死"，判题器靠它给出运行错误。
func TestExitStatusOf(t *testing.T) {
	tests := []struct {
		name string
		ws   unix.WaitStatus
		want int
	}{
		{"正常退出 0", unix.WaitStatus(0), 0},
		{"正常退出 3", unix.WaitStatus(3 << 8), 3},
		{"被 SIGABRT 杀死", unix.WaitStatus(unix.SIGABRT), 128 + int(unix.SIGABRT)},
		{"被 SIGSEGV 杀死", unix.WaitStatus(unix.SIGSEGV), 128 + int(unix.SIGSEGV)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitStatusOf(tc.ws); got != tc.want {
				t.Errorf("exitStatusOf(%#x) = %d，期望 %d", uint32(tc.ws), got, tc.want)
			}
		})
	}
}
