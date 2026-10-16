package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sempr/hustoj-go/internal/web/config"
	"github.com/sempr/hustoj-go/pkg/dbgen"
)

const showsourceSID = "showsourcetest1234567890ab"

// showsourceSeed 拼一段 PHP 会话文件内容（键名带 $OJ_NAME_ 前缀）。
func showsourceSeed(bits ...string) string {
	return strings.Join(bits, "")
}

// showsourceRequest 构造请求。session 非空时写一份 PHP 会话文件并带上 PHPSESSID。
func showsourceRequest(t *testing.T, f *fakeQuerier, mutate func(*config.Config), session, target string) *httptest.ResponseRecorder {
	t.Helper()
	dir := t.TempDir()
	if session != "" {
		if err := os.WriteFile(filepath.Join(dir, "sess_"+showsourceSID), []byte(session), 0600); err != nil {
			t.Fatal(err)
		}
	}
	srv := newTestServer(t, f, func(c *config.Config) {
		c.OJName = testOJName
		c.SessionBridge = true
		c.SessionDir = dir
		if mutate != nil {
			mutate(c)
		}
	})
	srv.sessionBridge = NewSessionBridge(dir, testOJName)
	if session != "" {
		// 命中 PHP 会话时 resolveIdentity 会把它接纳成 Go 会话，需要一个存储。
		srv.sessions = newTestSessionStore(t)
	}

	req := httptest.NewRequest(http.MethodGet, target, nil)
	if session != "" {
		req.AddCookie(&http.Cookie{Name: phpSessionCookieName, Value: showsourceSID})
	}
	rec := httptest.NewRecorder()
	srv.handleShowsource(rec, req)
	return rec
}

func baseShowsourceSolution() dbgen.GetSolutionByIDRow {
	return dbgen.GetSolutionByIDRow{
		SolutionID: 42,
		ProblemID:  1001,
		UserID:     "alice",
		Nick:       "Alice",
		Language:   1, // C++
		Result:     4,
		Time:       12,
		Memory:     2048,
	}
}

func TestShowsourceMissingID(t *testing.T) {
	body := showsourceRequest(t, &fakeQuerier{}, nil, "", "/showsource.php").Body.String()
	if !strings.Contains(body, "No such code!") {
		t.Errorf("缺少 No such code!\n%s", trunc(body, 600))
	}
}

func TestShowsourceOwnerCanView(t *testing.T) {
	f := &fakeQuerier{ssSolution: baseShowsourceSolution(), ssSource: "int main(){}"}
	session := showsourceSeed(testOJName + `_user_id|s:5:"alice";`)
	body := showsourceRequest(t, f, nil, session, "/showsource.php?id=42").Body.String()
	// 提交信息改在代码上方以表格展示；"+" 会被 html/template 转义成
	// "&#43;"（见设计文档 3.7），因此断言用不含 "+" 的前缀即可。
	for _, want := range []string{
		"int main(){}", "problem.php?id=1001", "alice", "[Alice]",
		"language-c", "highlight.min.js", "highlight-github.min.css",
		"正确", "12 ms", "2048 kb",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("缺少 %q\n%s", want, trunc(body, 1000))
		}
	}
	// 提交信息不再以注释形式贴在源码末尾。
	if strings.Contains(body, "/******") {
		t.Errorf("源码末尾不应再有提交信息注释\n%s", trunc(body, 1000))
	}
	if strings.Contains(body, "I am sorry") {
		t.Errorf("本人应能查看源码")
	}
}

func TestShowsourceAnonymousDenied(t *testing.T) {
	f := &fakeQuerier{ssSolution: baseShowsourceSolution(), ssSource: "int main(){}"}
	body := showsourceRequest(t, f, nil, "", "/showsource.php?id=42").Body.String()
	if !strings.Contains(body, "I am sorry, You could not view this code!") {
		t.Errorf("匿名访客不应看到源码\n%s", trunc(body, 800))
	}
	if strings.Contains(body, "int main(){}") {
		t.Errorf("匿名访客泄露了源码")
	}
}

