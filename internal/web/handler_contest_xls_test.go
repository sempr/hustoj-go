package web

import (
	"fmt"
	"strings"
	"testing"
)

// contestrank.xls.php 的 mark 评分与排序是纯计算，不需要数据库。
//
// 这几个用例守的都是已经踩到过的坑：

//  1. usort 的回调语义。PHP 的 s_cmp 写 `$A->solved < $B->solved`，但 usort
//     把返回的 true 当作"排在后面"，所以实际是 solved 降序。照抄字面会把整张
//     榜反过来 —— 第一版就是这么错的，导出里 0 解选手排在第 1 名。
//  2. usort 是稳定排序，SliceStable 才能复现并列选手的原始（user_id）顺序。
//  3. getMark 完全覆盖 Add 里的 mark 累加，mark 只由名次决定。
func TestXlsSortMatchesUsortSemantics(t *testing.T) {
	mk := func(id string, solved, tm int) *xlsTeam {
		tm2 := newXlsTeam(id, id)
		tm2.Solved = solved
		tm2.Time = tm
		return tm2
	}
	// 并列 0 解的选手必须保持查询里的 user_id 顺序。
	teams := []*xlsTeam{
		mk("u1", 0, 0),
		mk("u2", 4, 72),
		mk("u3", 0, 0),
		mk("u4", 4, 18),
		mk("u5", 0, 0),
		mk("u6", 2, 30),
	}
	sortXlsTeams(teams)

	var got []string
	for _, tm := range teams {
		got = append(got, fmt.Sprintf("%s:%d/%d", tm.UserID, tm.Solved, tm.Time))
	}
	want := []string{"u4:4/18", "u2:4/72", "u6:2/30", "u1:0/0", "u3:0/0", "u5:0/0"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("排序结果\n got %v\nwant %v", got, want)
	}
}

// getMark 用 (60+100)/2+10 = 90 为均值、(100-60)/5 = 8 为标准差，把名次按正态
// 分布映射到 60..100。名次越靠前分越高，且分布峰值在中段。
func TestXlsGetMarkIsRankBasedNormalDistribution(t *testing.T) {
	teams := make([]*xlsTeam, 200)
	for i := range teams {
		teams[i] = newXlsTeam(fmt.Sprintf("u%03d", i), "")
	}
	xlsGetMark(teams, xlsMarkStart, xlsMarkEnd, xlsMarkSigma)

	if got := teams[0].Mark; got != 100 {
		t.Fatalf("第 1 名 mark = %v, 期望 100", got)
	}
	if got := teams[len(teams)-1].Mark; got > 90 {
		t.Fatalf("末位 mark = %v, 正态分布尾部应明显低于峰值", got)
	}
	for i := 1; i < len(teams); i++ {
		if teams[i].Mark > teams[i-1].Mark {
			t.Fatalf("第 %d 名 mark %v 高于上一名 %v，分布应单调不增", i, teams[i].Mark, teams[i-1].Mark)
		}
		if teams[i].Mark < xlsMarkStart || teams[i].Mark > xlsMarkEnd {
			t.Fatalf("第 %d 名 mark %v 越界 [%d,%d]", i, teams[i].Mark, xlsMarkStart, xlsMarkEnd)
		}
	}
	// 峰值应当出现在中段而不是两端，验证确实按正态而不是线性分配。
	mid := teams[len(teams)/2].Mark
	if mid <= teams[len(teams)-1].Mark {
		t.Fatalf("中段 mark %v 未高于末位 %v", mid, teams[len(teams)-1].Mark)
	}
}

// getMark 之前不能除零：PHP 在空榜上会抛 DivisionByZeroError 并把响应截断在
// </tr>，Go 必须输出完整的空表。
func TestXlsGetMarkEmptyDoesNotPanic(t *testing.T) {
	xlsGetMark(nil, xlsMarkStart, xlsMarkEnd, xlsMarkSigma)
}

