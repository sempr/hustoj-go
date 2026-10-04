package web

import (
	"strings"
	"testing"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// 排名模板必须在真实数据下渲染通过。
//
// 这条守着一个已经踩过的坑：模板里的未导出方法 / {{range}} 内重绑定 . 都
// 要到渲染期才炸，编译期和 SQL 层都测不出来，页面直接 500。
func TestRenderContestRankTemplates(t *testing.T) {
	problems := []dbgen.GetContestProblemsForRankRow{
		{Num: 0, ProblemID: 1019},
		{Num: 1, ProblemID: 1020},
	}
	teams := []*contestTeam{
		newContestTeam("alice", "Alice"),
		newContestTeam("bob", "Bob"),
	}
	// alice：A 题 2 次错过后通过；B 题未通过但错了 1 次。
	teams[0].AddACM(0, 600, 6, false)
	teams[0].AddACM(0, 7, 6, false)
	teams[0].AddACM(0, 900, 4, false)
	teams[0].AddACM(1, 1200, 6, false)
	// bob：干净通过 B 题。
	teams[1].AddACM(1, 300, 4, false)
	sortTeams(teams, rankACM)

	fb := map[int]string{1: "bob"}
	for _, mode := range []rankMode{rankACM, rankOI} {
		view := newContestRankView(1000, "测试比赛", mode, problems, teams, fb, nil)
		view.ViewUser = "alice"

		name := "contestRank"
		if mode == rankOI {
			name = "contestRankOI"
		}
		var buf strings.Builder
		if err := pageTemplates.ExecuteTemplate(&buf, name, view); err != nil {
			t.Fatalf("%s 渲染失败: %v", name, err)
		}
		out := buf.String()
		for _, want := range []string{"alice", "bob", "Alice", "1000", "测试比赛"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s 输出缺少 %q", name, want)
			}
		}
		// OI 表头必须有总分列，ACM 不该有。
		if mode == rankOI && !strings.Contains(out, "总分") {
			t.Error("OI 表头缺少总分列")
		}
		if mode == rankACM && strings.Contains(out, "总分") {
			t.Error("ACM 表头不应有总分列")
		}
	}
}

// ── ACM 计分 ────────────────────────────────────────────────────────────

// 经典 ACM 罚时：每题通过时刻加上该题此前每次错误提交的 20 分钟。
func TestAddACMPenaltyMath(t *testing.T) {
	tt := newContestTeam("u", "n")
	tt.AddACM(0, 100, 6, false) // 错
	tt.AddACM(0, 200, 6, false) // 错
	tt.AddACM(0, 300, 4, false) // 对

	if tt.Solved != 1 {
		t.Errorf("Solved = %d，期望 1", tt.Solved)
	}
	// 300 + 2*1200 = 2700
	if tt.Time != 2700 {
		t.Errorf("Time = %d，期望 2700（通过 300 秒 + 2 次错误 × 1200）", tt.Time)
	}
	if got := sec2str(tt.Time); got != "00:45:00" {
		t.Errorf("sec2str = %q，期望 00:45:00", got)
	}
}

// 通过后再提交同题一律忽略，不能重复计分。
func TestAddACMIgnoresSubmissionsAfterSolve(t *testing.T) {
	tt := newContestTeam("u", "n")
	tt.AddACM(0, 300, 4, false)
	tt.AddACM(0, 200, 6, false) // 更早的错误提交
	tt.AddACM(0, 400, 4, false)

	if tt.Solved != 1 {
		t.Errorf("Solved = %d，期望 1（AC 后不应重复计数）", tt.Solved)
	}
	if tt.Time != 300 {
		t.Errorf("Time = %d，期望 300（AC 后的提交不应影响罚时）", tt.Time)
	}
	if tt.WANum[0] != 0 {
		t.Errorf("WA = %d，期望 0（AC 后的错误提交不计）", tt.WANum[0])
	}
}

