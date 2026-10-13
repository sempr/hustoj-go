// 状态页（/status.php）的参数解析与封榜逻辑测试。
//
// 这些用例针对的是真实回归：曾经 parseInt("") 返回 0 而 buildWhere 用
// `f.Language >= 0` 判断，导致不带任何参数的状态页被静默筛成
// "只看 language=0 且 result=0 的提交"。参数缺省必须与"显式传 0"区分开。

package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// 不带 language/jresult 参数时，两个筛选都必须处于"不过滤"状态。
//
// PHP 用 isset($_GET[...]) 判断（status.php:156、:170），没传就是不过滤。
// 若这里退回 0，未筛选的状态页会只剩"等待中"的 C 提交，几乎永远是空的。
func TestParseStatusFilterAbsentMeansUnfiltered(t *testing.T) {
	f := parseStatusFilter(httptest.NewRequest(http.MethodGet, "/status.php", nil))

	if f.Language != -1 {
		t.Errorf("缺省 Language = %d，期望 -1（不过滤）", f.Language)
	}
	if f.Result != -1 {
		t.Errorf("缺省 Result = %d，期望 -1（不过滤）", f.Result)
	}
}

// 显式传 0 是合法筛选：language=0 是"只看 C"，jresult=0 是"只看等待中"。
// 这是"缺省"与"传 0"必须区分开的根本原因。
func TestParseStatusFilterExplicitZeroIsAFilter(t *testing.T) {
	f := parseStatusFilter(httptest.NewRequest(http.MethodGet, "/status.php?language=0&jresult=0", nil))

	if f.Language != 0 {
		t.Errorf("显式 language=0 解析为 %d，期望 0（作为筛选生效）", f.Language)
	}
	if f.Result != 0 {
		t.Errorf("显式 jresult=0 解析为 %d，期望 0（作为筛选生效）", f.Result)
	}
}

// language=0 必须真的进入 WHERE 子句，且只出现一次该条件。
func TestBuildWhereAppliesExplicitLanguageZero(t *testing.T) {
	s := newTestServer(t, &fakeQuerier{}, nil)
	f := parseStatusFilter(httptest.NewRequest(http.MethodGet, "/status.php?language=0&jresult=0", nil))

	where, args := s.buildWhere(f, Anonymous())

	if !strings.Contains(where, "s.language = ?") {
		t.Errorf("显式 language=0 应生成语言条件，实际: %s", where)
	}
	if !strings.Contains(where, "s.result = ?") {
		t.Errorf("显式 jresult=0 应生成结果条件，实际: %s", where)
	}
	// 条件各带一个参数，不能多也不能少。状态页不再加 rank-hidden 排除项，
	// 因此参数只有两个筛选值，顺序必须与 WHERE 子句一致。
	want := []interface{}{int32(0), int32(0)}
	if len(args) != len(want) {
		t.Fatalf("参数个数 = %d，期望 %d，实际 args=%v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("args[%d] = %v，期望 %v（顺序必须匹配占位符）", i, args[i], want[i])
		}
	}
}

// 未传 language/jresult 时不应生成对应条件 —— 这正是曾经把整页筛空的 bug。
func TestBuildWhereOmitsAbsentFilters(t *testing.T) {
	s := newTestServer(t, &fakeQuerier{}, nil)
	f := parseStatusFilter(httptest.NewRequest(http.MethodGet, "/status.php", nil))

	where, _ := s.buildWhere(f, Anonymous())

	if strings.Contains(where, "s.language = ?") {
		t.Errorf("未传 language 时不该生成语言条件: %s", where)
	}
	if strings.Contains(where, "s.result = ?") {
		t.Errorf("未传 jresult 时不该生成结果条件: %s", where)
	}
}

// language 超出上界按 -1 处理（status.php:158 的 count($language_ext) = 26）。
func TestParseStatusFilterRejectsOutOfRangeLanguage(t *testing.T) {
	for _, q := range []string{"language=27", "language=999", "language=-1"} {
		f := parseStatusFilter(httptest.NewRequest(http.MethodGet, "/status.php?"+q, nil))
		if f.Language != -1 {
			t.Errorf("%s 解析为 %d，期望 -1（越界即不过滤）", q, f.Language)
		}
	}
	// 边界值 26 仍然有效。
	f := parseStatusFilter(httptest.NewRequest(http.MethodGet, "/status.php?language=26", nil))
	if f.Language != 26 {
		t.Errorf("language=26 解析为 %d，期望 26（上界含端点）", f.Language)
	}
}