// Add 里的 mark 运算要用 intval 截断，不能用浮点比较：mark_per_problem=20 时
// mark_per_punish=4，5 次 WA 的惩罚正好等于封顶，边界上差 1 就会被看出来。
func TestXlsAddMarkTruncatesPunishLikeIntval(t *testing.T) {
	const per = 20.0
	tm := newXlsTeam("u", "")
	// 先错 5 次再通过：intval(5 * 4) = 20 >= intval(20*0.8)=16 → 扣满 16。
	for i := 0; i < 5; i++ {
		tm.xlsAdd(0, 10, 5, false, 60, per, per/5)
	}
	tm.xlsAdd(0, 20, 4, false, 60, per, per/5)
	if got, want := tm.Mark, 60.0-16; got != want {
		t.Fatalf("mark = %v, 期望 %v（60 基础分扣满 16 封顶）", got, want)
	}

	// 只错 1 次：intval(1*4)=4 < 16 → 只扣 4。
	tm2 := newXlsTeam("u", "")
	tm2.xlsAdd(0, 10, 5, false, 60, per, per/5)
	tm2.xlsAdd(0, 20, 4, false, 60, per, per/5)
	if got, want := tm2.Mark, 60.0-4; got != want {
		t.Fatalf("mark = %v, 期望 %v", got, want)
	}
}

// 单题比赛 mark_per_problem=0，惩罚封顶也是 0，所以无论错多少次 mark 都是
// 基础分（contestrank.xls.php:214-216 会把 mark_base 改成 100）。
func TestXlsAddSingleProblemKeepsFullMark(t *testing.T) {
	tm := newXlsTeam("u", "")
	for i := 0; i < 9; i++ {
		tm.xlsAdd(0, 10, 5, false, 100, 0, 0)
	}
	tm.xlsAdd(0, 20, 4, false, 100, 0, 0)
	if tm.Mark != 100 {
		t.Fatalf("mark = %v, 单题比赛期望 100", tm.Mark)
	}
}

// CE 的处理与 contestrank.php 一致：$OJ_CE_PENALTY 关闭时 CE 不计错误次数。
func TestXlsAddIgnoresCEWhenPenaltyDisabled(t *testing.T) {
	tm := newXlsTeam("u", "")
	tm.xlsAdd(0, 10, 11, false, 60, 20, 4)
	if tm.Solved != 0 || len(tm.WANum) != 0 {
		t.Fatalf("CE 被计入：solved=%d wa=%v", tm.Solved, tm.WANum)
	}
	tm.xlsAdd(0, 20, 4, false, 60, 20, 4)
	if tm.Solved != 1 || tm.Time != 20 {
		t.Fatalf("AC 后 solved=%d time=%d，期望 1/20（CE 不产生罚时）", tm.Solved, tm.Time)
	}

	// 开启 CE 罚时时 CE 计入错误次数。
	tm2 := newXlsTeam("u", "")
	tm2.xlsAdd(0, 10, 11, true, 60, 20, 4)
	if tm2.WANum[0] != 1 {
		t.Fatalf("CE 应计入 WA 次数，实际 %v", tm2.WANum)
	}
}

// 与 contestrank.php 不同，XLS 的 Add 没有 `if ($sec < 0) return;`：
// 开赛前的提交照样计入，罚时可以变成负数。
func TestXlsAddAcceptsNegativeTime(t *testing.T) {
	tm := newXlsTeam("u", "")
	tm.xlsAdd(0, -3600, 4, false, 60, 0, 0)
	if tm.Solved != 1 {
		t.Fatalf("solved = %d, 期望 1（负时间提交仍算通过）", tm.Solved)
	}
	if tm.Time != -3600 {
		t.Fatalf("time = %d, 期望 -3600", tm.Time)
	}
	if _, ok := tm.acSec(0); ok {
		t.Fatal("acSec 不应返回 sec<=0 的通过时刻（PHP 判据是 > 0）")
	}
}

