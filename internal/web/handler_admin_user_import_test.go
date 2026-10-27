package web

import (
	"bytes"
	"database/sql"
	"mime/multipart"
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

// ── admin/user_add.php ───────────────────────────────────────────────

func TestAdminUserAdd(t *testing.T) {
	f := &fakeQuerier{userByIDErr: sql.ErrNoRows}
	form := url.Values{"do": {"do"}, "ulist": {"u1 p1\nu2 p2"}}
	rec := doNews(t, f, seedAdmin(), http.MethodPost, "/admin/user_add.php", form, (*Server).handleAdminUserAdd)
	body := rec.Body.String()
	for _, want := range []string{"u1 is added!", "u2 is added!"} {
		if !strings.Contains(body, want) {
			t.Errorf("缺少 %q", want)
		}
	}
	if f.lastBulkUser.UserID != "u2" || f.lastBulkUser.Nick != "u2" || !f.lastBulkUser.Password.Valid {
		t.Errorf("InsertBulkUser 参数 = %+v", f.lastBulkUser)
	}
	if !containsCall(f.called, "InsertLoginLog") {
		t.Errorf("建号应写 loginlog")
	}
}

func TestAdminUserAddDuplicate(t *testing.T) {
	f := &fakeQuerier{userByID: dbgen.GetUserByIDRow{UserID: "u1"}}
	form := url.Values{"do": {"do"}, "ulist": {"u1 p1"}}
	rec := doNews(t, f, seedAdmin(), http.MethodPost, "/admin/user_add.php", form, (*Server).handleAdminUserAdd)
	if !strings.Contains(rec.Body.String(), "User already exist!") {
		t.Errorf("已存在用户应报错")
	}
	if containsCall(f.called, "InsertBulkUser") {
		t.Errorf("重复用户不应建号")
	}
}

func TestAdminUserAddLineFormatError(t *testing.T) {
	f := &fakeQuerier{userByIDErr: sql.ErrNoRows}
	form := url.Values{"do": {"do"}, "ulist": {"a b c"}}
	rec := doNews(t, f, seedAdmin(), http.MethodPost, "/admin/user_add.php", form, (*Server).handleAdminUserAdd)
	if !strings.Contains(rec.Body.String(), "Line format error!") {
		t.Errorf("三个 token 应报行格式错误")
	}
}

// ── admin/user_import.php ────────────────────────────────────────────

func doImport(t *testing.T, f *fakeQuerier, csvContent string) *httptest.ResponseRecorder {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sess_"+newsSID), []byte(seedAdmin()), 0600); err != nil {
		t.Fatal(err)
	}
	srv := newTestServer(t, f, func(c *config.Config) {
		c.OJName = testOJName
		c.SessionBridge = true
		c.SessionDir = dir
	})
	srv.sessionBridge = NewSessionBridge(dir, testOJName)
	srv.sessions = newTestSessionStore(t)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("fps", "users.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(csvContent)); err != nil {
		t.Fatal(err)
	}
	mw.Close()

	req := httptest.NewRequest(http.MethodPost, "/admin/user_import.php", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.AddCookie(&http.Cookie{Name: phpSessionCookieName, Value: newsSID})
	rec := httptest.NewRecorder()
	srv.handleAdminUserImport(rec, req)
	return rec
}

func TestAdminUserImportCSV(t *testing.T) {
	f := &fakeQuerier{}
	csv := "学号,昵称,密码,学校,电子邮件,分组,到期日\nu1,Nick1,pw1,S1,e@x,g1,2030-01-01\n"
	rec := doImport(t, f, csv)
	body := rec.Body.String()
	if !strings.Contains(body, "u1") {
		t.Errorf("导入结果应包含 u1, 实际: %s", body)
	}
	got := f.lastImportUser
	if got.UserID != "u1" || got.Nick != "Nick1" || got.School != "S1" || got.GroupName != "g1" {
		t.Errorf("UpsertImportUser 参数 = %+v", got)
	}
	if got.ExpiryDate.Format("2006-01-02") != "2030-01-01" {
		t.Errorf("到期日 = %s", got.ExpiryDate.Format("2006-01-02"))
	}
	if !got.Password.Valid || got.Password.String == "" {
		t.Errorf("密码应被哈希")
	}
}

func TestAdminUserImportBadTemplate(t *testing.T) {
	f := &fakeQuerier{}
	csv := "foo,bar,baz\n1,2,3\n"
	rec := doImport(t, f, csv)
	if !strings.Contains(rec.Body.String(), "请用下载的模板") {
		t.Errorf("无学号表头应报模板错误")
	}
	if containsCall(f.called, "UpsertImportUser") {
		t.Errorf("模板错误时不应导入")
	}
}
