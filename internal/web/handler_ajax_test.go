package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sempr/hustoj-go/internal/web/config"
	"github.com/sempr/hustoj-go/pkg/dbgen"
)

const ajaxSID = "ajaxtest1234567890abcdef"

// doAJAX 构造一次 AJAX 请求。session 非空时写 PHP 会话文件并带 PHPSESSID。
func doAJAX(t *testing.T, f *fakeQuerier, mutate func(*config.Config), session, target string,
	h func(*Server, http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	dir := t.TempDir()
	if session != "" {
		if err := os.WriteFile(filepath.Join(dir, "sess_"+ajaxSID), []byte(session), 0600); err != nil {
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
		srv.sessions = newTestSessionStore(t)
	}

	req := httptest.NewRequest(http.MethodGet, target, nil)
	if session != "" {
		req.AddCookie(&http.Cookie{Name: phpSessionCookieName, Value: ajaxSID})
	}
	rec := httptest.NewRecorder()
	h(srv, rec, req)
	return rec
}

func seedAdmin() string {
	return showsourceSeed(
		testOJName+`_user_id|s:5:"admin";`,
		testOJName+`_administrator|b:1;`,
	)
}

func seedAlice() string {
	return showsourceSeed(testOJName + `_user_id|s:5:"alice";`)
}

func baseStatusSolution() dbgen.Solution {
	return dbgen.Solution{
		SolutionID: 42,
		ProblemID:  1001,
		UserID:     "alice",
		Nick:       "Alice",
		Time:       12,
		Memory:     2048,
		Result:     4,
		Language:   1,
		Judger:     "judge0",
		PassRate:   1,
	}
}

// ── problem-ajax.php ────────────────────────────────────────────────

func TestProblemAjaxCreator(t *testing.T) {
	f := &fakeQuerier{paCreator: "alice"}
	rec := doAJAX(t, f, nil, "", "/problem-ajax.php?pid=1001", (*Server).handleProblemAjax)
	if got := rec.Body.String(); got != "<a href='userinfo.php?user=alice'>alice</a>" {
		t.Errorf("body = %q", got)
	}
	if f.paRightstr != "p1001" {
		t.Errorf("rightstr = %q，期望 p1001", f.paRightstr)
	}
}

func TestProblemAjaxImported(t *testing.T) {
	f := &fakeQuerier{err: sql.ErrNoRows}
	rec := doAJAX(t, f, nil, "", "/problem-ajax.php?pid=7", (*Server).handleProblemAjax)
	if got := rec.Body.String(); got != msgImported {
		t.Errorf("body = %q，期望 %q", got, msgImported)
	}
}

func TestProblemAjaxEscapesUser(t *testing.T) {
	f := &fakeQuerier{paCreator: "a<b"}
	rec := doAJAX(t, f, nil, "", "/problem-ajax.php?pid=1", (*Server).handleProblemAjax)
	if got := rec.Body.String(); !strings.Contains(got, "a&lt;b") {
		t.Errorf("用户名未转义: %q", got)
	}
}

// ── status-ajax.php ─────────────────────────────────────────────────

func TestStatusAjaxNoSolutionEchoesID(t *testing.T) {
	f := &fakeQuerier{err: sql.ErrNoRows}
	rec := doAJAX(t, f, nil, "", "/status-ajax.php?solution_id=999", (*Server).handleStatusAjax)
	if got := rec.Body.String(); got != "9990, 0, 0,unknown,0" {
		t.Errorf("body = %q", got)
	}
}

func TestStatusAjaxPublicResult(t *testing.T) {
	f := &fakeQuerier{saSolution: baseStatusSolution()}
	rec := doAJAX(t, f, nil, "", "/status-ajax.php?solution_id=42", (*Server).handleStatusAjax)
	want := "4,2048 KB,12 ms,none,100,alice"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q，期望 %q", got, want)
	}
}

func TestStatusAjaxAdminSeesJudger(t *testing.T) {
	f := &fakeQuerier{saSolution: baseStatusSolution()}
	rec := doAJAX(t, f, nil, seedAdmin(), "/status-ajax.php?solution_id=42", (*Server).handleStatusAjax)
	if got := rec.Body.String(); !strings.Contains(got, ",judge0,") {
		t.Errorf("管理员应看到判题机: %q", got)
	}
}

func TestStatusAjaxPassRate(t *testing.T) {
	sol := baseStatusSolution()
	sol.Result = 6
	sol.PassRate = 0.5
	f := &fakeQuerier{saSolution: sol}
	rec := doAJAX(t, f, nil, "", "/status-ajax.php?solution_id=42", (*Server).handleStatusAjax)
	if got := rec.Body.String(); got != "6,2048 KB,12 ms,none,50,alice" {
		t.Errorf("body = %q", got)
	}
}

// 关闭 $OJ_PUBLIC_STATUS 后，匿名访客只能看到 NOIP 警告。
func TestStatusAjaxHiddenWhenNotPublic(t *testing.T) {
	f := &fakeQuerier{saSolution: baseStatusSolution()}
	rec := doAJAX(t, f, func(c *config.Config) { c.PublicStatus = false }, "",
		"/status-ajax.php?solution_id=42", (*Server).handleStatusAjax)
	want := "noip 比赛进行中，结束后才能查看结果。"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %q，期望 %q", got, want)
	}
}