// jresult=-1 是 PHP 的保留值，表示"不按结果过滤"。
func TestParseStatusFilterJResultMinusOneIsUnfiltered(t *testing.T) {
	f := parseStatusFilter(httptest.NewRequest(http.MethodGet, "/status.php?jresult=-1", nil))
	if f.Result != -1 {
		t.Errorf("jresult=-1 解析为 %d，期望 -1", f.Result)
	}
}

// 状态页**不再**排除 rank-hidden 账号：admin 等账号的提交一律可见，
// 以保证 rank-hidden 账号（如 admin）本人登录后能看到自己的提交。
func TestBuildWhereAnonymousDoesNotExcludeRankHiddenUsers(t *testing.T) {
	s := newTestServer(t, &fakeQuerier{}, nil)
	where, args := s.buildWhere(StatusFilter{}, Anonymous())

	if strings.Contains(where, "NOT IN") {
		t.Errorf("状态页不应再排除 rank-hidden 账号: %s", where)
	}
	for _, a := range args {
		if str, ok := a.(string); ok && (str == "admin" || str == "zhblue") {
			t.Errorf("不应把 rank-hidden 账号作为排除参数: %v", args)
		}
	}
}

// 游客不能看比赛提交，只能看练习（contest_id 为 0/NULL）。
func TestBuildWhereAnonymousSeesPracticeOnly(t *testing.T) {
	s := newTestServer(t, &fakeQuerier{}, nil)
	where, _ := s.buildWhere(StatusFilter{}, Anonymous())

	if !strings.Contains(where, "s.contest_id = 0") {
		t.Errorf("游客应只看到练习提交: %s", where)
	}
}

// 代码审查员可见全站，不应被练习限制挡住。
func TestBuildWhereSourceBrowserSeesEverything(t *testing.T) {
	s := newTestServer(t, &fakeQuerier{}, nil)
	id := Identity{UserID: "auditor", SourceBrowser: true, LoggedIn: true}
	where, _ := s.buildWhere(StatusFilter{}, id)

	if strings.Contains(where, "s.contest_id = 0") {
		t.Errorf("代码审查员不应受练习范围限制: %s", where)
	}
	if strings.Contains(where, "NOT IN") {
		t.Errorf("代码审查员不应被 rank-hidden 过滤: %s", where)
	}
}

// ── 封榜 ──────────────────────────────────────────────────────────────

// 构造一场给定起点/终点的比赛。
func rankTestContest(start, end time.Time, contestType int16) dbgen.GetContestForRankRow {
	return dbgen.GetContestForRankRow{
		ContestID:   1000,
		Title:       sql.NullString{String: "测试比赛", Valid: true},
		StartTime:   sql.NullTime{Time: start, Valid: true},
		EndTime:     sql.NullTime{Time: end, Valid: true},
		ContestType: sql.NullInt16{Int16: contestType, Valid: true},
	}
}

// 默认 0（PHP 的 $OJ_RANK_LOCK_PERCENT 默认值）表示从不封榜。
func TestContestLockWindowDisabledByDefault(t *testing.T) {
	now := time.Now()
	c := rankTestContest(now.Add(-2*time.Hour), now.Add(time.Hour), 1)

	if _, active := contestLockWindow(&c, now, 0); active {
		t.Error("percent=0 时不应封榜（PHP 默认不封榜）")
	}
}

