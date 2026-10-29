package web

import (
	"context"
	"database/sql"
	"html"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sempr/hustoj-go/internal/web/config"
	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// defaultTestConfig 返回测试用默认配置。
func defaultTestConfig() *config.Config { return config.Default() }

// templateEscape 复刻 html/template 对纯文本的转义，用于构造期望值。
// 只处理详情测试需要的字符：& < > " '。
func templateEscape(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&#34;", "'", "&#39;")
	return r.Replace(s)
}

// newTestLogger 返回一个丢弃输出的 logger，避免测试把错误日志刷到终端。
func newTestLogger() *slog.Logger {
	if os.Getenv("HUSTOJ_TEST_LOG") != "" {
		return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

// newTestServer 构造一个注入替身查询器、不碰真实数据库的服务。
func newTestServer(t *testing.T, q dbgen.FullQuerier, mutate func(*config.Config)) *Server {
	t.Helper()
	cfg := config.Default()
	if mutate != nil {
		mutate(cfg)
	}
	log := newTestLogger()
	return &Server{cfg: cfg, log: log, q: q}
}

// ── 参数解析 ──────────────────────────────────────────────────────────

// 只由空白组成的搜索词在 PHP 里等同没搜索（trim 后为空）。
// Go 侧若不 trim，会走搜索分支却匹配不到任何东西，用户看到空列表。
func TestParseSearchTreatsWhitespaceAsEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "\t", "\n", "  \t\n "} {
		if got := parseSearch(in); got != "" {
			t.Errorf("parseSearch(%q) = %q，期望空（等同未搜索）", in, got)
		}
	}
	// 词内部的空白必须保留，它是搜索内容的一部分
	if got := parseSearch("  a b  "); got != "a b" {
		t.Errorf("parseSearch 内部空白被吃掉: %q", got)
	}
}

func TestParsePage(t *testing.T) {
	tests := map[string]int{
		"":     1, // 未给 page
		"1":    1,
		"3":    3,
		"abc":  1, // PHP 的 intval("abc")=0，Go 夹到 1
		"0":    1, // 0 会被夹到 1，否则 LIMIT 偏移为负
		"-2":   1,
		"999":  999,
		" 2 ":  2, // intval 容忍首尾空白，strconv.Atoi 不容忍
		"2abc": 2, // intval 解析到首个非数字即停，Atoi 会直接报错
		"+3":   3,
		// 溢出与 PHP 一致：intval 饱和到整型末位，LIMIT 偏移过大只是查不到行。
		"99999999999999999999": math.MaxInt32,
	}
	for in, want := range tests {
		if got := parsePage(in); got != want {
			t.Errorf("parsePage(%q) = %d，期望 %d", in, got, want)
		}
	}
}

// ── 身份 → 可见范围 ──────────────────────────────────────────────────

// PHP 的判断顺序是「管理员 → 自由练习 → 普通用户」，管理员优先。
// 顺序反了会让自由练习站点的管理员看不到已删除题目。
func TestViewerForIdentity(t *testing.T) {
	tests := []struct {
		name string
		free bool
		id   Identity
		want Viewer
	}{
		{"匿名+普通站", false, Anonymous(), ViewerMember},
		{"匿名+自由练习站", true, Anonymous(), ViewerFreePractice},
		{"管理员+普通站", false, Identity{Admin: true}, ViewerAdmin},
		// 关键用例：管理员优先于自由练习开关
		{"管理员+自由练习站", true, Identity{Admin: true}, ViewerAdmin},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t, &fakeQuerier{}, func(c *config.Config) { c.FreePractice = tc.free })
			if got := s.viewerFor(tc.id); got != tc.want {
				t.Errorf("viewerFor = %v，期望 %v", got, tc.want)
			}
		})
	}
}

// ── 题目列表 HTTP ────────────────────────────────────────────────────

