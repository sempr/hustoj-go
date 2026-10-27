package web

import (
	"database/sql"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// seedProblemEditor / seedContestCreator 供内联编辑的权限分支测试使用。
func seedProblemEditor() string {
	return showsourceSeed(
		testOJName+`_user_id|s:5:"pedit";`,
		testOJName+`_problem_editor|b:1;`,
	)
}
func seedContestCreator() string {
	return showsourceSeed(
		testOJName+`_user_id|s:5:"creat";`,
		testOJName+`_contest_creator|b:1;`,
	)
}

func TestAdminAjaxForbiddenForAnonymous(t *testing.T) {
	f := &fakeQuerier{}
	rec := doNews(t, f, "", http.MethodPost, "/admin/ajax.php", url.Values{"m": {"user_update_nick"}}, (*Server).handleAdminAjax)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("状态码 = %d, 期望 403", rec.Code)
	}
}

func TestAdminAjaxUserUpdateNick(t *testing.T) {
	f := &fakeQuerier{}
	form := url.Values{"m": {"user_update_nick"}, "user_id": {"alice"}, "nick": {"NewName"}}
	doNews(t, f, seedAdmin(), http.MethodPost, "/admin/ajax.php", form, (*Server).handleAdminAjax)
	if f.lastSetUserNick.UserID != "alice" || f.lastSetUserNick.Nick != "NewName" {
		t.Errorf("SetUserNick 参数 = %+v", f.lastSetUserNick)
	}
	if !containsCall(f.called, "UpdateUserSolutionNick") {
		t.Errorf("改昵称应同步 solution.nick")
	}
}

func TestAdminAjaxUserUpdateFields(t *testing.T) {
	cases := []struct {
		m      string
		field  string
		value  string
		verify func(*fakeQuerier) bool
	}{
		{"user_update_school", "school", "SchoolX", func(f *fakeQuerier) bool { return f.lastSetUserSchool.School == "SchoolX" }},
		{"user_update_group_name", "group_name", "G1", func(f *fakeQuerier) bool { return f.lastSetUserGroup.GroupName == "G1" }},
		{"user_update_coin_bonus", "coin_bonus", "7", func(f *fakeQuerier) bool { return f.lastSetUserCoinBonus.CoinBonus == 7 }},
		{"user_update_coin_spent", "coin_spent", "3", func(f *fakeQuerier) bool { return f.lastSetUserCoinSpent.CoinSpent == 3 }},
	}
	for _, tc := range cases {
		t.Run(tc.m, func(t *testing.T) {
			f := &fakeQuerier{}
			form := url.Values{"m": {tc.m}, "user_id": {"alice"}, tc.field: {tc.value}}
			doNews(t, f, seedAdmin(), http.MethodPost, "/admin/ajax.php", form, (*Server).handleAdminAjax)
			if !tc.verify(f) {
				t.Errorf("%s 未按预期写入", tc.m)
			}
		})
	}
}

func TestAdminAjaxUserUpdateExpiryDate(t *testing.T) {
	f := &fakeQuerier{}
	form := url.Values{"m": {"user_update_expiry_date"}, "user_id": {"alice"}, "expiry_date": {"2030-05-06"}}
	doNews(t, f, seedAdmin(), http.MethodPost, "/admin/ajax.php", form, (*Server).handleAdminAjax)
	if f.lastSetUserExpiry.UserID != "alice" || f.lastSetUserExpiry.ExpiryDate.Format("2006-01-02") != "2030-05-06" {
		t.Errorf("SetUserExpiryDate 参数 = %+v", f.lastSetUserExpiry)
	}
}

func TestAdminAjaxParentPhoneInvalid(t *testing.T) {
	f := &fakeQuerier{}
	form := url.Values{"m": {"user_update_parent_phone"}, "user_id": {"alice"}, "parent_phone": {"123"}}
	rec := doNews(t, f, seedAdmin(), http.MethodPost, "/admin/ajax.php", form, (*Server).handleAdminAjax)
	if !strings.Contains(rec.Body.String(), "ERR: invalid phone") {
		t.Errorf("非法手机号应被拒")
	}
	if containsCall(f.called, "SetUserParentPhone") {
		t.Errorf("非法手机号不应写库")
	}
}

func TestAdminAjaxNewsImportance(t *testing.T) {
	f := &fakeQuerier{}
	form := url.Values{"m": {"news_update_importance"}, "news_id": {"5"}, "importance": {"3"}}
	doNews(t, f, seedAdmin(), http.MethodPost, "/admin/ajax.php", form, (*Server).handleAdminAjax)
	if f.lastSetNewsImportance.NewsID != 5 || f.lastSetNewsImportance.Importance != 3 {
		t.Errorf("SetNewsImportance 参数 = %+v", f.lastSetNewsImportance)
	}
}

func TestAdminAjaxProblemGetTitle(t *testing.T) {
	f := &fakeQuerier{problemTitleSource: dbgen.GetProblemTitleSourceRow{
		Title:  "Hello",
		Source: sql.NullString{String: "src", Valid: true},
	}}
	form := url.Values{"m": {"problem_get_title"}, "pid": {"1001"}}
	rec := doNews(t, f, seedProblemEditor(), http.MethodPost, "/admin/ajax.php", form, (*Server).handleAdminAjax)
	body := rec.Body.String()
	if !strings.Contains(body, "Hello") || !strings.Contains(body, "src") {
		t.Errorf("应回显标题与来源, 实际: %q", body)
	}
}

func TestAdminAjaxProblemSetSourceUnique(t *testing.T) {
	f := &fakeQuerier{}
	form := url.Values{"m": {"problem_set_source"}, "pid": {"1001"}, "ns": {"a  a b"}}
	doNews(t, f, seedAdmin(), http.MethodPost, "/admin/ajax.php", form, (*Server).handleAdminAjax)
	if f.lastSetProblemSource.Source.String != "a b" {
		t.Errorf("uniqueSource 应去重为 \"a b\", 实际 %q", f.lastSetProblemSource.Source.String)
	}
	if f.lastSetProblemSource.ProblemID != 1001 {
		t.Errorf("problem_id = %d", f.lastSetProblemSource.ProblemID)
	}
}

func TestAdminAjaxGetUserListOfContest(t *testing.T) {
	f := &fakeQuerier{contestPrivilegeUsers: []string{"alice", "bob"}}
	form := url.Values{"m": {"get_user_list_of_contest"}, "contest_id": {"1000"}}
	rec := doNews(t, f, seedContestCreator(), http.MethodPost, "/admin/ajax.php", form, (*Server).handleAdminAjax)
	body := rec.Body.String()
	if !strings.Contains(body, "alice") || !strings.Contains(body, "bob") {
		t.Errorf("应回显比赛用户列表, 实际: %q", body)
	}
}
