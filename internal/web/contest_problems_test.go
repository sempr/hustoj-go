package web

import (
	"testing"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// TestPIDLetter 对齐 const.inc.php:14 的 $PID。
//
// 关键边界是 25/26 与 701/702：Z 之后是 AA 而不是 BA；
// $PID 实测只有 702 项，越界返回空串。
func TestPIDLetter(t *testing.T) {
	cases := []struct {
		idx  int32
		want string
	}{
		{0, "A"}, {1, "B"}, {25, "Z"}, {26, "AA"}, {27, "AB"},
		{51, "AZ"}, {52, "BA"}, {700, "ZY"}, {701, "ZZ"},
		// 越界：PHP 读未定义下标得 null，渲染成空串。
		{702, ""}, {18277, ""}, {-1, ""},
	}
	for _, c := range cases {
		if got := pidLetter(c.idx); got != c.want {
			t.Errorf("pidLetter(%d) = %q, 期望 %q", c.idx, got, c.want)
		}
	}
}

// TestContestACMarkOf 复刻 check_ac（my_func.inc.php:616）的三条分支。
func TestContestACMarkOf(t *testing.T) {
	cases := []struct {
		name string
		c    dbgen.ContestProblemACCounts
		noip bool
		want acMark
	}{
		{"普通模式已 AC", dbgen.ContestProblemACCounts{SubTotal: 5, ACCount: 1}, false, markAC},
		{"普通模式交过没过", dbgen.ContestProblemACCounts{SubTotal: 3, BadCount: 3}, false, markBad},
		{"普通模式没交", dbgen.ContestProblemACCounts{}, false, markNone},
		{"NOIP 交过就只给问号", dbgen.ContestProblemACCounts{SubTotal: 5, ACCount: 1}, true, markAsked},
		{"NOIP 没交", dbgen.ContestProblemACCounts{SubTotal: 0, ACCount: 0}, true, markNone},
		// PHP 的 AC 统计**不带** problem_id 过滤，
		// 所以"练习提交也算 AC"是既有行为，必须照搬。
		{"练习提交也能算 AC", dbgen.ContestProblemACCounts{SubTotal: 0, ACCount: 1}, false, markAC},
	}
	for _, c := range cases {
		if got := contestACMarkOf(c.c, c.noip); got != c.want {
			t.Errorf("%s: 得到 %v, 期望 %v", c.name, got, c.want)
		}
	}
}

// TestACMarkString 确认标记字符与 PHP 的 label 文本一致。
func TestACMarkString(t *testing.T) {
	if markNone.String() != "" {
		t.Errorf("markNone 应为空串, 得到 %q", markNone.String())
	}
	if markAC.String() != "Y" || markBad.String() != "N" || markAsked.String() != "?" {
		t.Error("标记字符应为 Y / N / ?")
	}
}

// TestMaskAccepted 覆盖 contest.php:126 的打码条件。
//
// 重点验证 source_browser 不豁免打码：PHP 里它只在 :67-71 豁免 $noip。
func TestMaskAccepted(t *testing.T) {
	cases := []struct {
		name string
		f    contestProblemFlags
		want bool
	}{
		{"平时不锁", contestProblemFlags{}, false},
		{"NOIP 打码", contestProblemFlags{NOIP: true}, true},
		{"封榜打码", contestProblemFlags{HideOthers: true}, true},
		{"管理员不被打码", contestProblemFlags{NOIP: true, IsManager: true}, false},
		{"限时赛管理员不被打码", contestProblemFlags{HideOthers: true, IsManager: true}, false},
	}
	for _, c := range cases {
		if got := c.f.maskAccepted(); got != c.want {
			t.Errorf("%s: maskAccepted = %v, 期望 %v", c.name, got, c.want)
		}
	}
}

// TestCoinStars 复刻 contest.php:95 的 min(coin, 5)。
func TestCoinStars(t *testing.T) {
	cases := []struct {
		coin uint32
		want string
	}{
		{0, ""}, {1, "✦"}, {3, "✦✦✦"}, {5, "✦✦✦✦✦"},
		{6, "✦✦✦✦✦"}, {99, "✦✦✦✦✦"},
	}
	for _, c := range cases {
		if got := coinStars(c.coin); got != c.want {
			t.Errorf("coinStars(%d) = %q, 期望 %q", c.coin, got, c.want)
		}
	}
}

// TestContestTitle 覆盖 contest.php:242-243 的空标题回落。
func TestContestTitle(t *testing.T) {
	if got := contestTitle("  ", 1000); got != "比赛1000" {
		t.Errorf("空标题应回落为 比赛1000, 得到 %q", got)
	}
	if got := contestTitle(" 第一场 ", 1); got != "第一场" {
		t.Errorf("标题应去掉首尾空白, 得到 %q", got)
	}
}

// TestContestTotalPage 复刻 contest.php:154 的 intval($total / 25) + 1。
//
// 总数恰为 25 的倍数时会多算一页空列表，属 PHP 既有表现。
func TestContestTotalPage(t *testing.T) {
	cases := []struct {
		total int64
		want  int
	}{
		{0, 1}, {1, 1}, {24, 1}, {25, 2}, {26, 2}, {50, 3}, {141, 6},
	}
	for _, c := range cases {
		if got := int(c.total/contestSetPageSize) + 1; got != c.want {
			t.Errorf("total=%d: 总页数 = %d, 期望 %d", c.total, got, c.want)
		}
	}
}

// TestContestPageWindow 覆盖页码条窗口，含页码越界。
//
// ?page=9999 时 PHP 的 for 循环一次都不执行；
// 早先的写法在这里算出负数 cap 并 panic，这里锁住该边界。
func TestContestPageWindow(t *testing.T) {
	cases := []struct {
		name            string
		page, totalPage int
		want            []int
	}{
		{"第 1 页", 1, 1, []int{1}},
		{"首页不越下界", 1, 20, []int{1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{"中间页双向取 8", 10, 20, []int{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18}},
		{"末页不越上界", 20, 20, []int{12, 13, 14, 15, 16, 17, 18, 19, 20}},
		{"页码远超总页数", 9999, 1, nil},
		{"无比赛", 1, 0, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := contestPageWindow(c.page, c.totalPage)
			if len(got) != len(c.want) {
				t.Fatalf("页码 = %v, 期望 %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("页码 = %v, 期望 %v", got, c.want)
				}
			}
		})
	}
}