// percent=0.2 的 5 小时比赛，最后 1 小时封榜。
func TestContestLockWindowInsideLockPeriod(t *testing.T) {
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Hour)
	c := rankTestContest(start, end, 1)

	// 封榜时刻 = 14:00。14:30 应处于封榜期。
	lockTime, active := contestLockWindow(&c, start.Add(4*time.Hour+30*time.Minute), 0.2)
	if !active {
		t.Fatal("14:30 应处于封榜期")
	}
	if !lockTime.Equal(start.Add(4 * time.Hour)) {
		t.Errorf("封榜时刻 = %v，期望 %v", lockTime, start.Add(4*time.Hour))
	}

	// 13:59 还没封榜。
	if _, active := contestLockWindow(&c, start.Add(3*time.Hour+59*time.Minute), 0.2); active {
		t.Error("13:59 不应处于封榜期")
	}
	// 比赛结束后不再封榜。
	if _, active := contestLockWindow(&c, end.Add(time.Hour), 0.2); active {
		t.Error("比赛结束后不应处于封榜期")
	}
}

// 封榜期内，封榜前提交的他人记录被隐藏。
func TestApplyRankLockMaskingHidesOthersOldSubmissions(t *testing.T) {
	lockTime := time.Date(2026, 1, 1, 14, 0, 0, 0, time.UTC)
	rows := []StatusRow{
		// 封榜前提交，查看者不是本人 → 隐藏
		{SolutionID: 1, UserID: "alice", InDate: lockTime.Add(-time.Hour)},
		// 封榜前提交，查看者本人 → 不隐藏
		{SolutionID: 2, UserID: "bob", InDate: lockTime.Add(-time.Hour)},
		// 封榜后提交 → 不隐藏（此刻还没有结果）
		{SolutionID: 3, UserID: "alice", InDate: lockTime.Add(time.Minute)},
	}

	applyRankLockMasking(rows, lockTime, "bob")

	if !rows[0].Masked {
		t.Error("他人的封榜前提交应被隐藏")
	}
	if rows[1].Masked {
		t.Error("查看者本人的提交不应被隐藏")
	}
	if rows[2].Masked {
		t.Error("封榜后的提交不应被隐藏")
	}
}

// 封榜行的展示必须是 PHP 的 "----"，且不泄露结果与资源占用。
func TestMaskedRowHidesResultAndResources(t *testing.T) {
	row := StatusRow{Result: 4, Memory: 1024, Time: 250, Masked: true}

	if got := row.ResultLabel(); got != "----" {
		t.Errorf("封榜行结果文案 = %q，期望 %q", got, "----")
	}
	if got := row.ResultClass(); got != "" {
		t.Errorf("封榜行不应带状态色（会通过颜色泄露结果）: %q", got)
	}
	if got := row.MemoryText(); got != "" {
		t.Errorf("封榜行内存应隐藏，实际 %q", got)
	}
	if got := row.TimeText(); got != "" {
		t.Errorf("封榜行耗时应隐藏，实际 %q", got)
	}
}

// 未封榜行正常显示，且内存/耗时带单位。
func TestUnmaskedRowShowsResultAndResources(t *testing.T) {
	row := StatusRow{Result: 4, Memory: 1024, Time: 250}

	if got := row.ResultLabel(); got != "通过" {
		t.Errorf("未封榜行结果文案 = %q，期望 %q", got, "通过")
	}
	if got := row.MemoryText(); got != "1024 KiB" {
		t.Errorf("内存文案 = %q", got)
	}
	if got := row.TimeText(); got != "250 ms" {
		t.Errorf("耗时文案 = %q", got)
	}
}

// ── NOIP 锁榜 ─────────────────────────────────────────────────────────

// 标题含 noip 且比赛未结束 → NOIP 模式。
func TestIsNOIPContestDetectsKeyword(t *testing.T) {
	now := time.Now()
	c := rankTestContest(now.Add(-time.Hour), now.Add(time.Hour), 1)
	c.Title = sql.NullString{String: "NOIP 模拟赛", Valid: true}

	if !isNOIPContest(&c, "noip", now) {
		t.Error("标题含 noip 的进行中比赛应启用 NOIP 模式")
	}
	// 关键字匹配大小写不敏感（PHP 用 stripos）。
	c.Title = sql.NullString{String: "noip 模拟赛", Valid: true}
	if !isNOIPContest(&c, "noip", now) {
		t.Error("NOIP 关键字匹配应大小写不敏感")
	}
}