// source_browser 可查看他人源码（showsource.php:114）。
func TestShowsourceSourceBrowserCanView(t *testing.T) {
	f := &fakeQuerier{ssSolution: baseShowsourceSolution(), ssSource: "int main(){}"}
	session := showsourceSeed(
		testOJName+`_user_id|s:3:"bob";`,
		testOJName+`_source_browser|b:1;`,
	)
	body := showsourceRequest(t, f, nil, session, "/showsource.php?id=42").Body.String()
	if !strings.Contains(body, "int main(){}") {
		t.Errorf("source_browser 应能查看源码\n%s", trunc(body, 800))
	}
}

// s<problem_id> 权限可查看（showsource.php:98-107）。
func TestShowsourceProblemPrivilegeCanView(t *testing.T) {
	f := &fakeQuerier{ssSolution: baseShowsourceSolution(), ssSource: "int main(){}", privileged: true}
	session := showsourceSeed(testOJName + `_user_id|s:3:"bob";`)
	body := showsourceRequest(t, f, nil, session, "/showsource.php?id=42").Body.String()
	if !strings.Contains(body, "int main(){}") {
		t.Errorf("持有 s<pid> 权限应能查看源码\n%s", trunc(body, 800))
	}
}

// 题目被进行中比赛锁榜时拒绝（showsource.php:55-72）。
func TestShowsourceLockedContestDenied(t *testing.T) {
	f := &fakeQuerier{ssSolution: baseShowsourceSolution(), ssLockedCID: 5, ssSource: "int main(){}"}
	body := showsourceRequest(t, f, nil, "", "/showsource.php?id=42").Body.String()
	if !strings.Contains(body, "作业比赛编号 : 5") || !strings.Contains(body, "考试期间") {
		t.Errorf("缺少锁榜拦截文案\n%s", trunc(body, 800))
	}
	if strings.Contains(body, "int main(){}") {
		t.Errorf("锁榜时不应返回源码")
	}
}

// 提交属于进行中的比赛时可查看，跳过锁榜检查（showsource.php:43-47）。
func TestShowsourceOngoingContestSkipsLockCheck(t *testing.T) {
	now := time.Now()
	f := &fakeQuerier{
		ssSolution:      baseShowsourceSolution(),
		ssContestWindow: dbgen.GetContestWindowRow{StartTime: nowArg(now.Add(-time.Hour)), EndTime: nowArg(now.Add(time.Hour))},
		ssLockedCID:     9,
	}
	sol := f.ssSolution
	sol.ContestID = sql.NullInt32{Int32: 5, Valid: true}
	f.ssSolution = sol
	body := showsourceRequest(t, f, nil, "", "/showsource.php?id=42").Body.String()
	if strings.Contains(body, "考试期间") {
		t.Errorf("进行中的比赛不应触发锁榜拦截\n%s", trunc(body, 800))
	}
	for _, name := range f.called {
		if name == "ProblemLockedContestID" {
			t.Errorf("进行中的比赛不应查询锁榜")
		}
	}
}

// 提交属于已结束的比赛时仍需检查是否被进行中比赛占用（showsource.php:39-41）。
func TestShowsourceEndedContestChecksLock(t *testing.T) {
	now := time.Now()
	f := &fakeQuerier{
		ssSolution:      baseShowsourceSolution(),
		ssContestWindow: dbgen.GetContestWindowRow{StartTime: nowArg(now.Add(-2 * time.Hour)), EndTime: nowArg(now.Add(-time.Hour))},
		ssLockedCID:     7,
	}
	sol := f.ssSolution
	sol.ContestID = sql.NullInt32{Int32: 5, Valid: true}
	f.ssSolution = sol
	body := showsourceRequest(t, f, nil, "", "/showsource.php?id=42").Body.String()
	if !strings.Contains(body, "作业比赛编号 : 7") {
		t.Errorf("已结束比赛应继续检查锁榜\n%s", trunc(body, 800))
	}
}

