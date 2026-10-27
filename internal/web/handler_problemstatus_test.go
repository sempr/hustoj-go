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

// psSolution 构造一条榜单行，便于测试里只覆盖关心的字段。
func psSolution(id int32, user string, lang uint32, ms, kb, cl int32) dbgen.ProblemBestACSolRow {
	return dbgen.ProblemBestACSolRow{
		Att:        1,
		UserID:     user,
		SolutionID: id,
		Language:   lang,
		Time:       ms,
		Memory:     kb,
		CodeLength: cl,
		InDate:     time.Date(2026, 5, 13, 19, 24, 0, 0, time.Local),
	}
}

func getProblemStatus(t *testing.T, f *fakeQuerier, target string) *httptest.ResponseRecorder {
	t.Helper()
	srv := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	srv.handleProblemStatus(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func TestProblemStatusRendersStatsAndBoard(t *testing.T) {
	f := &fakeQuerier{
		psUsers:   3,
		psACUsers: 2,
		psResults: []dbgen.GetProblemResultStatsRow{
			{Result: 4, Cnt: 2},
			{Result: 6, Cnt: 1},
		},
		psSolutions: []dbgen.ProblemBestACSolRow{
			psSolution(9001, "alice", 1, 12, 2048, 399),
			psSolution(9002, "bob", 1, 5, 1024, 200),
		},
	}
	rec := getProblemStatus(t, f, "/problemstatus.php?id=1001")
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"提交记录", "用户(提交)", "用户(解决)",
		"正确", "答案错误", // result 4 与 6 的标签（对齐 PHP $judge_result）
		"alice", "bob", "userinfo.php?user=alice",
		"2048 KiB", "12 ms", "399 bytes",
		"2026-05-13 19:24:00",
		"主页",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("缺少 %q\n%s", want, trunc(body, 1200))
		}
	}
	if strings.Contains(body, "------") {
		t.Errorf("题目未被比赛占用时不应出现 ------ 占位")
	}
}

// 题目被进行中的比赛占用时，内存/耗时/代码长度列显示 ------（problemstatus.php:174-188）。
func TestProblemStatusRunningContestHidesMetrics(t *testing.T) {
	f := &fakeQuerier{
		psACUsers:   1,
		psSolutions: []dbgen.ProblemBestACSolRow{psSolution(9001, "alice", 1, 12, 2048, 399)},
		psInRunning: true,
	}
	body := getProblemStatus(t, f, "/problemstatus.php?id=1001").Body.String()
	if strings.Count(body, "------") != 3 {
		t.Errorf("应有 3 个 ------ 占位，实际 %d\n%s", strings.Count(body, "------"), trunc(body, 800))
	}
	if strings.Contains(body, "2048 KiB") {
		t.Errorf("比赛进行中不应泄露内存数值")
	}
}

// NOIP 比赛进行中直接拦截，且不应再查榜单（problemstatus.php:34-44）。
func TestProblemStatusNOIPIntercepts(t *testing.T) {
	f := &fakeQuerier{psNOIP: 1}
	body := getProblemStatus(t, f, "/problemstatus.php?id=1001").Body.String()
	if !strings.Contains(body, "noip 比赛进行中，结束后才能查看结果。") {
		t.Errorf("缺少 NOIP 拦截文案\n%s", trunc(body, 800))
	}
	for _, name := range f.called {
		if name == "ListProblemBestACSolutions" {
			t.Errorf("NOIP 拦截后不应查询榜单")
		}
	}
}

// 只有 URL 带 id 参数时才做相关推荐（problemstatus.php:237）。
func TestProblemStatusRecommendsOnlyWithIDParam(t *testing.T) {
	f := &fakeQuerier{psSource: sql.NullString{String: "UVA", Valid: true}, psProblemIDs: []int32{1002, 1003}}
	if body := getProblemStatus(t, f, "/problemstatus.php").Body.String(); strings.Contains(body, "相关题目") {
		t.Errorf("无 id 参数时不应出现相关题目\n%s", trunc(body, 800))
	}
	for _, name := range f.called {
		if name == "ListProblemIDsBySource" {
			t.Errorf("无 id 参数时不应调用 ListProblemIDsBySource")
		}
	}

	f2 := &fakeQuerier{psSource: sql.NullString{String: "UVA", Valid: true}, psProblemIDs: []int32{1002, 1003}}
	body := getProblemStatus(t, f2, "/problemstatus.php?id=1001").Body.String()
	if !strings.Contains(body, "相关题目") || !strings.Contains(body, "problem.php?id=1002") {
		t.Errorf("带 id 参数时应渲染相关题目\n%s", trunc(body, 800))
	}
}

// 题目不存在时 GetProblemSource 返回 ErrNoRows，PHP 会当成 null，推荐列表为空但不报错。
func TestProblemStatusRecommendMissingProblem(t *testing.T) {
	f := &fakeQuerier{}
	f.err = sql.ErrNoRows
	srv := newTestServer(t, f, nil)
	rec := httptest.NewRecorder()
	srv.handleProblemStatus(rec, httptest.NewRequest(http.MethodGet, "/problemstatus.php?id=1001", nil))
	if rec.Code != http.StatusInternalServerError {
		// Count 查询先失败，整页 500；这条只锁错误路径不会 panic。
		t.Fatalf("状态码 = %d，期望 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "页面暂时不可用") {
		t.Errorf("错误响应缺少文案: %q", rec.Body.String())
	}
}

// 语言 id 越界时渲染空串而不是崩溃（PHP undefined index）。
func TestProblemStatusLanguageNameOutOfRange(t *testing.T) {
	if got := problemStatusLanguageName(999); got != "" {
		t.Errorf("越界语言 = %q，期望空串", got)
	}
	if got := problemStatusLanguageName(1); got != "C++" {
		t.Errorf("语言 1 = %q，期望 C++", got)
	}
}
