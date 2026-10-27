package web

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sempr/hustoj-go/internal/web/config"
	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// ── admin/privilege_list.php ─────────────────────────────────────────

func TestAdminPrivilegeListRendersRows(t *testing.T) {
	f := &fakeQuerier{
		privCount: 2,
		privList: []dbgen.Privilege{
			{UserID: "alice", Rightstr: "vip", Valuestr: "true"},
			{UserID: "bob", Rightstr: "problem_start", Valuestr: "1000"},
		},
	}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/privilege_list.php", nil, (*Server).handleAdminPrivilegeList)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"alice", "vip", "bob", "problem_start:1000", "privilege-delete"} {
		if !strings.Contains(body, want) {
			t.Errorf("列表缺少 %q", want)
		}
	}
}

func TestAdminPrivilegeListRequiresAdmin(t *testing.T) {
	f := &fakeQuerier{}
	rec := doNews(t, f, "", http.MethodGet, "/admin/privilege_list.php", nil, (*Server).handleAdminPrivilegeList)
	if !strings.Contains(rec.Body.String(), "Please Login First!") {
		t.Errorf("匿名访问应被拒绝")
	}
}

func TestAdminPrivilegeListSuicideGuard(t *testing.T) {
	f := &fakeQuerier{
		privCount: 1,
		privList:  []dbgen.Privilege{{UserID: "admin", Rightstr: "administrator", Valuestr: "true"}},
	}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/privilege_list.php", nil, (*Server).handleAdminPrivilegeList)
	if !strings.Contains(rec.Body.String(), "Can't suicide") {
		t.Errorf("自己的 administrator 权限不应可删")
	}
}

// ── admin/privilege_add.php ──────────────────────────────────────────

func TestAdminPrivilegeAdd(t *testing.T) {
	f := &fakeQuerier{}
	form := url.Values{"user_id": {"alice"}, "rightstr": {"vip"}, "valuestr": {"true"}, "do": {"do"}}
	rec := doNews(t, f, seedAdmin(), http.MethodPost, "/admin/privilege_add.php", form, (*Server).handleAdminPrivilegeAdd)
	if !strings.Contains(rec.Body.String(), "Privilege Added!") {
		t.Errorf("应提示添加成功")
	}
	got := f.lastInsertPriv
	if got.UserID != "alice" || got.Rightstr != "vip" || got.Valuestr != "true" {
		t.Errorf("InsertPrivilege 参数 = %+v", got)
	}
}

func TestAdminPrivilegeAddContestPrefix(t *testing.T) {
	f := &fakeQuerier{}
	form := url.Values{"user_id": {"alice"}, "rightstr": {"1000"}, "do": {"do"}, "contest": {"do"}}
	doNews(t, f, seedAdmin(), http.MethodPost, "/admin/privilege_add.php", form, (*Server).handleAdminPrivilegeAdd)
	if f.lastInsertPriv.Rightstr != "c1000" {
		t.Errorf("比赛权限应加 c 前缀, 实际 %q", f.lastInsertPriv.Rightstr)
	}
}

func TestAdminPrivilegeAddPSVPrefix(t *testing.T) {
	f := &fakeQuerier{}
	form := url.Values{"user_id": {"alice"}, "rightstr": {"101"}, "do": {"do"}, "psv": {"do"}}
	doNews(t, f, seedAdmin(), http.MethodPost, "/admin/privilege_add.php", form, (*Server).handleAdminPrivilegeAdd)
	if f.lastInsertPriv.Rightstr != "s101" {
		t.Errorf("源码查看权限应加 s 前缀, 实际 %q", f.lastInsertPriv.Rightstr)
	}
}

// ── admin/privilege_delete.php ───────────────────────────────────────

func TestAdminPrivilegeDelete(t *testing.T) {
	f := &fakeQuerier{}
	form := url.Values{"uid": {"alice"}, "rightstr": {"vip"}}
	rec := doNews(t, f, seedAdmin(), http.MethodPost, "/admin/privilege_delete.php", form, (*Server).handleAdminPrivilegeDelete)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "alice vip deleted!") {
		t.Errorf("应回显删除结果")
	}
	if f.lastDeletePriv.UserID != "alice" || f.lastDeletePriv.Rightstr != "vip" {
		t.Errorf("DeletePrivilege 参数 = %+v", f.lastDeletePriv)
	}
}

func TestAdminPrivilegeDeleteSelfAdminBlocked(t *testing.T) {
	f := &fakeQuerier{}
	form := url.Values{"uid": {"admin"}, "rightstr": {"administrator"}}
	rec := doNews(t, f, seedAdmin(), http.MethodPost, "/admin/privilege_delete.php", form, (*Server).handleAdminPrivilegeDelete)
	if !strings.Contains(rec.Body.String(), "Can't remove administrator for yourself!") {
		t.Errorf("应拒绝删除自己的 administrator")
	}
	if containsCall(f.called, "DeletePrivilege") {
		t.Errorf("被拒绝时不应执行删除")
	}
}