// contest_type 的 16 位同样触发 NOIP。
func TestIsNOIPContestDetectsTypeBit(t *testing.T) {
	now := time.Now()
	c := rankTestContest(now.Add(-time.Hour), now.Add(time.Hour), noipContestTypeBit)
	c.Title = sql.NullString{String: "普通标题", Valid: true}

	if !isNOIPContest(&c, "noip", now) {
		t.Error("contest_type & 16 的比赛应启用 NOIP 模式")
	}
}

// 比赛结束后 NOIP 失效。
func TestIsNOIPContestOffAfterEnd(t *testing.T) {
	now := time.Now()
	c := rankTestContest(now.Add(-2*time.Hour), now.Add(-time.Hour), noipContestTypeBit)

	if isNOIPContest(&c, "noip", now) {
		t.Error("已结束的比赛不应再启用 NOIP 模式")
	}
}

// 豁免身份：管理员、代码审查员、比赛创建者、限时赛持有人。
func TestStatusNOIPExempt(t *testing.T) {
	cases := []struct {
		name string
		id   Identity
		cid  int32
		want bool
	}{
		{"管理员", Identity{Admin: true}, 1000, true},
		{"代码审查员", Identity{SourceBrowser: true}, 1000, true},
		{"比赛创建者", Identity{ContestCreator: true}, 1000, true},
		{"限时赛持有人", Identity{ContestTimeLimitUsed: map[int32]bool{1000: true}}, 1000, true},
		{"他人限时赛不豁免", Identity{ContestTimeLimitUsed: map[int32]bool{2000: true}}, 1000, false},
		{"普通用户", Identity{UserID: "alice", LoggedIn: true}, 1000, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusNOIPExempt(tc.id, tc.cid); got != tc.want {
				t.Errorf("statusNOIPExempt = %v，期望 %v", got, tc.want)
			}
		})
	}
}

// ── 模板渲染 ──────────────────────────────────────────────────────────

// 状态页模板必须能真正渲染出表格行。
//
// 这条用例守着两个已经踩过的坑：
//   - statusPagination 的分页方法曾是小写（html/template 调不到未导出方法）；
//   - 语言/结果下拉框在 {{range}} 内部引用 .Filter，被重绑定后渲染直接 500。
//
// 两者都只在真实渲染时才暴露，编译期与 SQL 层面都测不出来。
func TestRenderStatusPageTemplate(t *testing.T) {
	rows := []StatusRow{{
		SolutionID: 9001, ProblemID: 100, UserID: "alice", Language: 1, Result: 4,
		InDate: time.Now(), Time: 12, Memory: 2048, Nick: "Alice",
		ContestTitle: sql.NullString{String: "练习", Valid: true},
	}}
	var buf strings.Builder
	if err := pageTemplates.ExecuteTemplate(&buf, "statusPage", newStatusPage(rows, StatusFilter{})); err != nil {
		t.Fatalf("渲染 statusPage 失败: %v", err)
	}
	out := buf.String()

	for _, want := range []string{"9001", "alice", "通过", "12 ms", "2048 KiB"} {
		if !strings.Contains(out, want) {
			t.Errorf("状态页缺少 %q\n输出:\n%s", want, out)
		}
	}
}

// 显式筛选值必须在下拉框里回显为 selected，否则用户提交一次筛选就
// 看不到当前条件，只能靠重新输入。
func TestRenderStatusPageEchoesFilters(t *testing.T) {
	p := newStatusPage(nil, StatusFilter{Language: 0, HasLanguage: true, Result: 4, HasResult: true})
	var buf strings.Builder
	if err := pageTemplates.ExecuteTemplate(&buf, "statusPage", p); err != nil {
		t.Fatalf("渲染 statusPage 失败: %v", err)
	}
	out := buf.String()

	if !strings.Contains(out, `value="0" selected`) {
		t.Errorf("language=0 应回显为选中\n输出:\n%s", out)
	}
	if !strings.Contains(out, `value="4" selected`) {
		t.Errorf("jresult=4 应回显为选中\n输出:\n%s", out)
	}
}