// 关闭公开状态后，本人仍可看到自己的结果。
func TestStatusAjaxOwnerSeesWhenHidden(t *testing.T) {
	f := &fakeQuerier{saSolution: baseStatusSolution()}
	rec := doAJAX(t, f, func(c *config.Config) { c.PublicStatus = false }, seedAlice(),
		"/status-ajax.php?solution_id=42", (*Server).handleStatusAjax)
	if got := rec.Body.String(); got != "4,2048 KB,12 ms,none,100,alice" {
		t.Errorf("本人应能看到自己的结果: %q", got)
	}
}

// 进行中 NOIP 比赛里的提交只回关键字，不泄露结果。
func TestStatusAjaxNoipMask(t *testing.T) {
	sol := baseStatusSolution()
	sol.ContestID = sql.NullInt32{Int32: 5, Valid: true}
	f := &fakeQuerier{
		saSolution: sol,
		saContestTitle: dbgen.GetContestTitleTypeRow{
			Title: sql.NullString{String: "NOIP2024", Valid: true},
		},
	}
	rec := doAJAX(t, f, nil, "", "/status-ajax.php?solution_id=42", (*Server).handleStatusAjax)
	if got := rec.Body.String(); got != "noip" {
		t.Errorf("body = %q，期望 noip", got)
	}
}

func TestStatusAjaxContestTypeMask(t *testing.T) {
	sol := baseStatusSolution()
	sol.ContestID = sql.NullInt32{Int32: 5, Valid: true}
	f := &fakeQuerier{
		saSolution: sol,
		saContestTitle: dbgen.GetContestTitleTypeRow{
			Title:       sql.NullString{String: "普通比赛", Valid: true},
			ContestType: sql.NullInt16{Int16: 16, Valid: true},
		},
	}
	rec := doAJAX(t, f, nil, "", "/status-ajax.php?solution_id=42", (*Server).handleStatusAjax)
	if got := rec.Body.String(); got != "noip" {
		t.Errorf("contest_type & 16 应触发屏蔽: %q", got)
	}
}

func TestStatusAjaxQueryUser(t *testing.T) {
	f := &fakeQuerier{saSolution: baseStatusSolution()}
	rec := doAJAX(t, f, nil, "", "/status-ajax.php?q=user_id&solution_id=42", (*Server).handleStatusAjax)
	if got := rec.Body.String(); got != "alice[Alice]" {
		t.Errorf("body = %q", got)
	}
}

