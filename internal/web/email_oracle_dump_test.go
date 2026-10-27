package web

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"testing"
)

// TestEmailOracleDump 把 Go 对一批候选项的判定导出，交给 PHP 复核。
//
// 这是 validEmail 的差分测试工具：真实 PHP 用
//
//	php -r '...' < <(HUSTOJ_EMAIL_CASES=cases HUSTOJ_EMAIL_DUMP=out go test -run TestEmailOracleDump)
//
// 与 filter_var 逐条比对。判定值在导出时用 base64 编码候选项，
// 避免候选项自身含制表符/换行时把 TSV 格式撕裂。
//
// 仅在同时设置 HUSTOJ_EMAIL_CASES 与 HUSTOJ_EMAIL_DUMP 时启用，
// 常规测试运行会自动跳过。email_oracle_test.go 里的固化数据就是这么筛出来的。
func TestEmailOracleDump(t *testing.T) {
	out := os.Getenv("HUSTOJ_EMAIL_DUMP")
	if out == "" {
		t.Skip("跳过")
	}
	in, err := os.Open(os.Getenv("HUSTOJ_EMAIL_CASES"))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	defer w.Flush()
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		s := sc.Text()
		// 用 base64 编码输入，避免候选项自身含有制表符/换行时
		// 把 TSV 格式撕裂（之前就因此产生 6 条假不一致）。
		fmt.Fprintf(w, "%s\t%v\n", base64.StdEncoding.EncodeToString([]byte(s)), validEmail(s))
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
}