// keyset 游标必须取自实际结果：Top 是首行（最大 ID），Bottom 是末行。
func TestNewStatusPageDerivesKeysetCursors(t *testing.T) {
	rows := []StatusRow{
		{SolutionID: 105}, {SolutionID: 104}, {SolutionID: 100},
	}
	p := newStatusPage(rows, StatusFilter{})

	if p.Top != 105 {
		t.Errorf("Top = %d，期望 105（首行 solution_id）", p.Top)
	}
	if p.Bottom != 100 {
		t.Errorf("Bottom = %d，期望 100（末行 solution_id）", p.Bottom)
	}
	// 没有 prevtop 时，Prev 游标按 PHP 的 top + statusPageSize 推导。
	if want := int64(105) + statusPageSize; p.PrevTop != want {
		t.Errorf("PrevTop = %d，期望 %d", p.PrevTop, want)
	}
}

// 请求里带了 prevtop 时必须原样沿用，否则"下一页返回上一页"会跳错位置。
func TestNewStatusPageKeepsPrevTopFromRequest(t *testing.T) {
	p := newStatusPage([]StatusRow{{SolutionID: 105}}, StatusFilter{PrevTop: 900})
	if p.PrevTop != 900 {
		t.Errorf("PrevTop = %d，期望沿用请求里的 900", p.PrevTop)
	}
}

// 空结果时游标必须是 -1，不能是 0 —— 0 会被当作合法的 top 过滤掉所有数据。
func TestNewStatusPageEmptyRowsHasNegativeCursors(t *testing.T) {
	p := newStatusPage(nil, StatusFilter{})
	if p.Top != -1 || p.Bottom != -1 || p.PrevTop != -1 {
		t.Errorf("空结果的游标应为 -1，实际 Top=%d Bottom=%d PrevTop=%d", p.Top, p.Bottom, p.PrevTop)
	}
	if p.NextHref() != "" {
		t.Error("空结果时不应有 Next 链接")
	}
}

// 不满一页说明已到最新，没有 Next 链接；否则点 Next 只会重复当前页。
func TestNextHrefAbsentWhenPageNotFull(t *testing.T) {
	rows := make([]StatusRow, statusPageSize-1)
	for i := range rows {
		rows[i] = StatusRow{SolutionID: uint32(1000 - i)}
	}
	p := newStatusPage(rows, StatusFilter{})
	if p.NextHref() != "" {
		t.Error("未取满一页时不应给出 Next 链接（会重复当前页）")
	}

	full := make([]StatusRow, statusPageSize)
	for i := range full {
		full[i] = StatusRow{SolutionID: uint32(1000 - i)}
	}
	p2 := newStatusPage(full, StatusFilter{})
	if got := string(p2.NextHref()); !strings.Contains(got, "top=") {
		t.Errorf("取满一页时应给出 Next 链接，实际 %q", got)
	}
}

// StatusFilter 的零值必须等于"不过滤"。
//
// 这条用例守着一个已经真实发生过的坑：早先用 int32 的 0 当"未设置"的
// 哨兵，于是 StatusFilter{} 被理解成 language=0（C）。所有直接构造这个
// 结构体的代码（包括几乎全部单元测试）都会悄悄多出一个语言过滤条件，
// 而且编译期、SQL 层都看不出来 —— 只有对着 DB 核对行数才会暴露。
func TestStatusFilterZeroValueFiltersNothing(t *testing.T) {
	f := StatusFilter{}

	if f.HasLanguage || f.HasResult {
		t.Fatalf("零值不该标记为已指定：HasLanguage=%v HasResult=%v", f.HasLanguage, f.HasResult)
	}

	sqlStr, _ := (&Server{}).buildWhere(f, Anonymous())
	for _, bad := range []string{"s.language", "s.result"} {
		if strings.Contains(sqlStr, bad) {
			t.Errorf("零值筛选不应包含 %s 条件，实际 SQL: %s", bad, sqlStr)
		}
	}

	if got := string(statusHref(0, 0, f)); strings.Contains(got, "language") || strings.Contains(got, "jresult") {
		t.Errorf("零值筛选的分页链接不应带语言/结果参数，实际 %q", got)
	}

	if f.SelectedLanguage() != -1 || f.SelectedResult() != -1 {
		t.Errorf("零值不应回显任何选中项，实际 language=%d result=%d",
			f.SelectedLanguage(), f.SelectedResult())
	}
}