func TestHandleProblemSetRendersRows(t *testing.T) {
	f := &fakeQuerier{
		total: 2,
		rows: []fakeRow{
			{ID: 1000, Title: "A+B Problem", Source: sql.NullString{String: "Codeforces 洛谷", Valid: true},
				Submit: sql.NullInt32{Int32: 10, Valid: true}, Accepted: sql.NullInt32{Int32: 4, Valid: true}, Defunct: "N"},
			{ID: 1001, Title: "第二题", Source: sql.NullString{Valid: true}, Defunct: "N"},
		},
	}
	s := newTestServer(t, f, nil)

	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problemset.php", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200；body: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	// 每个题目都要出现，且链接指向 PHP 相同格式的详情页
	for _, want := range []string{
		`href="problem.php?id=1000"`,
		// 注意：html/template 会把 + 转义成 &#43;（防 UTF-7 型 XSS 的历史行为），
		// 浏览器渲染出来仍是 "+"。这里断言转义后的形式。
		"A&#43;B Problem",
		`href="problem.php?id=1001"`, "第二题",
		"Codeforces", "洛谷",
		"4/10",    // 通过/提交
		"40.000%", // 通过率 4/10
	} {
		if !strings.Contains(body, want) {
			t.Errorf("响应缺少 %q\nbody:\n%s", want, body)
		}
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "charset=utf-8") {
		t.Errorf("Content-Type = %q，期望含 charset=utf-8（中文标题会乱码）", ct)
	}
}

// 提交数为 0 时不能算成除零，也不能显示 NaN%。
func TestHandleProblemSetZeroSubmit(t *testing.T) {
	f := &fakeQuerier{total: 1, rows: []fakeRow{
		{ID: 1, Title: "零提交", Defunct: "N",
			Submit: sql.NullInt32{Valid: true}, Accepted: sql.NullInt32{Valid: true}},
	}}
	s := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problemset.php", nil))

	body := rec.Body.String()
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	for _, bad := range []string{"NaN", "+Inf", "Inf%"} {
		if strings.Contains(body, bad) {
			t.Errorf("通过率出现 %q（提交为 0 时应显示 0.000%%）", bad)
		}
	}
	if !strings.Contains(body, "0.000%") {
		t.Errorf("提交为 0 时应显示 0.000%%\nbody:\n%s", body)
	}
}

// source 为 NULL 时不能渲染出空白标签或 "null"。
func TestHandleProblemSetNullSource(t *testing.T) {
	f := &fakeQuerier{total: 1, rows: []fakeRow{
		{ID: 1, Title: "无来源", Source: sql.NullString{Valid: false}, Defunct: "N"},
	}}
	s := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problemset.php", nil))

	body := rec.Body.String()
	if strings.Contains(body, "null") || strings.Contains(body, "<a title=\"\"") {
		t.Errorf("source 为 NULL 时渲染异常:\n%s", body)
	}
}

// 标题含 HTML 时必须转义。problemset.php 第 2 项直接拼 $row['title']，
// 属上游存储型 XSS；Go 侧不复刻。
func TestHandleProblemSetEscapesTitle(t *testing.T) {
	f := &fakeQuerier{total: 1, rows: []fakeRow{
		{ID: 1, Title: `<script>alert(1)</script>`, Source: sql.NullString{Valid: true}, Defunct: "N"},
	}}
	s := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problemset.php", nil))

	body := rec.Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Errorf("标题未转义，存在存储型 XSS:\n%s", body)
	}
	if !strings.Contains(body, "&lt;script&gt;") {
		t.Errorf("标题未被正确转义:\n%s", body)
	}
}

// AC 标记必须反映当前用户状态；未登录时不得显示任何标记。
func TestHandleProblemSetMarkers(t *testing.T) {
	f := &fakeQuerier{
		total: 3,
		rows: []fakeRow{
			{ID: 1, Title: "AC", Defunct: "N"},
			{ID: 2, Title: "提交过", Defunct: "N"},
			{ID: 3, Title: "没提交", Defunct: "N"},
		},
		best: []dbgen.GetUserBestResultsRow{
			{ProblemID: 1, Result: 4},
			{ProblemID: 2, Result: 6},
		},
	}
	// 这里只测渲染模型，不经过 HTTP 层 —— HTTP 层的匿名身份会把标记
	// 全部覆盖成"未提交"，无法验证 AC/提交过两种标记的渲染。
	_ = newTestServer(t, f, nil)

	// 直接测渲染模型
	page := ProblemListPage{Problems: []ProblemSummary{
		{ProblemID: 1, Title: "AC", Defunct: "N", SolvedByUser: true, AcceptedByUser: true},
		{ProblemID: 2, Title: "提交过", Defunct: "N", SolvedByUser: true},
		{ProblemID: 3, Title: "没提交", Defunct: "N"},
	}}
	rows := buildProblemRows(page)
	if rows[0].MarkerClass != "label label-success" || rows[0].Marker != "Y" {
		t.Errorf("AC 题标记 = %q/%q，期望 Y + label-success", rows[0].Marker, rows[0].MarkerClass)
	}
	// PHP 的标记文案：AC → 'Y'，提交过但没 AC → 'N'（第 172-176 行）。
	// 这里容易写反成 "AC/Y" 或"提交过/Y"，所以显式断言字面值。
	if rows[1].MarkerClass != "label label-danger" || rows[1].Marker != "N" {
		t.Errorf("仅提交题标记 = %q/%q，期望 N + label-danger", rows[1].Marker, rows[1].MarkerClass)
	}
	if rows[2].MarkerClass != "none" || rows[2].Marker != "" {
		t.Errorf("未提交题标记 = %q/%q，期望空 + none", rows[2].Marker, rows[2].MarkerClass)
	}
}

