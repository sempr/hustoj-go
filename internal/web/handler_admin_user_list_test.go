package web

import (
	"database/sql"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

func TestAdminUserListRendersRows(t *testing.T) {
	f := &fakeQuerier{
		usersCount: 2,
		usersActive: []dbgen.ListUsersActiveRow{
			{
				UserID: "alice", Nick: "Alice", Email: sql.NullString{String: "a@b", Valid: true},
				Ip: "1.2.3.4", School: "S", GroupName: "g", ParentPhone: "13800000000", Defunct: "N",
				CoinEarned: 5, CoinBonus: 1, CoinSpent: 2, ExpiryDate: time.Now().AddDate(0, 0, 30),
			},
			{
				UserID: "bob", Email: sql.NullString{}, Ip: "5.6.7.8", Defunct: "Y",
				ExpiryDate: time.Now().AddDate(0, 0, -1),
			},
		},
	}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/user_list.php", nil, (*Server).handleAdminUserList)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"alice", "Alice", "1.2.3.4", "13800000000",
		">Normal<", ">Deleted<", `class="green"`, `class="red"`,
		"user_df_change.php?cid=alice", "changepass.php?uid=alice", "privilege_add.php?uid=alice",
		"<td>4</td>", // alice 金币余额 = 5+1-2
		`fd="nick"`,  // 行内编辑 span
	} {
		if !strings.Contains(body, want) {
			t.Errorf("列表缺少 %q", want)
		}
	}
}

func TestAdminUserListRequiresAdmin(t *testing.T) {
	f := &fakeQuerier{}
	rec := doNews(t, f, "", http.MethodGet, "/admin/user_list.php", nil, (*Server).handleAdminUserList)
	if !strings.Contains(rec.Body.String(), "Please Login First!") {
		t.Errorf("匿名访问应被拒绝")
	}
}

func TestAdminUserListTrash(t *testing.T) {
	f := &fakeQuerier{
		usersCount: 1,
		usersTrash: []dbgen.ListUsersTrashRow{{UserID: "gone", Defunct: "Y", ExpiryDate: time.Now()}},
	}
	doNews(t, f, seedAdmin(), http.MethodGet, "/admin/user_list.php?trash", nil, (*Server).handleAdminUserList)
	if !containsCall(f.called, "ListUsersTrash") {
		t.Errorf("trash 视图应走 ListUsersTrash")
	}
	if containsCall(f.called, "ListUsersActive") {
		t.Errorf("trash 视图不应走 ListUsersActive")
	}
}

func TestAdminUserListSearchNoPagination(t *testing.T) {
	f := &fakeQuerier{
		usersCount:  100,
		usersSearch: []dbgen.SearchUsersRow{{UserID: "alice", Defunct: "N", ExpiryDate: time.Now()}},
	}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/user_list.php?keyword=ali", nil, (*Server).handleAdminUserList)
	body := rec.Body.String()
	if !strings.Contains(body, "alice") {
		t.Errorf("搜索结果应包含命中行")
	}
	if strings.Contains(body, "go to page") {
		t.Errorf("带关键词时不应渲染分页")
	}
	if !containsCall(f.called, "SearchUsers") {
		t.Errorf("应走搜索查询")
	}
}