// sec < 0 的提交要整条丢弃：续赛比赛里旧提交的时间会算成负数。
func TestAddACMIgnoresNegativeTime(t *testing.T) {
	tt := newContestTeam("u", "n")
	tt.AddACM(0, -500, 4, false)
	tt.AddACM(0, -100, 6, false)

	if tt.Solved != 0 || len(tt.WANum) != 0 {
		t.Errorf("负时间提交应被整体丢弃，实际 solved=%d wa=%v", tt.Solved, tt.WANum)
	}
}

// 编译错误（result=11）是否计罚时，由 $OJ_CE_PENALTY 决定。
// 线上 db_info.inc.php:36 是 false —— CE 直接丢弃，既不计错误次数也不计罚时。
func TestAddACMCompileErrorFollowsCEPenalty(t *testing.T) {
	off := newContestTeam("u", "n")
	off.AddACM(0, 100, 11, false)
	off.AddACM(0, 300, 4, false)

	if off.WANum[0] != 0 {
		t.Errorf("关闭 CE 罚时时 WA = %d，期望 0（CE 应被忽略）", off.WANum[0])
	}
	if off.Time != 300 {
		t.Errorf("关闭 CE 罚时时 Time = %d，期望 300（无罚时）", off.Time)
	}

	on := newContestTeam("u", "n")
	on.AddACM(0, 100, 11, true)
	on.AddACM(0, 300, 4, true)

	if on.WANum[0] != 1 {
		t.Errorf("开启 CE 罚时时 WA = %d，期望 1", on.WANum[0])
	}
	if on.Time != 300+penaltyPerWA {
		t.Errorf("开启 CE 罚时时 Time = %d，期望 %d", on.Time, 300+penaltyPerWA)
	}
}

// ACM 排序：解题数降序，其次罚时升序。
func TestSortACMTeams(t *testing.T) {
	mk := func(id string, solved, tm int) *contestTeam {
		t := newContestTeam(id, id)
		t.Solved, t.Time = solved, tm
		return t
	}
	teams := []*contestTeam{
		mk("c", 1, 100),
		mk("a", 3, 900),
		mk("b", 3, 300),
	}
	sortTeams(teams, rankACM)

	got := []string{teams[0].UserID, teams[1].UserID, teams[2].UserID}
	want := []string{"b", "a", "c"} // 同为 3 题，罚时小的在前
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("排序 = %v，期望 %v", got, want)
		}
	}
}

// 完全同分时要给出确定顺序，不能依赖 map 遍历或排序不稳定性。
func TestSortTeamsIsDeterministicOnFullTie(t *testing.T) {
	build := func() []*contestTeam {
		return []*contestTeam{
			newContestTeam("zoe", "z"),
			newContestTeam("amy", "a"),
			newContestTeam("max", "m"),
		}
	}
	first := build()
	sortTeams(first, rankACM)
	for i := 0; i < 20; i++ {
		again := build()
		sortTeams(again, rankACM)
		for j := range first {
			if first[j].UserID != again[j].UserID {
				t.Fatalf("同分排序不确定：第 %d 次第 %d 位 %q vs %q",
					i, j, first[j].UserID, again[j].UserID)
			}
		}
	}
}

// ── OI 计分 ────────────────────────────────────────────────────────────

// OI 分数 = 每题最优 pass_rate × 100 之和：通过记 100，未通过取最大
// pass_rate（调用方封顶 0.95 → 95）。总分高者排名靠前。
func TestAddOIScoring(t *testing.T) {
	tt := newContestTeam("u", "n")
	tt.AddOI(0, 100, 0.5, 6) // 未通过，50 分
	tt.AddOI(0, 200, 0.3, 6) // 更差的成绩，不该拉低已有分数
	tt.AddOI(1, 300, 0.0, 0) // 封榜提交，0 分
	tt.AddOI(1, 400, 1.0, 4) // 通过该题 → 100 分，替换 0 分

	if tt.Solved != 1 {
		t.Errorf("Solved = %d，期望 1", tt.Solved)
	}
	if tt.Total != 150 {
		t.Errorf("Total = %v，期望 150（题0 50 分 + 题1 100 分）", tt.Total)
	}
	// 通过题1 时把之前记的 0 分减掉，再加 100。
	if got := tt.PassRate[1]; got != 1.0 {
		t.Errorf("题1 PassRate = %v，期望 1.0", got)
	}
}