// 未登录时 PHP 根本不发完成状态查询，Go 侧同样不发。
// 若这里改成无条件查，线上会给每个匿名请求都打一次 solution 全表扫描。
func TestAnonymousListSkipsBestResults(t *testing.T) {
	f := &fakeQuerier{total: 1, rows: []fakeRow{{ID: 1, Title: "x", Defunct: "N"}}}
	s := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problemset.php", nil))

	for _, c := range f.called {
		if c == "GetUserBestResults" {
			t.Error("匿名请求不应查询用户完成状态")
		}
	}
}

// 搜索词要传到查询参数里，否则搜索框形同虚设。
func TestHandleProblemSetPassesSearch(t *testing.T) {
	f := &fakeQuerier{total: 0}
	s := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problemset.php?search=abc", nil))

	if f.lastTitleLike != "%abc%" {
		t.Errorf("title 匹配串 = %q，期望 %%abc%%", f.lastTitleLike)
	}
}

// 纯空白搜索词必须走无搜索分支，否则会得到空列表。
func TestHandleProblemSetWhitespaceSearchIsNoSearch(t *testing.T) {
	f := &fakeQuerier{total: 0}
	s := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problemset.php?search=%20%20", nil))

	for _, c := range f.called {
		if strings.HasPrefix(c, "Search") {
			t.Errorf("空白搜索词走了搜索分支（%s），应走无搜索分支", c)
		}
	}
}

// 题库列表要给出"这题参加过哪些比赛"的链接。
//
// 泄题边界：进行中比赛的题目本来就不在普通用户的题库列表里，所以
// 这里挂出的关联不会暴露"进行中比赛的题集"（见 ListProblemContests 的注释）。
func TestProblemsetRendersContestLinks(t *testing.T) {
	f := &fakeQuerier{
		rows: []fakeRow{{ID: 1001, Title: "A+B Problem", Defunct: "N"}},
		problemContests: []dbgen.ListProblemContestsRow{
			{ProblemID: 1001, ContestID: 1119, Title: sql.NullString{String: "示例比赛：从入门到交互", Valid: true}},
		},
	}
	page, err := ListProblems(context.Background(), f, ViewerMember, "", 1, "", time.Now())
	if err != nil {
		t.Fatalf("ListProblems 失败: %v", err)
	}
	if len(page.Problems) != 1 {
		t.Fatalf("题目数 = %d，期望 1", len(page.Problems))
	}
	links := page.Problems[0].Contests
	if len(links) != 1 {
		t.Fatalf("比赛链接数 = %d，期望 1", len(links))
	}
	if links[0].ContestID != 1119 || links[0].Title != "示例比赛：从入门到交互" {
		t.Errorf("比赛链接 = %+v，期望 cid=1119", links[0])
	}

	body := mustRender(t, "problemset", buildPageVM(page))
	if !strings.Contains(body, `href="contest.php?cid=1119"`) {
		t.Errorf("题库列表缺少比赛链接:\n%s", body)
	}
	if !strings.Contains(body, "示例比赛：从入门到交互") {
		t.Errorf("比赛链接缺少标题:\n%s", body)
	}
}

// 没有参加过比赛的题目不应渲染出比赛链接。
func TestProblemsetWithoutContestHasNoLink(t *testing.T) {
	f := &fakeQuerier{rows: []fakeRow{{ID: 1002, Title: "x", Defunct: "N"}}}
	page, err := ListProblems(context.Background(), f, ViewerMember, "", 1, "", time.Now())
	if err != nil {
		t.Fatalf("ListProblems 失败: %v", err)
	}
	if len(page.Problems[0].Contests) != 0 {
		t.Errorf("不该有比赛链接: %+v", page.Problems[0].Contests)
	}
	body := mustRender(t, "problemset", buildPageVM(page))
	if strings.Contains(body, "contest.php?cid=") {
		t.Errorf("不该出现比赛链接:\n%s", body)
	}
}