// ── admin/changepass.php ─────────────────────────────────────────────

func TestAdminChangePass(t *testing.T) {
	f := &fakeQuerier{updatePwRows: 1}
	form := url.Values{"user_id": {"alice"}, "passwd": {"secret"}, "do": {"do"}}
	rec := doNews(t, f, seedAdmin(), http.MethodPost, "/admin/changepass.php", form, (*Server).handleAdminChangePass)
	if !strings.Contains(rec.Body.String(), "Password Changed!") {
		t.Errorf("应提示密码已改")
	}
	if f.lastUpdatePw.UserID != "alice" || !f.lastUpdatePw.Password.Valid || f.lastUpdatePw.Password.String == "" {
		t.Errorf("UpdateUserPasswordAdmin 参数 = %+v", f.lastUpdatePw)
	}
}

func TestAdminChangePassAdminRejected(t *testing.T) {
	f := &fakeQuerier{updatePwRows: 0}
	form := url.Values{"user_id": {"root"}, "passwd": {"secret"}, "do": {"do"}}
	rec := doNews(t, f, seedAdmin(), http.MethodPost, "/admin/changepass.php", form, (*Server).handleAdminChangePass)
	if !strings.Contains(rec.Body.String(), "is administrator!") {
		t.Errorf("管理员账号应改不动")
	}
}

// ── admin/user_df_change.php ─────────────────────────────────────────

func TestAdminUserDefunctToggle(t *testing.T) {
	for _, tc := range []struct{ from, to string }{{"N", "Y"}, {"Y", "N"}} {
		f := &fakeQuerier{userDefunct: tc.from}
		rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/user_df_change.php?cid=alice", nil, (*Server).handleAdminUserDefunctChange)
		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/user_list.php" {
			t.Fatalf("%s: 状态码/Location = %d/%q", tc.from, rec.Code, rec.Header().Get("Location"))
		}
		if f.lastSetDefunct.UserID != "alice" || f.lastSetDefunct.Defunct != tc.to {
			t.Errorf("%s → 期望 %s, 实际 %+v", tc.from, tc.to, f.lastSetDefunct)
		}
	}
}

func TestAdminUserDefunctSelfBlocked(t *testing.T) {
	f := &fakeQuerier{}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/user_df_change.php?cid=admin", nil, (*Server).handleAdminUserDefunctChange)
	if !strings.Contains(rec.Body.String(), "defunct yourself!") {
		t.Errorf("不能封禁自己")
	}
	if containsCall(f.called, "SetUserDefunct") {
		t.Errorf("被拒绝时不应写库")
	}
}

func TestAdminUserDefunctNotFound(t *testing.T) {
	f := &fakeQuerier{userDefunctErr: sql.ErrNoRows}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/user_df_change.php?cid=ghost", nil, (*Server).handleAdminUserDefunctChange)
	if !strings.Contains(rec.Body.String(), "No Such User!") {
		t.Errorf("不存在的用户应提示 No Such User!")
	}
}

// ── admin/user_set_ip.php ────────────────────────────────────────────

func TestAdminUserSetIP(t *testing.T) {
	f := &fakeQuerier{}
	form := url.Values{"user_id": {"alice"}, "ip": {"1.2.3.4"}, "do": {"do"}}
	rec := doNews(t, f, seedAdmin(), http.MethodPost, "/admin/user_set_ip.php", form, (*Server).handleAdminUserSetIP)
	if !strings.Contains(rec.Body.String(), "Login IP Changed!") {
		t.Errorf("应提示已设置登录 IP")
	}
	got := f.lastLoginLog
	if got.UserID != "alice" || got.Ip.String != "1.2.3.4" || got.Password.String != "set ip by admin" {
		t.Errorf("InsertLoginLog 参数 = %+v", got)
	}
}

// TestAdminUserRoutesAreMounted 通过真实 mux 验证用户/权限路由已挂上。
func TestAdminUserRoutesAreMounted(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sess_"+newsSID), []byte(seedAdmin()), 0600); err != nil {
		t.Fatal(err)
	}
	f := &fakeQuerier{privList: []dbgen.Privilege{}}
	srv := newTestServer(t, f, func(c *config.Config) {
		c.OJName = testOJName
		c.SessionBridge = true
		c.SessionDir = dir
	})
	srv.sessionBridge = NewSessionBridge(dir, testOJName)
	srv.sessions = newTestSessionStore(t)
	h := srv.handler()

	for _, p := range []string{
		"/admin/privilege_list.php",
		"/admin/privilege_add.php",
		"/admin/changepass.php",
		"/admin/user_set_ip.php",
		"/admin/user_list.php",
	} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		req.AddCookie(&http.Cookie{Name: phpSessionCookieName, Value: newsSID})
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("%s 状态码 = %d, 期望 200（路由是否已挂载？）", p, rec.Code)
		}
	}
}