// language=0 / jresult=0 必须真正生效 —— 它们是合法筛选值，不是"未设置"。
func TestStatusFilterExplicitZeroIsHonoured(t *testing.T) {
	f := StatusFilter{Language: 0, HasLanguage: true, Result: 0, HasResult: true}

	sqlStr, args := (&Server{}).buildWhere(f, Anonymous())
	if !strings.Contains(sqlStr, "s.language = ?") || !strings.Contains(sqlStr, "s.result = ?") {
		t.Errorf("显式 0 应生成过滤条件，实际 SQL: %s", sqlStr)
	}
	var sawLang, sawRes bool
	for _, a := range args {
		if v, ok := a.(int32); ok {
			if v == 0 {
				sawLang, sawRes = true, true
			}
		}
	}
	if !sawLang || !sawRes {
		t.Errorf("参数里应出现两次 0（语言、结果），实际 %#v", args)
	}

	if f.SelectedLanguage() != 0 || f.SelectedResult() != 0 {
		t.Error("显式 0 应回显为选中")
	}
	if got := string(statusHref(0, 0, f)); !strings.Contains(got, "language=0") || !strings.Contains(got, "jresult=0") {
		t.Errorf("分页链接应保留显式 0，实际 %q", got)
	}
}

// 越界语言值要归一成"不过滤"，而不是当成语言 0。
func TestStatusFilterRejectsOutOfRangeLanguage(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/status.php?language=27", nil)
	f := parseStatusFilter(r)

	if f.HasLanguage {
		t.Errorf("language=27 超出 0..26，应视为未指定，实际 Language=%d", f.Language)
	}
	sqlStr, _ := (&Server{}).buildWhere(f, Anonymous())
	if strings.Contains(sqlStr, "s.language") {
		t.Errorf("越界语言不应生成条件，实际 SQL: %s", sqlStr)
	}
}

// PHP 用 isset() 区分缺省与显式 0（status.php:158/171），这条守住解析层。
func TestParseStatusFilterDistinguishesAbsentFromZero(t *testing.T) {
	cases := []struct {
		query                   string
		wantLang, wantResult    int32
		wantHasLang, wantHasRes bool
	}{
		{"/status.php", -1, -1, false, false},
		{"/status.php?language=0&jresult=0", 0, 0, true, true},
		{"/status.php?language=3", 3, -1, true, false},
		{"/status.php?jresult=4", -1, 4, false, true},
		{"/status.php?jresult=-1", -1, -1, false, false},
		{"/status.php?language=26", 26, -1, true, false},
		{"/status.php?language=27", -1, -1, false, false},
		// 空值参数是个真实的坑：PHP 用 isset() 判断"是否带了参数"，而
		// intval("") == 0，所以 ?language= 会被当成"筛语言 0（C）"。
		// Go 这边必须照抄，否则同一个 URL 两端返回不同行数。
		{"/status.php?language=&jresult=", 0, 0, true, true},
	}
	for _, c := range cases {
		f := parseStatusFilter(httptest.NewRequest(http.MethodGet, c.query, nil))
		if f.Language != c.wantLang || f.Result != c.wantResult ||
			f.HasLanguage != c.wantHasLang || f.HasResult != c.wantHasRes {
			t.Errorf("%s -> lang=%d(%v) result=%d(%v)，期望 lang=%d(%v) result=%d(%v)",
				c.query, f.Language, f.HasLanguage, f.Result, f.HasResult,
				c.wantLang, c.wantHasLang, c.wantResult, c.wantHasRes)
		}
	}
}

// top / prevtop 是 keyset 游标，不能和筛选参数混在一起被清掉。
func TestParseStatusFilterReadsKeysetCursors(t *testing.T) {
	f := parseStatusFilter(httptest.NewRequest(http.MethodGet, "/status.php?top=500&prevtop=900&jresult=4", nil))
	if f.Top != 500 || f.PrevTop != 900 || !f.HasResult || f.Result != 4 {
		t.Errorf("解析结果 %+v，期望 Top=500 PrevTop=900 Result=4", f)
	}
}