func TestStatusAjaxCompileError(t *testing.T) {
	sol := baseStatusSolution()
	sol.Result = 11
	f := &fakeQuerier{
		saSolution:     sol,
		saCompileError: sql.NullString{String: "main.cpp:1: error", Valid: true},
	}
	rec := doAJAX(t, f, nil, seedAlice(), "/status-ajax.php?tr=1&solution_id=42", (*Server).handleStatusAjax)
	if got := rec.Body.String(); got != "main.cpp:1: error" {
		t.Errorf("body = %q", got)
	}
	if !containsCall(f.called, "DeleteCustomInput") {
		t.Errorf("查看详情后应清理 custominput，调用序列: %v", f.called)
	}
}

func TestStatusAjaxErrorClassification(t *testing.T) {
	sol := baseStatusSolution()
	sol.Result = 11
	f := &fakeQuerier{
		saSolution:     sol,
		saCompileError: sql.NullString{String: "judge crashed", Valid: true},
	}
	rec := doAJAX(t, f, nil, seedAlice(), "/status-ajax.php?tr=1&solution_id=42", (*Server).handleStatusAjax)
	if got := rec.Body.String(); got != "error1" {
		t.Errorf("body = %q，期望 error1", got)
	}
}

func TestStatusAjaxRuntimeError(t *testing.T) {
	sol := baseStatusSolution()
	sol.Result = 10
	f := &fakeQuerier{
		saSolution:     sol,
		saRuntimeError: sql.NullString{String: "segfault", Valid: true},
	}
	rec := doAJAX(t, f, nil, seedAlice(), "/status-ajax.php?tr=1&solution_id=42", (*Server).handleStatusAjax)
	if got := rec.Body.String(); got != "segfault" {
		t.Errorf("body = %q", got)
	}
}

// spj==2 且隐藏答案时，本人也不能看到运行详情。
func TestStatusAjaxSPJHidesAnswer(t *testing.T) {
	sol := baseStatusSolution()
	sol.Result = 10
	f := &fakeQuerier{
		saSolution:     sol,
		spj:            "2",
		saRuntimeError: sql.NullString{String: "secret", Valid: true},
	}
	rec := doAJAX(t, f, nil, seedAlice(), "/status-ajax.php?tr=1&solution_id=42", (*Server).handleStatusAjax)
	if got := rec.Body.String(); got != msgWarningAccessDenied {
		t.Errorf("body = %q", got)
	}
	if containsCall(f.called, "DeleteCustomInput") {
		t.Errorf("拒绝访问时不应清理 custominput")
	}
}

// 无详情行时什么都不输出，也不清理 custominput。
func TestStatusAjaxNoDetailRow(t *testing.T) {
	sol := baseStatusSolution()
	sol.Result = 11
	f := &fakeQuerier{saSolution: sol, saCompileErr: sql.ErrNoRows}
	rec := doAJAX(t, f, nil, seedAlice(), "/status-ajax.php?tr=1&solution_id=42", (*Server).handleStatusAjax)
	if got := rec.Body.String(); got != "" {
		t.Errorf("body = %q，期望空", got)
	}
	if containsCall(f.called, "DeleteCustomInput") {
		t.Errorf("无详情行时不应清理 custominput")
	}
}

// 非本人带 tr=1 时不走详情分支，落到公开结果。
func TestStatusAjaxTrIgnoredForStranger(t *testing.T) {
	f := &fakeQuerier{saSolution: baseStatusSolution()}
	rec := doAJAX(t, f, nil, "", "/status-ajax.php?tr=1&solution_id=42", (*Server).handleStatusAjax)
	if got := rec.Body.String(); got != "4,2048 KB,12 ms,none,100,alice" {
		t.Errorf("body = %q", got)
	}
}

func containsCall(calls []string, name string) bool {
	for _, c := range calls {
		if c == name {
			return true
		}
	}
	return false
}
