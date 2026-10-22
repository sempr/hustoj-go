package client

import "testing"

// 内存上限的单位只在一处转换：RunConfig.MemoryLimit 是 MB，沙箱的 --memory
// 是 KB。历史上测试运行把 KB 填进了 MB 的字段，cgroup 的 memory.max 被放大
// 1024 倍（256MB → 256TB），内存限制等于没有 —— 探针一口气申请了 2.4GB
// 才被宿主机的 OOM killer 收走。
func TestSandboxMemoryKB(t *testing.T) {
	tests := []struct {
		name string
		mb   int
		want int
	}{
		{"题目常见上限 128MB", 128, 131072},
		{"测试运行 256MB", testRunMemoryMB, 262144},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sandboxMemoryKB(tc.mb); got != tc.want {
				t.Errorf("sandboxMemoryKB(%d) = %d，期望 %d", tc.mb, got, tc.want)
			}
		})
	}
}

// 测试运行的内存上限必须是 MB 量级：填成 KB 会被 <<10 再放大一次。
func TestTestRunMemoryLimitInMB(t *testing.T) {
	if testRunMemoryMB <= 0 {
		t.Fatalf("测试运行的内存上限应为正数，实际 %d", testRunMemoryMB)
	}
	if got := sandboxMemoryKB(testRunMemoryMB); got > 1024*1024 {
		t.Errorf("测试运行的内存上限换算后为 %d KB，明显超出题目常见范围（单位可能写错了）", got)
	}
}