// OI 在通过前会持续把 pass_rate 往上抬（PHP 取最大值，不是最小值）。
func TestAddOIKeepsMaxPassRate(t *testing.T) {
	tt := newContestTeam("u", "n")
	tt.AddOI(0, 100, 0.30, 6)
	tt.AddOI(0, 200, 0.80, 6)

	if tt.PassRate[0] != 0.80 {
		t.Errorf("PassRate = %v，期望 0.80（应取最大值）", tt.PassRate[0])
	}
	if tt.Total != 80 {
		t.Errorf("Total = %v，期望 80", tt.Total)
	}
	if tt.WANum[0] != 2 {
		t.Errorf("WA = %d，期望 2", tt.WANum[0])
	}
}

// OI 通过后同样忽略后续提交。
func TestAddOIIgnoresSubmissionsAfterSolve(t *testing.T) {
	tt := newContestTeam("u", "n")
	tt.AddOI(0, 100, 1.0, 4)
	tt.AddOI(0, 200, 0.2, 6)

	if tt.Total != 100 {
		t.Errorf("Total = %v，期望 100（AC 后的提交应忽略）", tt.Total)
	}
	if tt.Solved != 1 {
		t.Errorf("Solved = %d，期望 1", tt.Solved)
	}
}

// OI 排序：总分降序 → 解题数降序 → 罚时升序。
func TestSortOITeams(t *testing.T) {
	mk := func(id string, total float64, solved, tm int) *contestTeam {
		t := newContestTeam(id, id)
		t.Total, t.Solved, t.Time = total, solved, tm
		return t
	}
	teams := []*contestTeam{
		mk("low", 100, 1, 10),
		mk("tieB", 300, 2, 500),
		mk("tieA", 300, 2, 100),
		mk("high", 400, 1, 999),
	}
	sortTeams(teams, rankOI)

	want := []string{"high", "tieA", "tieB", "low"}
	for i := range want {
		if teams[i].UserID != want[i] {
			t.Fatalf("OI 排序 = %v，期望 %v",
				[]string{teams[0].UserID, teams[1].UserID, teams[2].UserID, teams[3].UserID}, want)
		}
	}
}

// 把影子库 990006 号合成比赛的提交序列喂进来，结果与 contestrank-oi.php
// 的实跑输出逐字段一致（total=295 / solved=2 / time=5160 / wa=2,1,1）。
//
//	A 题 WA 0.50 → WA 0.80 → AC → AC 之后的 WA 0.99 必须被忽略
//	B 题 WA 0.30 → AC      （部分分要退掉再记满分）
//	C 题 WA 0.97           （未通过，封顶 0.95 → 95 分）
func TestAddOIMixedPassRateMatchesPHP(t *testing.T) {
	tt := newContestTeam("pass", "pass")
	type sub struct {
		pid      int
		sec      int
		passRate float64
		result   int32
	}
	for _, s := range []sub{
		{0, 600, 0.50, 6},
		{0, 480, 0.80, 6},
		{0, 840, 1.00, 4},
		{0, 900, 0.99, 6}, // AC 之后，忽略
		{1, 600, 0.30, 6},
		{1, 720, 1.00, 4},
		{2, 540, 0.97, 6}, // 封顶 0.95
	} {
		// 封顶发生在读 submissions 的那一层（contestrank-oi.php:184-186），
		// 不在 AddOI 里，这里按线上同样的顺序先封顶再累加。
		rate := s.passRate
		if s.result == 4 {
			rate = 1.0
		} else if rate >= 0.95 {
			rate = 0.95
		}
		tt.AddOI(s.pid, s.sec, rate, s.result)
	}

	if tt.Solved != 2 {
		t.Errorf("Solved = %d，期望 2", tt.Solved)
	}
	if tt.Total != 295 {
		t.Errorf("Total = %v，期望 295（题0 100 + 题1 100 + 题2 95）", tt.Total)
	}
	if tt.Time != 5160 {
		t.Errorf("Time = %d，期望 5160", tt.Time)
	}
	if tt.WANum[0] != 2 || tt.WANum[1] != 1 || tt.WANum[2] != 1 {
		t.Errorf("WA = %v，期望 [2 1 1]", tt.WANum)
	}
	if tt.PassRate[2] != 0.95 {
		t.Errorf("题2 PassRate = %v，期望 0.95（0.97 应被封顶）", tt.PassRate[2])
	}
}

