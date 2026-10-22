package client

import (
	"testing"

	"github.com/sempr/hustoj-go/pkg/constants"
)

// 判罚码要能从"程序非零退出/被信号杀死"里区分出运行错误，否则崩溃的程序
// 会被当成答案错误。
func TestVerdictFromExit(t *testing.T) {
	tests := []struct {
		name       string
		userStatus int
		exitStatus int
		want       int
	}{
		{"正常退出保持正确", constants.OJ_AC, 0, constants.OJ_AC},
		{"非零退出判运行错误", constants.OJ_AC, 1, constants.OJ_RE},
		{"被信号杀死判运行错误", constants.OJ_AC, 128, constants.OJ_RE},
		{"超时优先于退出码", constants.OJ_TL, 137, constants.OJ_TL},
		{"超内存优先于退出码", constants.OJ_ML, 137, constants.OJ_ML},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := verdictFromExit(tc.userStatus, tc.exitStatus); got != tc.want {
				t.Errorf("verdictFromExit(%d, %d) = %d，期望 %d",
					tc.userStatus, tc.exitStatus, got, tc.want)
			}
		})
	}
}