// 封榜期必须丢掉 jresult 筛选。
//
// 对应 status.php:170 的 `if ($result != -1 && !$lock)`。这不是可有可无的
// 细节：封榜的整个目的就是不让别人看到对手的过题情况，而"只看 AC"
// 筛选恰好能把这件事精确问出来 —— 封榜期有人 AC 了，筛一下就知道了。
func TestStatusFilterWhereDropsResultDuringLock(t *testing.T) {
	f := StatusFilter{Result: 4, HasResult: true}

	var unlocked []interface{}
	if sql := statusFilterWhere(f, &unlocked); !strings.Contains(sql, "s.result = ?") {
		t.Errorf("未封榜时应保留 jresult 条件，实际: %s", sql)
	}

	// 模拟 handleContestStatus 在封榜时做的清空。
	f.HasResult = false
	var locked []interface{}
	if sql := statusFilterWhere(f, &locked); strings.Contains(sql, "s.result = ?") {
		t.Errorf("封榜期不应带 jresult 条件，实际: %s", sql)
	}
}

// 语言筛选不受封榜影响 —— PHP 只在 jresult 上加了 !$lock，
// language 条件是无条件生效的。
func TestStatusFilterWhereKeepsLanguageDuringLock(t *testing.T) {
	f := StatusFilter{Language: 1, HasLanguage: true}
	f.HasResult = false // 模拟封榜期

	var args []interface{}
	sql := statusFilterWhere(f, &args)
	if !strings.Contains(sql, "s.language = ?") {
		t.Errorf("封榜期仍应保留语言筛选，实际: %s", sql)
	}
}

// 比赛模式的 WHERE 必须钉死 contest_id，同时保留筛选与 keyset 游标。
//
// 这条对着曾经的回归：比赛模式走 sqlc 生成的 GetStatusListContest，
// 只按 contest_id 查，language/jresult/user/top 全被忽略，且分页退回
// OFFSET —— 表现就是 /status.php?cid=N 永远停在第一页、筛不了任何东西。
func TestStatusFilterWhereSupportsContestMode(t *testing.T) {
	f := StatusFilter{
		ProblemID:   1019,
		UserID:      "sempr",
		Language:    1,
		HasLanguage: true,
		Result:      4,
		HasResult:   true,
		Top:         5000,
		HasTop:      true,
	}

	var args []interface{}
	// 比赛模式的 keyset 游标与练习模式共用同一段代码。
	sql := "WHERE s.contest_id = ? AND s.solution_id <= ? " + statusFilterWhere(f, &args)

	for _, want := range []string{"s.solution_id <= ?", "s.problem_id = ?", "s.user_id = ?", "s.language = ?", "s.result = ?"} {
		if !strings.Contains(sql, want) {
			t.Errorf("比赛模式缺少 %s，实际: %s", want, sql)
		}
	}

	// 参数顺序必须与占位符出现顺序一致，否则筛错列而不报错。
	want := []interface{}{int64(5000), int32(1019), "sempr", int32(1), int32(4)}
	if len(args) != len(want) {
		t.Fatalf("参数个数 %d，期望 %d：%#v", len(args), len(want), args)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("参数[%d] = %#v，期望 %#v", i, args[i], want[i])
		}
	}
}

// 筛选参数顺序错位是最危险的 bug：SQL 不报错，只是筛错列。
func TestStatusFilterWhereArgumentOrderMatchesPlaceholders(t *testing.T) {
	f := StatusFilter{
		UserID:      "alice",
		ProblemID:   100,
		Language:    7,
		HasLanguage: true,
		Result:      10,
		HasResult:   true,
		School:      "附中",
		GroupName:   "g1",
	}
	var args []interface{}
	sql := statusFilterWhere(f, &args)

	order := []string{"s.problem_id", "s.user_id", "s.language", "s.result", "u.school", "u.group_name"}
	prev := -1
	for _, cond := range order {
		at := strings.Index(sql, cond)
		if at < 0 {
			t.Fatalf("SQL 缺少条件 %s：%s", cond, sql)
		}
		if at < prev {
			t.Errorf("条件 %s 的位置(%d) 早于前一个，与参数顺序不符：%s", cond, at, sql)
		}
		prev = at
	}
	want := []interface{}{int32(100), "alice", int32(7), int32(10), "附中", "g1"}
	if len(args) != len(want) {
		t.Fatalf("参数 %#v，期望 %#v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Errorf("参数[%d] = %#v，期望 %#v", i, args[i], want[i])
		}
	}
}

