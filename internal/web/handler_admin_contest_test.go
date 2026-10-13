package web

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

func TestAdminContestListRendersRows(t *testing.T) {
	f := &fakeQuerier{
		contestsCount: 1,
		contestsAdminList: []dbgen.ListContestsAdminRow{
			{ContestID: 1, Title: sql.NullString{String: "C1", Valid: true}, Defunct: "N", Private: 0},
		},
	}
	rec := doProblem(t, f, seedAdmin(), http.MethodGet, "/admin/contest_list.php", nil, t.TempDir(), (*Server).handleAdminContestList)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"C1", "contest_edit.php?cid=1", "contest_pr_change.php?cid=1", "contest_df_change.php?cid=1", "公开", "可用"} {
		if !strings.Contains(body, want) {
			t.Errorf("列表缺少 %q", want)
		}
	}
}

func TestAdminContestListRequiresRole(t *testing.T) {
	f := &fakeQuerier{}
	rec := doProblem(t, f, "", http.MethodGet, "/admin/contest_list.php", nil, t.TempDir(), (*Server).handleAdminContestList)
	if !strings.Contains(rec.Body.String(), "Please Login First!") {
		t.Errorf("匿名访问应被拒绝")
	}
}

func TestAdminContestDefunctChange(t *testing.T) {
	f := &fakeQuerier{contestDefunct: "N"}
	rec := doProblem(t, f, seedAdmin(), http.MethodGet, "/admin/contest_df_change.php?cid=1", nil, t.TempDir(), (*Server).handleAdminContestDefunctChange)
	if rec.Code != http.StatusFound {
		t.Fatalf("状态码 = %d, 期望 302", rec.Code)
	}
	if f.lastSetContestDefunct.ContestID != 1 || f.lastSetContestDefunct.Defunct != "Y" {
		t.Errorf("切换参数 = %+v", f.lastSetContestDefunct)
	}
}

func TestAdminContestPrivateChange(t *testing.T) {
	f := &fakeQuerier{contestPrivate: 0}
	doProblem(t, f, seedAdmin(), http.MethodGet, "/admin/contest_pr_change.php?cid=1", nil, t.TempDir(), (*Server).handleAdminContestPrivateChange)
	if f.lastSetContestPrivate.ContestID != 1 || f.lastSetContestPrivate.Private != 1 {
		t.Errorf("切换参数 = %+v", f.lastSetContestPrivate)
	}
}

func TestAdminContestAdd(t *testing.T) {
	f := &fakeQuerier{nextContestID: 5}
	form := url.Values{
		"startdate": {"2030-01-01"}, "shour": {"10"}, "sminute": {"00"},
		"enddate": {"2030-01-02"}, "ehour": {"12"}, "eminute": {"30"},
		"title": {"MyContest"}, "private": {"1"}, "password": {"pw"}, "subnet": {"0.0.0.0/0"},
		"description": {"desc"}, "cproblem": {"1000,1001"}, "ulist": {"alice\nbob"},
	}
	rec := doProblem(t, f, seedAdmin(), http.MethodPost, "/admin/contest_add.php", form, t.TempDir(), (*Server).handleAdminContestAdd)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/contest_list.php" {
		t.Fatalf("状态码/Location = %d/%q", rec.Code, rec.Header().Get("Location"))
	}
	got := f.lastInsertContest
	// 未选任何语言 → langmask = 全部位 = (1<<len(languageExt))-1。
	wantMask := uint32(1)<<uint(len(languageExt)) - 1
	if got.Title.String != "MyContest" || got.Private != 1 || got.Langmask != wantMask {
		t.Errorf("InsertContest 参数 = %+v (langmask 期望 %d)", got, wantMask)
	}
	if !containsCall(f.called, "InsertContestProblem") || !containsCall(f.called, "InsertPrivilege") {
		t.Errorf("应挂题并写权限: %v", f.called)
	}
	if f.lastInsertContest.StartTime.Time.Format("2006-01-02 15:04:05") != "2030-01-01 10:00:00" {
		t.Errorf("start_time = %v", f.lastInsertContest.StartTime.Time)
	}
}

func TestAdminContestEditPost(t *testing.T) {
	f := &fakeQuerier{}
	form := url.Values{
		"cid": {"7"}, "startdate": {"2030-01-01"}, "shour": {"10"}, "sminute": {"00"},
		"enddate": {"2030-01-02"}, "ehour": {"12"}, "eminute": {"30"},
		"title": {"E"}, "private": {"0"}, "description": {"d"}, "cproblem": {"1000"}, "ulist": {""},
	}
	rec := doProblem(t, f, seedAdmin(), http.MethodPost, "/admin/contest_edit.php", form, t.TempDir(), (*Server).handleAdminContestEdit)
	if rec.Code != http.StatusFound {
		t.Fatalf("状态码 = %d, 期望 302", rec.Code)
	}
	if f.lastUpdateContest.ContestID != 7 {
		t.Errorf("UpdateContest.ContestID = %d", f.lastUpdateContest.ContestID)
	}
	if !containsCall(f.called, "ResetSolutionNum") || !containsCall(f.called, "SetSolutionNum") {
		t.Errorf("编辑应重排题号: %v", f.called)
	}
}

func TestAdminContestEditGet(t *testing.T) {
	f := &fakeQuerier{
		contest:              dbgen.GetContestByIDRow{ContestID: 7, Title: sql.NullString{String: "E", Valid: true}},
		contestUserIDs:       []string{"alice"},
		contestProblemIDsOrd: []int32{1000, 1001},
	}
	rec := doProblem(t, f, seedAdmin(), http.MethodGet, "/admin/contest_edit.php?cid=7", nil, t.TempDir(), (*Server).handleAdminContestEdit)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"1000,1001", "alice", `name="title"`} {
		if !strings.Contains(body, want) {
			t.Errorf("编辑表单缺少 %q", want)
		}
	}
}