// ── 工具函数 ────────────────────────────────────────────────────────────

// 题号字母要复刻 PHP 的 $PID：A..Z 之后是 AA、AB……（Excel 列名式）。
func TestContestProblemLetter(t *testing.T) {
	cases := map[int]string{
		0: "A", 1: "B", 25: "Z", 26: "AA", 27: "AB", 51: "AZ", 52: "BA", 701: "ZZ", 702: "AAA",
	}
	for num, want := range cases {
		if got := contestProblemLetter(num); got != want {
			t.Errorf("contestProblemLetter(%d) = %q，期望 %q", num, got, want)
		}
	}
	if got := contestProblemLetter(-1); got != "" {
		t.Errorf("负题号应返回空串，实际 %q", got)
	}
}

// sec2str 必须与 PHP 的 sprintf("%02d:%02d:%02d", sec/3600, sec%3600/60, sec%60) 一致。
func TestSec2Str(t *testing.T) {
	cases := map[int]string{
		0:     "00:00:00",
		59:    "00:00:59",
		60:    "00:01:00",
		3599:  "00:59:59",
		3600:  "01:00:00",
		3661:  "01:01:01",
		86399: "23:59:59",
		90000: "25:00:00", // 超过 24 小时不环绕
	}
	for sec, want := range cases {
		if got := sec2str(sec); got != want {
			t.Errorf("sec2str(%d) = %q，期望 %q", sec, got, want)
		}
	}
	if got := sec2str(-5); got != "00:00:00" {
		t.Errorf("负秒数应归零，实际 %q", got)
	}
}

// ── 视图模型 ────────────────────────────────────────────────────────────

// 缺席选手必须排在末尾，且不能覆盖最后一名真实选手。
//
// PHP 在这里有个 off-by-one：主循环里 $user_cnt 已自增，之后又写
// $U[$user_cnt] = new TM()，第一个缺席者会盖掉最后一名真实选手。
func TestAbsentUsersDoNotOverwriteLastRank(t *testing.T) {
	problems := []dbgen.GetContestProblemsForRankRow{{Num: 0, ProblemID: 1}}

	real := newContestTeam("solver", "Solver")
	real.AddACM(0, 60, 4, false)
	absent := newContestTeam("ghost", "Ghost") // 零提交

	teams := []*contestTeam{real, absent}
	sortTeams(teams, rankACM)

	view := newContestRankView(1000, "t", rankACM, problems, teams, nil,
		[]absentUser{{UserID: "ghost", Nick: "Ghost"}})

	if len(view.Rows) != 2 {
		t.Fatalf("行数 = %d，期望 2", len(view.Rows))
	}
	if view.Rows[0].UserID != "solver" || view.Rows[0].Rank != 1 {
		t.Errorf("第一行应为 solver(rank=1)，实际 %s(rank=%d)", view.Rows[0].UserID, view.Rows[0].Rank)
	}
	if view.Rows[0].Solved != 1 {
		t.Errorf("solver 的解题数被覆盖成 %d，期望 1", view.Rows[0].Solved)
	}
	if view.Rows[1].UserID != "ghost" {
		t.Errorf("第二行应为缺席的 ghost，实际 %s", view.Rows[1].UserID)
	}
	// PHP 把缺席名单追加进 $U 后统一渲染，缺席选手照常拿名次；
	// 他们只是额外出现在页面底部的缺席区块。
	if view.Rows[1].Rank != 2 {
		t.Errorf("缺席选手名次 = %d，期望 2（PHP 照常排名次）", view.Rows[1].Rank)
	}
	if len(view.Absent) != 1 || view.Absent[0].UserID != "ghost" {
		t.Errorf("底部缺席名单 = %v，期望 [ghost]", view.Absent)
	}
}