// Top 链接必须真的回到最新，不能把当前游标带过去。
//
// 曾经 statusHref 会继承 f.Top，于是第二页的 "Top" 链接长成
// status.php?cid=1000&top=9900070 —— 点"回到最新"原地不动。
func TestTopHrefDropsCurrentCursor(t *testing.T) {
	// 用户正停在第 2 页（请求里带了 top）。
	p := newStatusPage(nil, StatusFilter{CID: 1000, Top: 9900070, HasTop: true})

	got := string(p.TopHref())
	if strings.Contains(got, "top=") {
		t.Errorf("Top 链接不应携带游标，实际 %q", got)
	}
	if !strings.Contains(got, "cid=1000") {
		t.Errorf("Top 链接应保留比赛筛选，实际 %q", got)
	}
}

// top 的判定要照抄 PHP：`isset($top) && $top != -1`，不是 `$top > 0`。
// ?top=0 在 PHP 上是 `solution_id<=0`，返回空页。
func TestStatusFilterTopFollowsPHPExistenceRule(t *testing.T) {
	cases := []struct {
		query string
		want  bool // 是否应生成 solution_id 条件
		label string
	}{
		{"/status.php", false, "缺省"},
		{"/status.php?top=500", true, "正常游标"},
		{"/status.php?top=-1", false, "PHP 的 -1 保留值"},
		{"/status.php?top=0", true, "畸形但 PHP 会照单全收"},
	}
	for _, c := range cases {
		f := parseStatusFilter(httptest.NewRequest(http.MethodGet, c.query, nil))
		var args []interface{}
		sql := statusFilterWhere(f, &args)
		got := strings.Contains(sql, "s.solution_id <= ?")
		if got != c.want {
			t.Errorf("%s: 生成游标条件=%v，期望 %v（SQL: %s）", c.label, got, c.want, sql)
		}
	}

	// 直接构造结构体时零值必须安全：不过滤。
	var args []interface{}
	if sql := statusFilterWhere(StatusFilter{}, &args); strings.Contains(sql, "s.solution_id") {
		t.Errorf("零值筛选不应带游标条件，实际: %s", sql)
	}
}

// 三个分页链接的游标方向不能反。
func TestPaginationCursorsPointTheRightWay(t *testing.T) {
	rows := make([]StatusRow, statusPageSize)
	for i := range rows {
		rows[i] = StatusRow{SolutionID: uint32(5000 - i)}
	}
	// 模拟已翻到第 2 页：请求带 top=4951&prevtop=5000。
	p := newStatusPage(rows, StatusFilter{Top: 4951, HasTop: true, PrevTop: 5000, Result: 4, HasResult: true})

	if got := string(p.NextHref()); !strings.Contains(got, "top=4951") || !strings.Contains(got, "prevtop=5000") {
		t.Errorf("Next 应为 top=<上一页首行>&prevtop=<上一页首行>，实际 %q", got)
	}
	if got := string(p.PrevHref()); !strings.Contains(got, "top=5000") {
		t.Errorf("Prev 应回到上一页顶部 top=5000，实际 %q", got)
	}
	if got := string(p.TopHref()); strings.Contains(got, "top=") {
		t.Errorf("Top 不应带游标，实际 %q", got)
	}
	// 所有链接都要保住筛选条件。
	for name, href := range map[string]string{"Top": string(p.TopHref()), "Prev": string(p.PrevHref()), "Next": string(p.NextHref())} {
		if !strings.Contains(href, "jresult=4") {
			t.Errorf("%s 链接丢了 jresult 筛选: %q", name, href)
		}
	}
}
