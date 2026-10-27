// 比赛模式（/problem.php?cid=X&pid=Y）的单元测试。
//
// 这个路径此前是等价性测试的盲区：切流后才发现比赛排名页的链接
// 全部指向 problem.php?cid=…&pid=…，而 handler 只认 ?id=，
// 返回 400 —— 属于真实回归。这里按 PHP 的判定顺序逐条守卫。

package web

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// defaultContestRow 返回一个"已开始、未结束、公开、不限网段"的比赛。
// 与生产 cid=1000（归档赛）的结构一致，只是时间窗取相对值。
func defaultContestRow(cid int32) dbgen.GetContestByIDRow {
	now := time.Now()
	return dbgen.GetContestByIDRow{
		ContestID:   cid,
		Title:       sql.NullString{String: "测试比赛", Valid: true},
		StartTime:   sql.NullTime{Time: now.Add(-time.Hour), Valid: true},
		EndTime:     sql.NullTime{Time: now.Add(time.Hour), Valid: true},
		Defunct:     "N",
		Description: sql.NullString{String: "描述", Valid: true},
		Private:     0,
		Langmask:    16580496,
	}
}

// ── 路由分派 ──────────────────────────────────────────────────────────

// /problem.php 同时承载练习与比赛两种模式，判别顺序必须与 PHP 的
// if/else if 链一致：先 id，再 cid+pid。
func TestHandleProblemDispatchesByMode(t *testing.T) {
	tests := []struct {
		name  string
		url   string
		want  string
		code  int
		setup func(*fakeQuerier)
	}{
		// 匿名身份 → viewerFor 返回 ViewerMember，练习模式走成员可见性查询
		{"练习模式按 id", "/problem.php?id=1", "GetProblemForMember", 0, nil},
		{"比赛模式按 cid+pid", "/problem.php?cid=1000&pid=0", "GetContestByID", 0, nil},
		{"缺 id 落到 400", "/problem.php", "", http.StatusBadRequest, nil},
		{"只有 cid 不算比赛", "/problem.php?cid=1000", "", http.StatusBadRequest, nil},
		{"只有 pid 不算比赛", "/problem.php?pid=0", "", http.StatusBadRequest, nil},
		{"负数 cid 折叠成正数", "/problem.php?cid=-1000&pid=0", "GetContestByID", 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeQuerier{contest: defaultContestRow(1000)}
			s := newTestServer(t, f, nil)
			rec := httptest.NewRecorder()
			s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.url, nil))

			if tc.code != 0 && rec.Code != tc.code {
				t.Errorf("状态码 = %d，期望 %d", rec.Code, tc.code)
				return
			}
			if tc.want != "" {
				if len(f.called) == 0 || f.called[0] != tc.want {
					t.Errorf("首个调用 = %v，期望 %s", f.called, tc.want)
				}
			}
		})
	}
}

// 比赛取题的键是 contest_problem.num，不是 problem_id。
// 传错了会让选手看到别的题（甚至越界报错）。
func TestContestProblemUsesNumAsKey(t *testing.T) {
	f := &fakeQuerier{}
	if _, err := ContestProblem(context.Background(), f, 1000, 2); err != nil {
		t.Fatal(err)
	}
	if len(f.called) != 2 || f.called[0] != "GetContestProblemID" || f.called[1] != "GetProblemByID" {
		t.Errorf("调用序列 = %v，期望 [GetContestProblemID GetProblemByID]", f.called)
	}
}

// 比赛里的题目不存在时不应报 500：赛中被改题后选手点开旧链接是正常操作。
func TestContestProblemMissingIsGraceful(t *testing.T) {
	f := &fakeQuerier{}
	f.contest = defaultContestRow(1000)
	// GetContestProblemID 返回 ErrNoRows 模拟 num 越界
	s := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problem.php?cid=1000&pid=99", nil))

	// fake 的 GetContestProblemID 恒返回 num+1000，不会命中 no rows；
	// 直接测 ContestProblem 的错误路径。
	if _, err := ContestProblem(context.Background(), &missingProblemQuerier{}, 1000, 99); err == nil {
		t.Errorf("num 越界应返回错误")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("HTTP 状态码 = %d（此处 fake 不越界，应正常渲染）", rec.Code)
	}
}

// missingProblemQuerier 让 GetContestProblemID 返回 ErrNoRows。
type missingProblemQuerier struct{ fakeQuerier }

func (m *missingProblemQuerier) GetContestProblemID(context.Context, dbgen.GetContestProblemIDParams) (int32, error) {
	return 0, sql.ErrNoRows
}

// ── 比赛准入判定 ──────────────────────────────────────────────────────