// ── 题目详情 HTTP ────────────────────────────────────────────────────

func TestHandleProblemOK(t *testing.T) {
	f := &fakeQuerier{}
	f.detail = dbgen.Problem{
		ProblemID: 1000, Title: "A+B", Description: "<p>求两数之和</p>",
		Input: "读入两个整数", Output: "输出和",
		SampleInput:  sql.NullString{String: "1 2", Valid: true},
		SampleOutput: sql.NullString{String: "3", Valid: true},
		Hint:         "注意范围", Defunct: "N", TimeLimit: 1, MemoryLimit: 65536,
		Submit: sql.NullInt32{Int32: 10, Valid: true}, Accepted: sql.NullInt32{Int32: 4, Valid: true},
	}
	s := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problem.php?id=1000", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200；body: %s", rec.Code, rec.Body.String())
	}
	// 对 HTML 实体做解码后再断言：html/template 会把 "+" 转义成 "&#43;"、
	// "&" 转义成 "&amp;"，浏览器显示的仍是原文。断言必须对着"用户看到的
	// 文字"，否则会把正确的转义误判成缺失内容。
	body := html.UnescapeString(rec.Body.String())
	for _, want := range []string{"A+B", "求两数之和", "读入两个整数", "输出和", "1 2", "3", "注意范围"} {
		if !strings.Contains(body, want) {
			t.Errorf("详情缺少 %q\nbody:\n%s", want, body)
		}
	}
}

// 富文本字段（description）按 PHP 行为原样输出，不转义。
func TestHandleProblemKeepsRichText(t *testing.T) {
	f := &fakeQuerier{detail: dbgen.Problem{
		ProblemID: 1, Title: "t", Defunct: "N",
		Description: "<pre>输入格式说明</pre>",
	}}
	s := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problem.php?id=1", nil))

	if !strings.Contains(rec.Body.String(), "<pre>输入格式说明</pre>") {
		t.Errorf("题目描述被转义了，会导致公式/排版显示为源码:\n%s", rec.Body.String())
	}
}

// 样例是纯文本，必须转义，否则样例里的尖括号会破坏页面结构。
func TestHandleProblemEscapesSamples(t *testing.T) {
	f := &fakeQuerier{detail: dbgen.Problem{
		ProblemID: 1, Title: "t", Defunct: "N",
		SampleInput: sql.NullString{String: "a<b && c>d", Valid: true},
	}}
	s := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problem.php?id=1", nil))

	body := rec.Body.String()
	if strings.Contains(body, "a<b && c>d") {
		t.Errorf("样例未转义:\n%s", body)
	}
	if !strings.Contains(body, "a&lt;b") {
		t.Errorf("样例转义结果异常:\n%s", body)
	}
}

// 已删除题目只提示、不展示内容。
func TestHandleProblemDefunctHidesContent(t *testing.T) {
	f := &fakeQuerier{detail: dbgen.Problem{
		ProblemID: 1, Title: "已删除", Defunct: "Y", Description: "<p>不该显示</p>",
	}}
	s := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problem.php?id=1", nil))

	body := rec.Body.String()
	if strings.Contains(body, "不该显示") {
		t.Errorf("已删除题目仍展示了内容:\n%s", body)
	}
	if !strings.Contains(body, "该题目已被删除或尚未公开") {
		t.Errorf("缺少删除提示:\n%s", body)
	}
}

func TestHandleProblemBadID(t *testing.T) {
	for _, in := range []string{"", "?id=", "?id=abc", "?id=0", "?id=-5"} {
		f := &fakeQuerier{}
		s := newTestServer(t, f, nil)
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problem.php"+in, nil))

		if rec.Code != http.StatusBadRequest {
			t.Errorf("id=%q 状态码 = %d，期望 400", in, rec.Code)
		}
		// 非法 id 不该打到数据库
		if len(f.called) != 0 {
			t.Errorf("id=%q 非法时仍查询了数据库: %v", in, f.called)
		}
	}
}