// 回归：$OJ_CE_PENALTY=false 时只提交过 CE 的真实选手，解题数/错误数/AC
// 全为 0，形状上和"缺席"一模一样。他必须照常排名次，不能被当成缺席。
// 线上 contest_id=1000 尾部有 20 个这种选手，一度被误判导致名次少算 20 个。
func TestZeroScoringRealSolverIsNotAbsent(t *testing.T) {
	problems := []dbgen.GetContestProblemsForRankRow{{Num: 0, ProblemID: 1}}

	ceOnly := newContestTeam("ceonly", "CEonly")
	ceOnly.AddACM(0, 60, 11, false) // CE，不计罚时
	solver := newContestTeam("solver", "Solver")
	solver.AddACM(0, 120, 4, false)

	teams := []*contestTeam{ceOnly, solver}
	sortTeams(teams, rankACM)

	// 缺席名单（privilege 表）里没有 ceonly，他就是个零分的真实选手。
	view := newContestRankView(1000, "t", rankACM, problems, teams, nil, nil)

	if len(view.Rows) != 2 {
		t.Fatalf("行数 = %d，期望 2", len(view.Rows))
	}
	if view.Rows[0].UserID != "solver" || view.Rows[0].Rank != 1 {
		t.Fatalf("第一行应为 solver(rank=1)，实际 %s(rank=%d)",
			view.Rows[0].UserID, view.Rows[0].Rank)
	}
	// 关键：solver 解题数更高所以排前面；ceonly 排在后面但必须有名次，
	// 不能因为"零解题 + 零错误 + 零 AC"被误判成缺席而显示 *。
	got := view.Rows[1]
	if got.UserID != "ceonly" {
		t.Fatalf("第二行应为 ceonly，实际 %s", got.UserID)
	}
	if got.Rank != 2 {
		t.Errorf("只提交过 CE 的选手 rank = %d，期望 2（不能被当成缺席）", got.Rank)
	}
	if len(view.Absent) != 0 {
		t.Errorf("缺席名单应为空，实际 %v", view.Absent)
	}
}

// 首杀角标只给该题最早 AC 的人。
func TestFirstBloodMarking(t *testing.T) {
	problems := []dbgen.GetContestProblemsForRankRow{{Num: 0, ProblemID: 1}}

	early := newContestTeam("early", "Early")
	early.AddACM(0, 60, 4, false)
	late := newContestTeam("late", "Late")
	late.AddACM(0, 900, 4, false)

	fb := map[int]string{0: "early"}
	view := newContestRankView(1000, "t", rankACM, problems,
		[]*contestTeam{late, early}, fb, nil)

	byUser := map[string]rankRow{}
	for _, r := range view.Rows {
		byUser[r.UserID] = r
	}
	if !byUser["early"].Cells[0].First {
		t.Error("early 应标记为首杀")
	}
	if byUser["late"].Cells[0].First {
		t.Error("late 不应被标为首杀")
	}
}

// 昵称以 * 开头的选手不参与排名（与 PHP 的 $nick[0]!="*" 一致）。
func TestStarNickSkipsRankNumber(t *testing.T) {
	problems := []dbgen.GetContestProblemsForRankRow{{Num: 0, ProblemID: 1}}
	a := newContestTeam("a", "*Guest")
	a.AddACM(0, 60, 4, false)
	b := newContestTeam("b", "Bob")
	b.AddACM(0, 90, 4, false)

	view := newContestRankView(1000, "t", rankACM, problems, []*contestTeam{a, b}, nil, nil)
	for _, r := range view.Rows {
		if r.UserID == "a" && r.Rank != 0 {
			t.Errorf("以 * 开头的昵称不应有名次，实际 rank=%d", r.Rank)
		}
	}
}