// administrator 豁免锁榜检查，但本身不授予源码查看权（showsource.php:30、:113-114）。
func TestShowsourceAdminExemptFromLockButNotViewer(t *testing.T) {
	f := &fakeQuerier{ssSolution: baseShowsourceSolution(), ssLockedCID: 5, ssSource: "int main(){}"}
	session := showsourceSeed(
		testOJName+`_user_id|s:3:"bob";`,
		testOJName+`_administrator|b:1;`,
	)
	body := showsourceRequest(t, f, nil, session, "/showsource.php?id=42").Body.String()
	if strings.Contains(body, "考试期间") {
		t.Errorf("管理员应豁免锁榜拦截\n%s", trunc(body, 800))
	}
	if !strings.Contains(body, "I am sorry") {
		t.Errorf("管理员无 s<pid> 时不应看到他人源码")
	}
}

// 考试模式：contest_id 小于阈值且非 source_browser 一律拦截（showsource.php:76-87）。
func TestShowsourceExamModeBlocks(t *testing.T) {
	f := &fakeQuerier{ssSolution: baseShowsourceSolution(), ssSource: "int main(){}"}
	body := showsourceRequest(t, f, func(c *config.Config) { c.ExamContestID = 100 }, "", "/showsource.php?id=42").Body.String()
	if !strings.Contains(body, "作业比赛编号 : 100+") {
		t.Errorf("考试模式缺少拦截文案\n%s", trunc(body, 800))
	}
}

// 考试模式下 source_browser 豁免。
func TestShowsourceExamModeSourceBrowserExempt(t *testing.T) {
	f := &fakeQuerier{ssSolution: baseShowsourceSolution(), ssSource: "int main(){}"}
	session := showsourceSeed(
		testOJName+`_user_id|s:3:"bob";`,
		testOJName+`_source_browser|b:1;`,
	)
	body := showsourceRequest(t, f, func(c *config.Config) { c.ExamContestID = 100 }, session, "/showsource.php?id=42").Body.String()
	if !strings.Contains(body, "int main(){}") {
		t.Errorf("source_browser 应豁免考试模式\n%s", trunc(body, 800))
	}
}

// 提交不存在时按 $row=false 处理：不 500，访客看到拒绝页。
func TestShowsourceMissingSolution(t *testing.T) {
	f := &fakeQuerier{err: sql.ErrNoRows}
	rec := showsourceRequest(t, f, nil, "", "/showsource.php?id=999")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "I am sorry") {
		t.Errorf("提交不存在时访客应被拒绝\n%s", trunc(rec.Body.String(), 800))
	}
}

func TestSourceBrush(t *testing.T) {
	tests := map[uint32]string{
		0:  "c",
		1:  "c++",
		2:  "delphi",
		9:  "c#",
		10: "c",
		11: "vb",
		13: "c",
		14: "c++",
		22: "delphi",
		24: "python",
	}
	for lang, want := range tests {
		if got := sourceBrush(lang); got != want {
			t.Errorf("sourceBrush(%d) = %q，期望 %q", lang, got, want)
		}
	}
}

// hljsLang 把 SyntaxHighlighter brush 名映射到 highlight.js 语言名。
func TestHljsLang(t *testing.T) {
	tests := map[string]string{
		"":           "",
		"c":          "c",
		"c++":        "cpp",
		"cpp":        "cpp",
		"c#":         "csharp",
		"obj-c":      "objectivec",
		"vb":         "vbnet",
		"pascal":     "delphi",
		"delphi":     "delphi",
		"python":     "python",
		"javascript": "javascript",
	}
	for brush, want := range tests {
		if got := hljsLang(brush); got != want {
			t.Errorf("hljsLang(%q) = %q，期望 %q", brush, got, want)
		}
	}
}