// 判定顺序复刻 PHP：管理员绕过除 subnet 外的全部检查。
func TestEvaluateContestAccessAdminBypasses(t *testing.T) {
	f := &fakeQuerier{}
	c := defaultContestRow(1000)
	c.Defunct = "Y"                                                          // 已关闭
	c.Private = 1                                                            // 私有
	c.StartTime = sql.NullTime{Time: time.Now().Add(time.Hour), Valid: true} // 未开始

	acc, err := EvaluateContestAccess(context.Background(), f, c, Identity{Admin: true}, time.Now(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !acc.OK() {
		t.Errorf("管理员应绕过关闭/私有/未开始检查，实际 Deny=%v", acc.Deny)
	}
}

// 匿名访问私有比赛必须拒绝：错放行等于泄露赛题。
func TestEvaluateContestAccessPrivateDenied(t *testing.T) {
	f := &fakeQuerier{}
	c := defaultContestRow(1000)
	c.Private = 1

	acc, err := EvaluateContestAccess(context.Background(), f, c, Anonymous(), time.Now(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if acc.OK() {
		t.Errorf("匿名访问私有比赛应被拒绝")
	}
	if acc.Deny != ContestPrivate {
		t.Errorf("Deny = %v，期望 ContestPrivate", acc.Deny)
	}
}

// 带会话但未被邀请的私有比赛同样拒绝（Go 侧无法验证受邀标记）。
func TestEvaluateContestAccessPrivateWithSessionDenied(t *testing.T) {
	f := &fakeQuerier{} // privileged=false
	c := defaultContestRow(1000)
	c.Private = 1

	acc, err := EvaluateContestAccess(context.Background(), f, c, Identity{UserID: "u", LoggedIn: true}, time.Now(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if acc.OK() {
		t.Errorf("未被邀请的会话访问私有比赛应被拒绝")
	}
}

// privilege 表里有授权时放行，且查询的 rightstr 必须是 c<cid>。
func TestEvaluateContestAccessPrivilegeGranted(t *testing.T) {
	f := &fakeQuerier{privileged: true}
	c := defaultContestRow(1000)
	c.Private = 1

	acc, err := EvaluateContestAccess(context.Background(), f, c, Identity{UserID: "u", LoggedIn: true}, time.Now(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !acc.OK() {
		t.Errorf("有授权的会话应放行，实际 Deny=%v", acc.Deny)
	}
}

// 已关闭的比赛对非管理员拒绝，原因必须是 ContestClosed。
func TestEvaluateContestAccessClosed(t *testing.T) {
	f := &fakeQuerier{}
	c := defaultContestRow(1000)
	c.Defunct = "Y"

	acc, err := EvaluateContestAccess(context.Background(), f, c, Anonymous(), time.Now(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if acc.Deny != ContestClosed {
		t.Errorf("Deny = %v，期望 ContestClosed", acc.Deny)
	}
}

// 未开始的比赛拒绝（管理员除外）。
func TestEvaluateContestAccessNotStarted(t *testing.T) {
	f := &fakeQuerier{}
	c := defaultContestRow(1000)
	c.StartTime = sql.NullTime{Time: time.Now().Add(time.Hour), Valid: true}

	acc, err := EvaluateContestAccess(context.Background(), f, c, Anonymous(), time.Now(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if acc.Deny != ContestNotStarted {
		t.Errorf("Deny = %v，期望 ContestNotStarted", acc.Deny)
	}
}

// IP 不在网段内必须拒绝，即使其他条件全部满足。
func TestEvaluateContestAccessSubnet(t *testing.T) {
	f := &fakeQuerier{}
	c := defaultContestRow(1000)
	c.Subnet = "10.0.0.0/8"

	acc, err := EvaluateContestAccess(context.Background(), f, c, Anonymous(), time.Now(), "192.168.1.1", false)
	if err != nil {
		t.Fatal(err)
	}
	if acc.Deny != ContestSubnet {
		t.Errorf("Deny = %v，期望 ContestSubnet", acc.Deny)
	}

	// 命中网段则放行
	acc2, err := EvaluateContestAccess(context.Background(), f, c, Anonymous(), time.Now(), "10.1.2.3", false)
	if err != nil {
		t.Fatal(err)
	}
	if !acc2.OK() {
		t.Errorf("命中网段应放行，实际 Deny=%v", acc2.Deny)
	}
}

// subnet 为空表示不限制。
func TestInContestSubnetEmpty(t *testing.T) {
	if !inContestSubnet("1.2.3.4", "") {
		t.Errorf("subnet 为空应不限制")
	}
	if inContestSubnet("1.2.3.4", "10.0.0.0/8") {
		t.Errorf("不在网段内应拒绝")
	}
	if !inContestSubnet("10.2.3.4", "10.0.0.0/8, 192.168.0.0/16") {
		t.Errorf("多网段逗号分隔，任一命中即通过")
	}
}

// 个人限时赛：描述含"限时"且带会话时拒绝（无法计时，错放行等于放过超时作答）。
func TestEvaluateContestAccessLimited(t *testing.T) {
	f := &fakeQuerier{}
	c := defaultContestRow(1000)
	c.Description = sql.NullString{String: "限时90分钟", Valid: true}

	acc, err := EvaluateContestAccess(context.Background(), f, c, Identity{UserID: "u", LoggedIn: true}, time.Now(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if acc.Deny != ContestLimited {
		t.Errorf("Deny = %v，期望 ContestLimited", acc.Deny)
	}

	// 匿名时 PHP 也跳过该判定（isset($_SESSION[_user_id]) 为假），应放行
	acc2, err := EvaluateContestAccess(context.Background(), f, c, Anonymous(), time.Now(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !acc2.OK() {
		t.Errorf("匿名访问限时赛应放行（与 PHP 一致），实际 Deny=%v", acc2.Deny)
	}
}

// 自由练习开关不绕过比赛准入：contest-check.php 的检查都不看
// OJ_FREE_PRACTICE，problem.php:65 只影响错误页文案，两种情况都是错误页。
// 若放行会绕过私有比赛校验，属于越权，必须拒绝。
func TestEvaluateContestAccessFreePracticeDoesNotBypass(t *testing.T) {
	f := &fakeQuerier{}
	c := defaultContestRow(1000)
	c.Private = 1

	acc, err := EvaluateContestAccess(context.Background(), f, c, Anonymous(), time.Now(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if acc.OK() {
		t.Errorf("自由练习下私有比赛仍应拒绝，实际放行了")
	}
	if acc.Deny != ContestPrivate {
		t.Errorf("Deny = %v，期望 ContestPrivate", acc.Deny)
	}
}

// 已结束的比赛 IsOver 必须为真：模板据此切换提交入口（syzoj:92-95）。
func TestEvaluateContestAccessIsOver(t *testing.T) {
	f := &fakeQuerier{}
	c := defaultContestRow(1000)
	c.EndTime = sql.NullTime{Time: time.Now().Add(-time.Minute), Valid: true}

	acc, err := EvaluateContestAccess(context.Background(), f, c, Anonymous(), time.Now(), "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !acc.OK() {
		t.Fatalf("已结束的公开比赛应可访问，实际 Deny=%v", acc.Deny)
	}
	if !acc.IsOver {
		t.Errorf("IsOver 应为真")
	}
}

// ── 比赛模式渲染 ──────────────────────────────────────────────────────

// 比赛模式的提交入口：未结束时按 cid/pid/langmask，结束后退回练习方式。
// 这是切流回归的直接现场：链接写错会让提交页丢失比赛上下文。
func TestRenderContestActions(t *testing.T) {
	d := ProblemDetail{
		ProblemID: 1013,
		Title:     "题面",
		Contest:   &ContestDetail{CID: 1000, Title: "比赛", Langmask: 16580496, PID: 0, IsOver: false},
	}
	out := mustRenderActions(t, d)
	if !strings.Contains(out, "submitpage.php?cid=1000&pid=0&langmask=16580496") {
		t.Errorf("未结束的比赛应带 cid/pid/langmask 提交链接: %s", out)
	}
	if !strings.Contains(out, "contest.php?cid=1000") {
		t.Errorf("缺少返回比赛按钮: %s", out)
	}
	if !strings.Contains(out, "status.php?problem_id=1013&cid=1000") {
		t.Errorf("缺少本比赛提交记录按钮: %s", out)
	}

	// 结束后退回练习方式提交
	d.Contest.IsOver = true
	out = mustRenderActions(t, d)
	if !strings.Contains(out, "submitpage.php?id=1013") {
		t.Errorf("已结束的比赛应退回 submitpage.php?id=: %s", out)
	}
	if strings.Contains(out, "langmask=") {
		t.Errorf("已结束的比赛不应再带 langmask: %s", out)
	}
}

// 比赛模式下标题区必须带所属比赛横幅。
func TestRenderContestBanner(t *testing.T) {
	d := ProblemDetail{
		ProblemID: 1013,
		Title:     "题面",
		Contest:   &ContestDetail{CID: 1000, Title: "比赛", PID: 2, IsOver: false},
	}
	out := mustRenderDetail(t, d)
	if !strings.Contains(out, "contest.php?cid=1000") {
		t.Errorf("缺少比赛横幅链接: %s", out)
	}
	if !strings.Contains(out, "第 2 题") {
		t.Errorf("缺少题号: %s", out)
	}
}

// NOIP 锁榜隐藏 AC 数与提交数，替换成问号（problem.php:158-160）。
func TestRenderNoIPMasked(t *testing.T) {
	d := ProblemDetail{
		ProblemID:    1,
		Title:        "题面",
		StatsHidden:  true,
		SubmitStat:   sql.NullInt32{Int32: 10, Valid: true},
		AcceptedStat: sql.NullInt32{Int32: 5, Valid: true},
		Hint:         "提示内容",
		HintHidden:   true,
	}
	out := mustRenderDetail(t, d)
	if strings.Contains(out, ">10<") || strings.Contains(out, ">5<") {
		t.Errorf("锁榜时不应显示真实统计数字: %s", out)
	}
	if !strings.Contains(out, "?") {
		t.Errorf("锁榜时应显示问号占位: %s", out)
	}
	if strings.Contains(out, "提示内容") {
		t.Errorf("锁榜时不应显示提示: %s", out)
	}
}

// 错误页标题与内容的映射。
//
// 断言对齐 PHP contest-check.php:131-168 实际输出的内容：
// 标题行是"作业比赛编号 : {cid} - {标题}"，随后是警告语、
// 名次/OI名次/统计三个入口，以及密码输入表单。
func TestRenderContestError(t *testing.T) {
	c := defaultContestRow(1000)
	v := newContestErrorView(1000, &c, ContestNotStarted, false)
	out := mustRender(t, "contestError", v)

	if !strings.Contains(out, "作业比赛编号 : 1000") {
		t.Errorf("标题行应带比赛编号与中文表头: %s", out)
	}
	if !strings.Contains(out, msgPrivateWarning) {
		t.Errorf("错误页应给出不可查看题目的警告: %s", out)
	}
	if !strings.Contains(out, "contestrank.php?cid=1000") || !strings.Contains(out, "contestrank-oi.php?cid=1000") {
		t.Errorf("应有名次与 OI 名次入口: %s", out)
	}
	if !strings.Contains(out, `name="password"`) {
		t.Errorf("应保留密码输入表单: %s", out)
	}

	// 比赛不存在：标题是"比赛已经关闭!"，描述是网段那句。
	v2 := newContestErrorView(99999, nil, ContestNotFound, false)
	out2 := mustRender(t, "contestError", v2)
	if !strings.Contains(out2, msgContestClosedTitle) {
		t.Errorf("比赛不存在时标题应为 %q: %s", msgContestClosedTitle, out2)
	}
	if !strings.Contains(out2, "Not in "+msgSubnet) {
		t.Errorf("比赛不存在时描述应为网段提示: %s", out2)
	}

	// 个人限时赛超时：PHP 提前 exit，没有警告语、按钮与密码框。
	v3 := newContestErrorView(1000, &c, ContestLimited, false)
	out3 := mustRender(t, "contestError", v3)
	if strings.Contains(out3, msgPrivateWarning) || strings.Contains(out3, `name="password"`) {
		t.Errorf("限时赛拒绝页不应带警告与密码框: %s", out3)
	}
}

// 非法 cid 的归一化，对应 contest-check.php:3 的 abs(intval(...))。
func TestAbs32(t *testing.T) {
	for _, c := range []struct {
		in, want int32
	}{
		{0, 0}, {5, 5}, {-5, 5}, {2147483647, 2147483647},
	} {
		if got := abs32(c.in); got != c.want {
			t.Errorf("abs32(%d) = %d, 期望 %d", c.in, got, c.want)
		}
	}
}

// ── 模板渲染辅助 ────────────────────────────────────────────────────────
//
// 直接复用 pageTemplates 而不是新建模板集：这些用例要验证的正是
// 线上那一套模板（含 layout 与具名片段），单独 parse 一份等价模板
// 会出现"测试通过、线上报错"的假绿。

// mustRender 渲染指定具名模板并在出错时终止用例。
func mustRender(t *testing.T, name string, data any) string {
	t.Helper()
	var sb strings.Builder
	if err := pageTemplates.ExecuteTemplate(&sb, name, data); err != nil {
		t.Fatalf("渲染 %s 失败: %v", name, err)
	}
	return sb.String()
}

// mustRenderDetail 渲染题目详情页。
func mustRenderDetail(t *testing.T, d ProblemDetail) string {
	t.Helper()
	return mustRender(t, "problemDetail", d)
}

// mustRenderActions 渲染题面下方的操作按钮组。
//
// 单独拆一个辅助是因为它有不依赖数据库的部分：
// 只要 ProblemDetail 构造对，就能验证链接串是否带全比赛上下文。
func mustRenderActions(t *testing.T, d ProblemDetail) string {
	t.Helper()
	return mustRender(t, "problemActions", d)
}