// 题数不足时会补出空格子，不能少列。
func TestRankViewPadsMissingProblems(t *testing.T) {
	// contest_problem 只有 num=0，但视图要按最大 num+1 补齐。
	problems := []dbgen.GetContestProblemsForRankRow{{Num: 0, ProblemID: 1}}
	tt := newContestTeam("u", "n")
	tt.AddACM(0, 60, 4, false)

	view := newContestRankView(1000, "t", rankACM, problems, []*contestTeam{tt}, nil, nil)
	if len(view.Letters) != 1 {
		t.Errorf("题号数 = %d，期望 1", len(view.Letters))
	}
	if got := view.Rows[0].Cells[0].ACS; got != "00:01:00" {
		t.Errorf("通过时刻 = %q，期望 00:01:00", got)
	}
}

// 通过时刻为 0（开赛瞬间 AC）在 PHP 里不算已通过，视图也不能算。
func TestRankViewZeroAClSecIsNotSolved(t *testing.T) {
	problems := []dbgen.GetContestProblemsForRankRow{{Num: 0, ProblemID: 1}}
	tt := newContestTeam("u", "n")
	tt.ACSec[0] = 0 // 直接塞 0，绕过 Add 的路径

	view := newContestRankView(1000, "t", rankACM, problems, []*contestTeam{tt}, nil, nil)
	if view.Rows[0].Cells[0].Solved {
		t.Error("通过时刻为 0 不应显示为已通过")
	}
}

// 底色随错题数加深，两套赛制的公式不同（OI 的绿色通道封顶 230/130）。
func TestRankCellColors(t *testing.T) {
	if got := acmAccentStyle(0); got != "rgb(150, 255, 150)" {
		t.Errorf("acmAccentStyle(0) = %q", got)
	}
	if got := oiAccentStyle(10); got != "rgb(270, 255, 230)" {
		t.Errorf("oiAccentStyle(10) = %q（第三通道应封顶 230）", got)
	}
	if got := oiFailStyle(20); got != "rgb(255, 130, 130)" {
		t.Errorf("oiFailStyle(20) = %q（红绿通道应保底 130）", got)
	}
	if got := acmFailStyle(1); got != "rgb(255, 231, 231)" {
		t.Errorf("acmFailStyle(1) = %q", got)
	}
}

// OI 得分渲染：整数不带小数，非四舍五入到整数。
func TestFormatPassRate(t *testing.T) {
	cases := map[float64]string{1.0: "100", 0.95: "95", 0.5: "50", 0.123: "12.3"}
	for rate, want := range cases {
		if got := formatPassRate(rate); got != want {
			t.Errorf("formatPassRate(%v) = %q，期望 %q", rate, got, want)
		}
	}
	if got := formatOITotal(newContestTeam("u", "n"), rankACM); got != "" {
		t.Errorf("ACM 模式不应有总分，实际 %q", got)
	}
}

// PHP 的 php.ini precision=14（等价 %.14G）会把浮点噪声藏起来，
// strconv 的最短往返表示则会把 0.56*100 == 56.00000000000001 原样吐出来。
func TestPHPFloatMatchesPrecision14(t *testing.T) {
	cases := map[float64]string{
		0.56 * 100:            "56", // 实际是 56.00000000000001
		0.29 * 100:            "29",
		0.55 * 100:            "55",
		0.95 * 100:            "95",
		1.0 * 100:             "100",
		0.123 * 100:           "12.3", // 真实有效位要保留
		(0.56*100 + 0.75*100): "131",
		(0.56*100 + 0.30*100): "86",
		(0.56*100 + 0.95*100): "151",
		(0.01*100 + 0.02*100): "3", // 0.30000000000000004
	}
	for f, want := range cases {
		if got := formatPassRate(f / 100); got != want {
			t.Errorf("formatPassRate(%v/100) = %q，期望 %q", f, got, want)
		}
	}
	if got := phpFloat(0); got != "0" {
		t.Errorf("phpFloat(0) = %q，期望 \"0\"", got)
	}
}
