package web

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

func TestAdminHelp(t *testing.T) {
	f := &fakeQuerier{
		avgJudgeDelay:  float64(2),
		problemsCount:  3,
		usersCount:     4,
		solutionsCount: 5,
	}
	rec := doProblem(t, f, seedAdmin(), http.MethodGet, "/admin/help.php", nil, t.TempDir(), (*Server).handleAdminHelp)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"延迟", "/admin/problem_list.php", "/admin/contest_list.php", "/admin/rejudge.php"} {
		if !strings.Contains(body, want) {
			t.Errorf("落地页缺少 %q", want)
		}
	}
	for _, want := range []string{"题目：3", "用户：4", "提交：5"} {
		if !strings.Contains(body, want) {
			t.Errorf("系统计数缺少 %q", want)
		}
	}
}

func TestAdminHelpRequiresRole(t *testing.T) {
	f := &fakeQuerier{}
	rec := doProblem(t, f, "", http.MethodGet, "/admin/help.php", nil, t.TempDir(), (*Server).handleAdminHelp)
	if !strings.Contains(rec.Body.String(), "Please Login First!") {
		t.Errorf("匿名访问应被拒绝")
	}
}

func TestAdminSuspectList(t *testing.T) {
	f := &fakeQuerier{
		contest: dbgen.GetContestByIDRow{ContestID: 1000},
		suspectByIP: []dbgen.SuspectByIPRow{
			{Ip: "1.2.3.4", UserID: "alice", InDate: time.Now(), C: 2},
		},
		suspectByUser: []dbgen.SuspectUsersByIPRow{
			{UserID: "bob", Ip: sql.NullString{String: "5.6.7.8", Valid: true}, Time: sql.NullTime{Time: time.Now(), Valid: true}, C: 3},
		},
	}
	rec := doProblem(t, f, seedAdmin(), http.MethodGet, "/admin/suspect_list.php?cid=1000", nil, t.TempDir(), (*Server).handleAdminSuspectList)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"1.2.3.4", "alice", "bob", "5.6.7.8", "status.php?cid=1000&user_id=alice"} {
		if !strings.Contains(body, want) {
			t.Errorf("查重页缺少 %q", want)
		}
	}
}