// 成员模式下查不到（已删除或被比赛占用）应返回 404，与 PHP 渲染错误页语义一致，
// 但用 404 更利于监控发现问题。
func TestHandleProblemMemberHiddenIs404(t *testing.T) {
	// 成员模式走 GetProblemForMember，fake 注入 ErrNoRows 模拟"被比赛占用"
	f := &fakeQuerier{err: sql.ErrNoRows}
	s := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problem.php?id=1", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("状态码 = %d，期望 404", rec.Code)
	}
	for _, c := range f.called {
		if c != "GetProblemForMember" {
			t.Errorf("匿名请求应走 GetProblemForMember，实际调用了 %s", c)
		}
	}
}

// 自由练习模式下详情只查 defunct，不排除比赛题。
func TestHandleProblemFreePracticeQuery(t *testing.T) {
	f := &fakeQuerier{detail: dbgen.Problem{ProblemID: 1, Title: "t", Defunct: "N"}}
	s := newTestServer(t, f, func(c *config.Config) { c.FreePractice = true })
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problem.php?id=1", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	// NOIP 锁榜判定对练习模式也生效（problem.php:150-168 在模式分支之外），
	// 因此在选完可见性查询后还会查 IsProblemLocked。
	want := []string{"GetProblemFreePractice", "IsProblemLocked"}
	if !reflect.DeepEqual(f.called, want) {
		t.Errorf("调用序列 = %v，期望 %v", f.called, want)
	}
}

// 不存在的题目应是 404，不能算作服务端故障。
func TestHandleProblemNotFound(t *testing.T) {
	f := &fakeQuerier{err: sql.ErrNoRows}
	s := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problem.php?id=999999", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("状态码 = %d，期望 404", rec.Code)
	}
}

func TestHandleProblemDBErrorIs500(t *testing.T) {
	f := &fakeQuerier{err: sql.ErrConnDone}
	s := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problem.php?id=1", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("状态码 = %d，期望 500", rec.Code)
	}
}

// 错误响应不能泄露 SQL 或数据库结构。
func TestErrorResponseDoesNotLeakSQL(t *testing.T) {
	f := &fakeQuerier{err: sql.ErrConnDone}
	s := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/problem.php?id=1", nil))

	body := rec.Body.String()
	for _, leak := range []string{"SELECT", "FROM problem", "SQLSTATE", "connection refused"} {
		if strings.Contains(body, leak) {
			t.Errorf("错误响应泄露了内部细节 %q:\n%s", leak, body)
		}
	}
}

// ── 方法限制 ──────────────────────────────────────────────────────────

// 用 Go 1.22 的方法路由后，POST 不应命中 GET 处理器。
//
// 关键是要带上**合法**的 CSRF token：CSRF 中间件比方法路由更靠外，
// 没有 token 时请求会先被 CSRF 拦掉，拿到的是 200 + 弹窗，
// 那样这个用例就变成在测 CSRF，而不是在测 405。
func TestRoutesRejectPost(t *testing.T) {
	f := &fakeQuerier{total: 1, rows: []fakeRow{{ID: 1, Title: "x", Defunct: "N"}}}
	s := newTestServer(t, f, nil)
	s.sessions = newTestSessionStore(t)

	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, s)

	for _, path := range []string{"/problemset.php", "/problem.php?id=1"} {
		form := url.Values{"csrf_token": {token}}
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", sameOrigin)
		jar.apply(req)
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("POST %s = %d, want 405", path, rec.Code)
		}
	}
}

// ── 分页导航 ──────────────────────────────────────────────────────────

func TestPaginationHTML(t *testing.T) {
	tests := []struct {
		name     string
		vm       pageVM
		wantPrev bool
		wantNext bool
	}{
		{"单页无导航", pageVM{Page: 1, TotalPage: 1}, false, false},
		{"首页有下一页", pageVM{Page: 1, TotalPage: 5}, false, true},
		{"末页有上一页", pageVM{Page: 5, TotalPage: 5}, true, false},
		{"中间页两侧都有", pageVM{Page: 3, TotalPage: 5}, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vm := buildPageVM(ProblemListPage{Page: tc.vm.Page, TotalPage: int64(tc.vm.TotalPage)})
			if vm.HasPrev != tc.wantPrev {
				t.Errorf("HasPrev = %v，期望 %v", vm.HasPrev, tc.wantPrev)
			}
			if vm.HasNext != tc.wantNext {
				t.Errorf("HasNext = %v，期望 %v", vm.HasNext, tc.wantNext)
			}
			// 首页不应有"上一页"链接，否则用户点到 ?page=0
			got := html.UnescapeString(mustRender(t, "pagination", vm))
			if !tc.wantPrev && strings.Contains(got, "上一页") {
				t.Errorf("首页不该出现上一页链接: %s", got)
			}
		})
	}
}

