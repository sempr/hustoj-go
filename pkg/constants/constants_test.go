package constants

import (
	"os/exec"
	"strings"
	"testing"
)

// phpJudgeResultNames 是 phpweb/include/const.inc.php 中 $judge_result 的元素名，
// 按下标（即结果码）排列。PHP 侧若重排、插入或改名，本测试即失败。
// 该数组是 solution.result 的权威定义，Go 侧必须逐位对齐。
const phpJudgeResultCount = 18

var phpJudgeResultNames = [phpJudgeResultCount]string{
	"MSG_Pending",             //  0 WT0 等待
	"MSG_Pending_Rejudging",   //  1 WT1 等待重判
	"MSG_Compiling",           //  2 CI  编译中
	"MSG_Running_Judging",     //  3 RI  运行并评判
	"MSG_Accepted",            //  4 AC  正确
	"MSG_Presentation_Error",  //  5 PE  格式错误
	"MSG_Wrong_Answer",        //  6 WA  答案错误
	"MSG_Time_Limit_Exceed",   //  7 TL  时间超限
	"MSG_Memory_Limit_Exceed", //  8 ML  内存超限
	"MSG_Output_Limit_Exceed", //  9 OL  输出超限
	"MSG_Runtime_Error",       // 10 RE  运行错误
	"MSG_Compile_Error",       // 11 CE  编译错误
	"MSG_Compile_OK",          // 12 CO  编译成功
	"MSG_TEST_RUN",            // 13 TR  运行完成
	"MSG_MANUAL_CONFIRMATION", // 14 MC  自动评测通过，等待人工确认
	"MSG_SUBMITTING",          // 15 ST  提交中
	"MSG_REMOTE_PENDING",      // 16 RP  远程等待
	"MSG_REMOTE_JUDGING",      // 17 RJ  远程判题
}

// TestOJResultNamesCoverPHPArray 保证 ojResultNames 的长度与 PHP $judge_result 一致。
// 漏补一个结果码会让 GetOJResultName 对越界值返回 "OT"，表现为线上判罚文案变成"其它"。
func TestOJResultNamesCoverPHPArray(t *testing.T) {
	if got := len(ojResultNames); got != phpJudgeResultCount {
		t.Fatalf("ojResultNames 长度 = %d，PHP $judge_result 元素数 = %d", got, phpJudgeResultCount)
	}
}

// TestGetOJResultName 逐个码值断言名称。
func TestGetOJResultName(t *testing.T) {
	want := map[int]string{
		OJ_WT0: "WT0", OJ_WT1: "WT1", OJ_CI: "CI", OJ_RI: "RI", OJ_AC: "AC",
		OJ_PE: "PE", OJ_WA: "WA", OJ_TL: "TL", OJ_ML: "ML", OJ_OL: "OL",
		OJ_RE: "RE", OJ_CE: "CE", OJ_CO: "CO", OJ_TR: "TR", OJ_MC: "MC",
		OJ_ST: "ST", OJ_RP: "RP", OJ_RJ: "RJ",
		OJ_SE: "SE",
	}
	for code, name := range want {
		if got := GetOJResultName(code); got != name {
			t.Errorf("GetOJResultName(%d) = %q，期望 %q", code, got, name)
		}
	}

	for _, code := range []int{-1, 18, 50, 98, 100} {
		if got := GetOJResultName(code); got != "OT" {
			t.Errorf("GetOJResultName(%d) = %q，越界应返回 \"OT\"", code, got)
		}
	}
}

// TestResultCodesContiguous 保证 0..17 连续且无重复，防止插入时错位。
func TestResultCodesContiguous(t *testing.T) {
	codes := []int{OJ_WT0, OJ_WT1, OJ_CI, OJ_RI, OJ_AC, OJ_PE, OJ_WA, OJ_TL,
		OJ_ML, OJ_OL, OJ_RE, OJ_CE, OJ_CO, OJ_TR, OJ_MC, OJ_ST, OJ_RP, OJ_RJ}
	for i, c := range codes {
		if c != i {
			t.Errorf("第 %d 个结果码 = %d，应为 %d（判罚码必须与 PHP 数组下标对齐）", i, c, i)
		}
	}
}

// TestAgainstPHPConstInc 直接读取 phpweb/include/const.inc.php 并比对结果码名称。
// phpweb 未纳入 git 时该测试自动跳过，避免在没有 PHP 源码的环境误报。
func TestAgainstPHPConstInc(t *testing.T) {
	path := "../../phpweb/include/const.inc.php"
	out, err := exec.Command("php", "-r", `
		$f = $argv[1];
		if (!is_file($f)) { fwrite(STDERR, "missing\n"); exit(2); }
		$src = file_get_contents($f);
		if (!preg_match('/\$judge_result\s*=\s*Array\(([^;]*)\)/s', $src, $m)) {
			fwrite(STDERR, "no judge_result\n"); exit(3);
		}
		echo trim($m[1]);
	`, path).Output()
	if err != nil {
		t.Skipf("跳过：无法从 %s 解析 $judge_result（php 未安装或文件缺失）", path)
	}

	got := strings.Split(strings.TrimSpace(string(out)), ",")
	for i := range got {
		got[i] = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(got[i]), "$"))
	}
	if len(got) != phpJudgeResultCount {
		t.Fatalf("PHP $judge_result 元素数 = %d，期望 %d：%v", len(got), phpJudgeResultCount, got)
	}
	for i, elem := range got {
		if !strings.HasPrefix(elem, "MSG_") {
			t.Errorf("第 %d 项 = %q，不是 $MSG_ 变量", i, elem)
			continue
		}
		if elem != phpJudgeResultNames[i] {
			t.Errorf("第 %d 项（结果码 %d）PHP = %q，期望 %q —— 两侧判罚码已错位",
				i, i, elem, phpJudgeResultNames[i])
		}
	}

	// 同时校验 Go 侧名称表与该映射下标一致
	for i := range phpJudgeResultNames {
		if GetOJResultName(i) != ojResultNames[i] {
			t.Errorf("GetOJResultName(%d) = %q，期望 %q", i, GetOJResultName(i), ojResultNames[i])
		}
	}
}
