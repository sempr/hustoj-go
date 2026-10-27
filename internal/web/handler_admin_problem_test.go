package web

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

func TestAdminProblemListRendersRows(t *testing.T) {
	f := &fakeQuerier{
		problemsCount: 1,
		problemsAdminList: []dbgen.ListProblemsAdminListRow{
			{
				ProblemID: 1001, Title: "T1", Accepted: sql.NullInt32{Int32: 5, Valid: true},
				Defunct: "N", Source: sql.NullString{String: "tag1", Valid: true}, Coin: 3,
			},
		},
	}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/problem_list.php", nil, (*Server).handleAdminProblemList)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"1001", "T1", "tag1", "可用", `fd="coin"`, "problem_df_change.php?id=1001", "problem_edit.php?id=1001"} {
		if !strings.Contains(body, want) {
			t.Errorf("列表缺少 %q", want)
		}
	}
}

func TestAdminProblemListRequiresRole(t *testing.T) {
	f := &fakeQuerier{}
	rec := doNews(t, f, "", http.MethodGet, "/admin/problem_list.php", nil, (*Server).handleAdminProblemList)
	if !strings.Contains(rec.Body.String(), "Please Login First!") {
		t.Errorf("匿名访问应被拒绝")
	}
}

func TestAdminProblemListSearch(t *testing.T) {
	f := &fakeQuerier{
		problemsCount:  100,
		problemsSearch: []dbgen.SearchProblemsAdminListRow{{ProblemID: 2001, Title: "Hit", Defunct: "N"}},
	}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/problem_list.php?keyword=Hit", nil, (*Server).handleAdminProblemList)
	body := rec.Body.String()
	if !strings.Contains(body, "2001") {
		t.Errorf("搜索结果应包含命中行")
	}
	if strings.Contains(body, "go to page") {
		t.Errorf("带关键词时不应分页")
	}
	if !containsCall(f.called, "SearchProblemsAdminList") {
		t.Errorf("应走搜索查询")
	}
}

func TestAdminProblemDefunctChangeToggle(t *testing.T) {
	f := &fakeQuerier{problemDefunct: "N"}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/problem_df_change.php?id=1001", nil, (*Server).handleAdminProblemDefunctChange)
	if rec.Code != http.StatusFound {
		t.Fatalf("状态码 = %d, 期望 302", rec.Code)
	}
	if f.lastSetProblemDefunct.ProblemID != 1001 || f.lastSetProblemDefunct.Defunct != "Y" {
		t.Errorf("切换参数 = %+v", f.lastSetProblemDefunct)
	}
}

func TestAdminProblemDefunctChangeBatch(t *testing.T) {
	f := &fakeQuerier{}
	form := url.Values{"pid": {"1001", "1002"}, "enable": {""}, "hlist": {"1002,1003,abc"}}
	rec := doNews(t, f, seedAdmin(), http.MethodPost, "/admin/problem_df_change.php", form, (*Server).handleAdminProblemDefunctChange)
	if rec.Code != http.StatusFound {
		t.Fatalf("状态码 = %d, 期望 302", rec.Code)
	}
	if f.lastSetProblemDefunct.Defunct != "N" {
		t.Errorf("enable 应置 'N'")
	}
	// 1003 是 hlist 里最后一个有效 id，去重后按序应为 ...1003
	if f.lastSetProblemDefunct.ProblemID != 1003 {
		t.Errorf("最后一个应处理 1003, 实际 %d", f.lastSetProblemDefunct.ProblemID)
	}
}

func TestAdminProblemDel(t *testing.T) {
	f := &fakeQuerier{}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/problem_del.php?id=1001", nil, (*Server).handleAdminProblemDel)
	if rec.Code != http.StatusFound {
		t.Fatalf("状态码 = %d, 期望 302", rec.Code)
	}
	if !containsCall(f.called, "DeleteProblemByID") || !containsCall(f.called, "DeleteProblemPrivilege") || !containsCall(f.called, "UpdateSolutionProblemToZero") {
		t.Errorf("删除应清理 problem/privilege/solution: %v", f.called)
	}
}

func TestAdminProblemChangeIDTargetExists(t *testing.T) {
	f := &fakeQuerier{problemIDExistsCount: 1}
	form := url.Values{"from": {"1001"}, "to": {"1002"}, "do": {"do"}}
	rec := doNews(t, f, seedAdmin(), http.MethodPost, "/admin/problem_changeid.php", form, (*Server).handleAdminProblemChangeID)
	if !strings.Contains(rec.Body.String(), "fail...") {
		t.Errorf("目标题号已存在应失败")
	}
	if containsCall(f.called, "UpdateProblemID") {
		t.Errorf("失败时不应改库")
	}
}
