package web

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

func TestAdminRejudgeForm(t *testing.T) {
	f := &fakeQuerier{}
	rec := doProblem(t, f, seedAdmin(), http.MethodGet, "/admin/rejudge.php", nil, t.TempDir(), (*Server).handleAdminRejudge)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `name="rjpid"`) {
		t.Errorf("表单缺 rjpid")
	}
}

func TestAdminRejudgeProblem(t *testing.T) {
	f := &fakeQuerier{}
	form := url.Values{"rjpid": {"1001"}, "do": {"do"}}
	rec := doProblem(t, f, seedAdmin(), http.MethodPost, "/admin/rejudge.php", form, t.TempDir(), (*Server).handleAdminRejudge)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/status.php?problem_id=1001" {
		t.Fatalf("状态码/Location = %d/%q", rec.Code, rec.Header().Get("Location"))
	}
	if f.lastRejudgeProblem != 1001 || !containsCall(f.called, "ResetProblemAccepted") || !containsCall(f.called, "DeleteSimByProblem") {
		t.Errorf("重判题目应清理 solution/problem/sim: %v", f.called)
	}
}

func TestAdminRejudgeProblemZero(t *testing.T) {
	f := &fakeQuerier{}
	form := url.Values{"rjpid": {"0"}, "do": {"do"}}
	rec := doProblem(t, f, seedAdmin(), http.MethodPost, "/admin/rejudge.php", form, t.TempDir(), (*Server).handleAdminRejudge)
	if !strings.Contains(rec.Body.String(), "should not equal to 0") {
		t.Errorf("rjpid=0 应报错")
	}
	if containsCall(f.called, "RejudgeProblemSolutions") {
		t.Errorf("rjpid=0 不应改库")
	}
}

func TestAdminRejudgeSolutionNoContest(t *testing.T) {
	f := &fakeQuerier{solutionContestID: sql.NullInt32{Int32: 0, Valid: true}}
	form := url.Values{"rjsid": {"42"}, "do": {"do"}}
	rec := doProblem(t, f, seedAdmin(), http.MethodPost, "/admin/rejudge.php", form, t.TempDir(), (*Server).handleAdminRejudge)
	if rec.Header().Get("Location") != "/status.php?top=43" {
		t.Errorf("Location = %q", rec.Header().Get("Location"))
	}
	if f.rejudgedSID != 42 {
		t.Errorf("rejudgedSID = %d", f.rejudgedSID)
	}
}

func TestAdminRejudgeContest(t *testing.T) {
	f := &fakeQuerier{}
	form := url.Values{"rjcid": {"1000"}, "do": {"do"}}
	doProblem(t, f, seedAdmin(), http.MethodPost, "/admin/rejudge.php", form, t.TempDir(), (*Server).handleAdminRejudge)
	if f.lastRejudgeContest != 1000 {
		t.Errorf("lastRejudgeContest = %d", f.lastRejudgeContest)
	}
}

func TestAdminSolutionStatsForm(t *testing.T) {
	f := &fakeQuerier{recentContests50: []dbgen.ListRecentContests50Row{{ContestID: 1, Title: sql.NullString{String: "C", Valid: true}}}}
	rec := doProblem(t, f, seedAdmin(), http.MethodGet, "/admin/solution_statistics.php", nil, t.TempDir(), (*Server).handleAdminSolutionStatistics)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `name="pids"`) {
		t.Errorf("表单缺 pids")
	}
}

func TestAdminSolutionStatsPivot(t *testing.T) {
	f := &fakeQuerier{statMinUsers: []dbgen.SolutionMinByUsersRow{
		{UserID: "alice", Nick: "Alice", ProblemID: 1001, R: 4},
		{UserID: "bob", Nick: "Bob", ProblemID: 1002, R: 5},
	}}
	form := url.Values{"pids": {"1001,1002"}, "ulist": {"alice\nbob"}}
	rec := doProblem(t, f, seedAdmin(), http.MethodPost, "/admin/solution_statistics.php", form, t.TempDir(), (*Server).handleAdminSolutionStatistics)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"alice", "bob", ">AC<", ">WA<", "P1001", "P1002"} {
		if !strings.Contains(body, want) {
			t.Errorf("统计表缺少 %q", want)
		}
	}
}

func TestAdminSolutionStatsRequiresRole(t *testing.T) {
	f := &fakeQuerier{}
	rec := doProblem(t, f, "", http.MethodGet, "/admin/solution_statistics.php", nil, t.TempDir(), (*Server).handleAdminSolutionStatistics)
	if !strings.Contains(rec.Body.String(), "Please Login First!") {
		t.Errorf("匿名访问应被拒绝")
	}
}