// 输出时的公式注入前缀：PHP 的字符类 /^[=+-@]/ 里的 `-` 没转义，实际是
// 0x2B..0x40 的区间，把 , - . / 0-9 : ; < > ? 也一并命中了。
func TestXlsExcelInjectionMatchesPHPCharClass(t *testing.T) {
	hit := "<>?@=+-:;/.,09"
	for _, r := range hit {
		if !phpExcelInjectionRune(r) {
			t.Fatalf("字符 %q 期望命中 /^[=+-@]/", r)
		}
	}
	miss := "!\"#$%&'()* ABCXYZ_`{}[]|~\t"
	for _, r := range miss {
		if phpExcelInjectionRune(r) {
			t.Fatalf("字符 %q 不该命中 /^[=+-@]/", r)
		}
	}
}

// 文件名要复刻 mb_substr(32) → 拼 C<cid>- → 去空格 → 去 . \ | : * ? " < >
// → 追加 .xls → rawurlencode 的顺序。rawurlencode 与 QueryEscape 不同：
// 空格是 %20 而非 +，且 ~ 不转义。
func TestXlsFilename(t *testing.T) {
	cases := []struct {
		cid   int32
		title string
		want  string
	}{
		{1000, "归档", "C1000-%E5%BD%92%E6%A1%A3.xls"},
		// 空格与特殊字符被删除，中文保留并百分号编码。
		{1, `A B.C\D|E:F*G?H"I<J>K`, "C1-ABCDEFGHIJK.xls"},
		// 超过 32 个字符时按字符（不是字节）截断，且截断发生在拼前缀之前。
		{2, strings.Repeat("长", 40), "C2-" + phpRawURLEncode(strings.Repeat("长", 32)) + ".xls"},
	}
	for _, c := range cases {
		if got := xlsFilename(c.cid, c.title); got != c.want {
			t.Errorf("xlsFilename(%d, %q)\n got %s\nwant %s", c.cid, c.title, got, c.want)
		}
	}
	// 空格必须是 %20，~ 必须原样保留。
	if got := phpRawURLEncode("a b~c+d"); got != "a%20b~c%2Bd" {
		t.Fatalf("rawurlencode = %s", got)
	}
}

// 昵称转义对应 PHP 8 的 htmlspecialchars() 默认行为（ENT_QUOTES 生效）。
func TestXlsHTMLSpecialChars(t *testing.T) {
	got := phpHTMLSpecialChars(`<b>a&b</b>"c'd"`)
	want := "&lt;b&gt;a&amp;b&lt;/b&gt;&quot;c&#039;d&quot;"
	if got != want {
		t.Fatalf("got %s\nwant %s", got, want)
	}
}

// sec2str 必须复刻 PHP 对负数的处理：sprintf("%02d:%02d:%02d", $sec/3600,
// $sec%3600/60, $sec%60) 会把负号算进宽度并向零截断，续赛比赛的负时间不能被
// 夹成 0。
func TestSec2strNegativeMatchesPHP(t *testing.T) {
	cases := []struct {
		sec  int
		want string
	}{
		{0, "00:00:00"},
		{72, "00:01:12"},
		{86400, "24:00:00"},
		{-3600, "-1:00:00"},
		{-3661, "-1:-1:-1"},
		{-61, "00:-1:-1"},
	}
	for _, c := range cases {
		if got := sec2str(c.sec); got != c.want {
			t.Errorf("sec2str(%d) = %s, 期望 %s", c.sec, got, c.want)
		}
	}
}

// 表头题号列要复刻 include/const.inc.php 的 $PID。
func TestXlsProblemLettersMatchPIDArray(t *testing.T) {
	// include/const.inc.php 的 $PID 是 702 项的 Excel 列名式编号
	// （A..Z 后接 AA..ZZ），下标 0..701。
	for num, want := range map[int]string{0: "A", 25: "Z", 26: "AA", 51: "AZ", 52: "BA", 701: "ZZ"} {
		if got := contestProblemLetter(num); got != want {
			t.Errorf("contestProblemLetter(%d) = %s, 期望 %s", num, got, want)
		}
	}
}