// 搜索时分页链接必须带上搜索词，否则翻页就退化成普通浏览。
func TestPaginationKeepsSearch(t *testing.T) {
	vm := buildPageVM(ProblemListPage{Page: 2, TotalPage: 5, Search: "a b"})
	got := html.UnescapeString(mustRender(t, "pagination", vm))
	if !strings.Contains(got, "search=a+b") {
		t.Errorf("分页链接丢失搜索词: %s", got)
	}
	// 每个页码链接都要带搜索词，不能只有上一页/下一页带。
	if n := strings.Count(got, "search=a+b"); n != len(vm.Pages)+4 {
		t.Errorf("带搜索词的链接数 = %d，期望 %d（每个页码 + 首页/上一页/下一页/末页）: %s", n, len(vm.Pages)+4, got)
	}
}

// 翻页条要列出页码，而不是只能一页一页翻：中间页应显示当前页 ±4，
// 两端用首页/末页 + 省略号收口。
func TestPaginationShowsPageNumbers(t *testing.T) {
	vm := buildPageVM(ProblemListPage{Page: 10, TotalPage: 30, Total: 1500})
	got := html.UnescapeString(mustRender(t, "pagination", vm))

	for _, want := range []string{
		`href="problemset.php?page=1"`,
		`href="problemset.php?page=6"`,
		`href="problemset.php?page=14"`,
		`href="problemset.php?page=30"`,
		`class="active item" href="problemset.php?page=10"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("分页条缺少 %s: %s", want, got)
		}
	}
	for _, unwant := range []string{
		`href="problemset.php?page=5"`,
		`href="problemset.php?page=15"`,
	} {
		if strings.Contains(got, unwant) {
			t.Errorf("窗口外页码不应出现 %s: %s", unwant, got)
		}
	}
	if n := strings.Count(got, "…"); n != 2 {
		t.Errorf("省略号数量 = %d，期望 2: %s", n, got)
	}
}

// 首页不给“首页/上一页”，末页不给“下一页/末页”，避免点到越界页。
func TestPaginationEdges(t *testing.T) {
	first := html.UnescapeString(mustRender(t, "pagination",
		buildPageVM(ProblemListPage{Page: 1, TotalPage: 10})))
	if strings.Contains(first, "上一页") || strings.Contains(first, "«") {
		t.Errorf("首页不应出现上一页/首页链接: %s", first)
	}
	if !strings.Contains(first, `href="problemset.php?page=10"`) {
		t.Errorf("首页应有末页链接: %s", first)
	}

	last := html.UnescapeString(mustRender(t, "pagination",
		buildPageVM(ProblemListPage{Page: 10, TotalPage: 10})))
	if strings.Contains(last, "下一页") || strings.Contains(last, "»") {
		t.Errorf("末页不应出现下一页/末页链接: %s", last)
	}
}

// 越界页（?page=9999）没有数据可显示，但页码窗口要夹到末页，
// 否则用户看到一条只剩“上一页”的空白分页条。
func TestPaginationOutOfRangeShowsLastWindow(t *testing.T) {
	vm := buildPageVM(ProblemListPage{Page: 9999, TotalPage: 5})
	got := html.UnescapeString(mustRender(t, "pagination", vm))
	if !strings.Contains(got, `href="problemset.php?page=5"`) {
		t.Errorf("越界页应仍能跳到末页: %s", got)
	}
	if strings.Contains(got, `active item`) {
		t.Errorf("越界页没有真实页码可高亮: %s", got)
	}
}

// 题目列表页只能有一组分页：曾经 problemsetTable 与 problemset 各含一次，
// 页面底部出现两条相同的翻页条。
func TestProblemsetHasSinglePagination(t *testing.T) {
	vm := buildPageVM(ProblemListPage{Page: 1, TotalPage: 5})
	got := mustRender(t, "problemset", vm)
	if n := strings.Count(got, "ui pagination menu"); n != 1 {
		t.Errorf("problemset 分页块数 = %d，期望 1", n)
	}
}

// ── 工具函数 ──────────────────────────────────────────────────────────

// 标签配色必须与 PHP 的 md5 取模一致，否则切流后所有标签变色。
// 期望值由 PHP 的 md5(substr(md5($cat),0,7)) % 6 直接算出。
func TestThemeForMatchesPHP(t *testing.T) {
	tests := map[string]string{
		"Codeforces":             "default", // hash%6 = 5
		"acm":                    "primary", // hash%6 = 2
		"http://www.acwing.com/": "warning", // hash%6 = 1
		"洛谷":                     "default",
		"":                       "warning",
	}
	for cat, want := range tests {
		if got := themeFor(cat); got != want {
			t.Errorf("themeFor(%q) = %q，期望 %q（与 PHP 的 md5 取模不一致）", cat, got, want)
		}
	}
}

// 截断必须按 rune，不能按字节 —— 否则中文标签会被切出乱码。
func TestTruncateLabelByRune(t *testing.T) {
	if got := truncateLabel("abcdefghijklmnop", 10); got != "abcdefghij" {
		t.Errorf("got %q", got)
	}
	if got := truncateLabel("中文标签很长很长", 4); got != "中文标签" {
		t.Errorf("按字节截断会乱码，got %q", got)
	}
	if got := truncateLabel("短", 10); got != "短" {
		t.Errorf("短字符串被截断，got %q", got)
	}
}

func TestDomainOf(t *testing.T) {
	tests := map[string]string{
		"http://www.acwing.com/": "www.acwing.com",
		"https://example.com":    "example.com",
		"http://a.com/problem/1": "a.com",
		"http://a.com:8080/x":    "a.com",
		"Codeforces":             "Codeforces", // 非 URL 不变
		"http://x.com?search=a":  "x.com",
	}
	for in, want := range tests {
		if got := domainOf(in); got != want {
			t.Errorf("domainOf(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// PHP 的 urlencode 把空格编成 +；用 PathEscape 会编成 %20，行为不同。
func TestURLEncodeUsesPlusForSpace(t *testing.T) {
	if got := urlEncode("a b"); got != "a+b" {
		t.Errorf("urlEncode = %q，期望 a+b", got)
	}
	if got := urlEncode("A+B"); got != "A%2BB" {
		t.Errorf("urlEncode = %q，期望 A%%2BB（+ 必须转义，否则搜索词语义变了）", got)
	}
}

func TestNullIntStr(t *testing.T) {
	if got := nullIntStr(sql.NullInt32{}); got != "" {
		t.Errorf("NULL 应渲染成空串，got %q", got)
	}
	if got := nullIntStr(sql.NullInt32{Int32: 7, Valid: true}); got != "7" {
		t.Errorf("got %q", got)
	}
}

func TestTrimFloat(t *testing.T) {
	for in, want := range map[float64]string{1: "1", 1.5: "1.5", 2: "2", 0.5: "0.5"} {
		if got := trimFloat(in); got != want {
			t.Errorf("trimFloat(%v) = %q，期望 %q", in, got, want)
		}
	}
}

// fakeQuerier 需要 detail 字段与 err 注入，追加到已有的 fake 上。
func TestFakeSupportsDetail(t *testing.T) {
	// 占位断言：确保 fake 的新字段可用，避免后续加用例时才发现编译不过
	f := &fakeQuerier{detail: dbgen.Problem{Title: "t"}}
	if f.detail.Title != "t" {
		t.Error("fake detail 字段异常")
	}
	_ = context.Background
	_ = time.Now
}

// 样例输入/输出必须是题目文本本身，不能把 sql.NullString 结构体打出来。
//
// 曾经的 bug：模板写 `{{.SampleInput}}`，而字段是 sql.NullString，
// 页面会显示成 "{1 2\n true}" 这种结构体字面量。
func TestProblemDetailRendersSampleText(t *testing.T) {
	d := ProblemDetail{
		ProblemID:    1,
		Title:        "T",
		SampleInput:  sql.NullString{String: "1 2\n3 4\n", Valid: true},
		SampleOutput: sql.NullString{String: "3\n7\n", Valid: true},
		Source:       sql.NullString{String: "POJ", Valid: true},
	}
	got := mustRenderDetail(t, d)

	if !strings.Contains(got, "<pre>1 2\n3 4\n</pre>") {
		t.Errorf("样例输入应保留原文本与换行:\n%s", got)
	}
	if !strings.Contains(got, "<pre>3\n7\n</pre>") {
		t.Errorf("样例输出应保留原文本与换行:\n%s", got)
	}
	if strings.Contains(got, "{1 2") || strings.Contains(got, " true}") {
		t.Errorf("疑似把 sql.NullString 结构体打了出来:\n%s", got)
	}
	if !strings.Contains(got, "<div>POJ</div>") {
		t.Errorf("来源应显示文本:\n%s", got)
	}
}

// 题目标题只由 layout 渲染一次；时限/内存排在同一行、位于描述上方。
func TestProblemDetailTitleOnceAndMetaAboveDescription(t *testing.T) {
	d := ProblemDetail{
		ProblemID:   1,
		Title:       "标题X",
		Description: "<p>desc</p>",
		TimeLimit:   1,
		MemoryLimit: 128,
	}
	got := mustRenderDetail(t, d)

	if strings.Contains(got, "<h1") {
		t.Errorf("题面模板不应再输出 <h1>（标题由 layout 统一渲染）:\n%s", got)
	}

	ti := strings.Index(got, "Time Limit")
	mi := strings.Index(got, "Memory Limit")
	di := strings.Index(got, "problem-description")
	if ti < 0 || mi < 0 || di < 0 {
		t.Fatalf("缺少时限/内存/描述：ti=%d mi=%d di=%d\n%s", ti, mi, di, got)
	}
	if ti > di || mi > di {
		t.Errorf("时限/内存应在题目描述之上:\n%s", got)
	}
	// 同一行 = 同一个容器：两者之间不应出现结束标签。
	if strings.Contains(got[ti:mi], "</div>") {
		t.Errorf("时限与内存被拆到了不同行/容器:\n%s", got)
	}
}

// 特殊评测类型不能只显示数字：题面顶部要写清这是什么类型的题。
// spj=0（普通评测）不显示类型标签。
func TestProblemDetailJudgeTypeLabel(t *testing.T) {
	base := ProblemDetail{ProblemID: 1, Title: "T", TimeLimit: 1, MemoryLimit: 128}

	cases := []struct {
		spj  string
		want string
		tip  string
	}{
		{"1", "特殊评测（SPJ）", "评测程序"},
		{"2", "判断题（Raw Text）", "原始文本"},
		{"3", "交互题", "双向交互"},
		{"4", "通讯题（多轮交互）", "重复运行"},
	}
	for _, c := range cases {
		d := base
		d.SPJ = c.spj
		got := mustRenderDetail(t, d)
		if !strings.Contains(got, "评测类型："+c.want) {
			t.Errorf("spj=%s 应显示类型「%s」:\n%s", c.spj, c.want, got)
		}
		if !strings.Contains(got, c.tip) {
			t.Errorf("spj=%s 的类型说明应包含 %q:\n%s", c.spj, c.tip, got)
		}
		if strings.Contains(got, "Special Judge：") {
			t.Errorf("spj=%s 不应再显示裸数字的 Special Judge 标签:\n%s", c.spj, got)
		}
	}

	for _, spj := range []string{"", "0"} {
		d := base
		d.SPJ = spj
		if got := mustRenderDetail(t, d); strings.Contains(got, "评测类型：") {
			t.Errorf("spj=%q 为普通评测，不应显示类型标签:\n%s", spj, got)
		}
	}
}

// 需要 freopen 的题目（数据目录有 input.name/output.name）应在题面提示
// 读写文件名；没有时不应出现该提示。
func TestProblemDetailFreopenNotice(t *testing.T) {
	base := ProblemDetail{ProblemID: 1, Title: "T", TimeLimit: 1, MemoryLimit: 128}

	// 输入输出都固定。
	d := base
	d.InputFileName, d.OutputFileName = "sort.in", "sort.out"
	got := mustRenderDetail(t, d)
	for _, want := range []string{"freopen", "sort.in", "sort.out"} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少 %q:\n%s", want, got)
		}
	}

	// 只固定输入。
	d = base
	d.InputFileName = "a.in"
	if got := mustRenderDetail(t, d); !strings.Contains(got, "a.in") {
		t.Errorf("仅固定输入时应出现提示:\n%s", got)
	}

	// 没有文件读写：不出现提示。
	if got := mustRenderDetail(t, base); strings.Contains(got, "freopen") {
		t.Errorf("无文件读写时不应出现 freopen 提示:\n%s", got)
	}
}
