package web

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sempr/hustoj-go/internal/web/config"
	"github.com/sempr/hustoj-go/pkg/dbgen"
)

const newsSID = "newstest1234567890abcdef"

// doNews 构造一次 admin/news 请求。session 非空时写 PHP 会话文件并带 PHPSESSID；
// form 非空时按表单 POST 编码。（这些 handler 直接调用，不经过 mux，因此不涉 CSRF。）
func doNews(t *testing.T, f *fakeQuerier, session, method, target string, form url.Values,
	h func(*Server, http.ResponseWriter, *http.Request)) *httptest.ResponseRecorder {
	t.Helper()
	dir := t.TempDir()
	if session != "" {
		if err := os.WriteFile(filepath.Join(dir, "sess_"+newsSID), []byte(session), 0600); err != nil {
			t.Fatal(err)
		}
	}
	srv := newTestServer(t, f, func(c *config.Config) {
		c.OJName = testOJName
		c.SessionBridge = true
		c.SessionDir = dir
	})
	srv.sessionBridge = NewSessionBridge(dir, testOJName)
	if session != "" {
		srv.sessions = newTestSessionStore(t)
	}

	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if session != "" {
		req.AddCookie(&http.Cookie{Name: phpSessionCookieName, Value: newsSID})
	}
	rec := httptest.NewRecorder()
	h(srv, rec, req)
	return rec
}

var newsTime = time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)

// ── admin/news_list.php ──────────────────────────────────────────────

func TestAdminNewsListRendersRows(t *testing.T) {
	f := &fakeQuerier{
		newsCount: 2,
		newsAdminList: []dbgen.ListNewsAdminRow{
			{NewsID: 5, Title: "Hello", Time: newsTime, Defunct: "N", Importance: 3, Menu: 1},
			{NewsID: 4, Title: "", Time: newsTime, Defunct: "Y", Importance: 0, Menu: 0},
		},
	}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/news_list.php", nil, (*Server).handleAdminNewsList)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"Hello", "Empty", "2024-01-02 03:04:05",
		`news_edit.php?id=5`, `news_add_page.php?cid=5`,
		">On<", ">Off<", ">YES<", ">NO<",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("列表缺少 %q", want)
		}
	}
}

func TestAdminNewsListRequiresAdmin(t *testing.T) {
	f := &fakeQuerier{}
	rec := doNews(t, f, "", http.MethodGet, "/admin/news_list.php", nil, (*Server).handleAdminNewsList)
	if !strings.Contains(rec.Body.String(), "Please Login First!") {
		t.Errorf("匿名访问应被拒绝，实际: %s", rec.Body.String())
	}
}

func TestAdminNewsListSearchHidesPagination(t *testing.T) {
	f := &fakeQuerier{
		newsCount:       100,
		newsAdminSearch: []dbgen.SearchNewsAdminRow{{NewsID: 9, Title: "Found", Time: newsTime, Defunct: "N"}},
	}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/news_list.php?keyword=Foo", nil, (*Server).handleAdminNewsList)
	body := rec.Body.String()
	if !strings.Contains(body, "Found") {
		t.Errorf("搜索结果应包含命中行")
	}
	if strings.Contains(body, "go to page") {
		t.Errorf("带关键词时不应渲染分页")
	}
	if !containsCall(f.called, "SearchNewsAdmin") {
		t.Errorf("应走搜索查询")
	}
}

func TestAdminNewsListPaginates(t *testing.T) {
	f := &fakeQuerier{newsCount: 100, newsAdminList: []dbgen.ListNewsAdminRow{}}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/news_list.php?page=2", nil, (*Server).handleAdminNewsList)
	body := rec.Body.String()
	if !strings.Contains(body, "go to page") {
		t.Fatalf("无关键词时应渲染分页")
	}
	if !strings.Contains(body, `news_list.php?page=3`) {
		t.Errorf("第 2 页应有到第 3 页的链接")
	}
}

// ── admin/news_add_page.php ──────────────────────────────────────────

func TestAdminNewsAddPageCopy(t *testing.T) {
	f := &fakeQuerier{newsAdminGet: dbgen.GetNewsAdminRow{NewsID: 5, Title: "Old", Content: "Body"}}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/news_add_page.php?cid=5", nil, (*Server).handleAdminNewsAddPage)
	body := rec.Body.String()
	if !strings.Contains(body, "Old-Copy") {
		t.Errorf("复制标题应回填 <原标题>-Copy")
	}
	if !strings.Contains(body, "Body") {
		t.Errorf("正文应回填")
	}
}

// ── admin/news_add.php ───────────────────────────────────────────────

func TestAdminNewsAdd(t *testing.T) {
	f := &fakeQuerier{}
	// Markdown 原样入库，不再套 <p>/<br /> 之类 HTML 预处理。
	content := "# A\n\n`code, with, commas`"
	form := url.Values{"title": {"T"}, "content": {content}, "showInMenu": {"on"}}
	rec := doNews(t, f, seedAdmin(), http.MethodPost, "/admin/news_add.php", form, (*Server).handleAdminNewsAdd)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/news_list.php" {
		t.Fatalf("状态码/Location = %d/%q", rec.Code, rec.Header().Get("Location"))
	}
	got := f.lastNewsInsert
	if got.Title != "T" || got.Content != content || got.Menu != 1 || got.UserID != "admin" {
		t.Errorf("InsertNews 参数 = %+v", got)
	}
}

// ── admin/news_edit.php ──────────────────────────────────────────────

func TestAdminNewsEditGet(t *testing.T) {
	f := &fakeQuerier{newsAdminGet: dbgen.GetNewsAdminRow{NewsID: 7, Title: "E", Content: "C", Menu: 1}}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/news_edit.php?id=7", nil, (*Server).handleAdminNewsEdit)
	body := rec.Body.String()
	if !strings.Contains(body, `name="news_id" value="7"`) {
		t.Errorf("编辑表单应带 news_id")
	}
	if !strings.Contains(body, "checked") {
		t.Errorf("menu=1 时复选框应勾选")
	}
}

func TestAdminNewsEditNotFound(t *testing.T) {
	f := &fakeQuerier{newsAdminGetErr: sql.ErrNoRows}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/news_edit.php?id=999", nil, (*Server).handleAdminNewsEdit)
	if !strings.Contains(rec.Body.String(), "No such News!") {
		t.Errorf("不存在的公告应提示 No such News!")
	}
}

func TestAdminNewsEditPost(t *testing.T) {
	f := &fakeQuerier{}
	content := "x, y"
	form := url.Values{"news_id": {"7"}, "title": {"T2"}, "content": {content}}
	rec := doNews(t, f, seedAdmin(), http.MethodPost, "/admin/news_edit.php", form, (*Server).handleAdminNewsEdit)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/admin/news_list.php" {
		t.Fatalf("状态码/Location = %d/%q", rec.Code, rec.Header().Get("Location"))
	}
	got := f.lastNewsUpdate
	if got.NewsID != 7 || got.Title != "T2" || got.Content != content || got.Menu != 0 {
		t.Errorf("UpdateNews 参数 = %+v", got)
	}
}

// ── admin/news_df_change.php ─────────────────────────────────────────

func TestAdminNewsDefunctToggle(t *testing.T) {
	for _, tc := range []struct{ from, to string }{{"N", "Y"}, {"Y", "N"}} {
		f := &fakeQuerier{newsAdminGet: dbgen.GetNewsAdminRow{NewsID: 5, Defunct: tc.from}}
		rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/news_df_change.php?id=5", nil, (*Server).handleAdminNewsDefunctChange)
		if rec.Code != http.StatusFound {
			t.Fatalf("状态码 = %d, 期望 302", rec.Code)
		}
		if f.lastNewsDefunct.NewsID != 5 || f.lastNewsDefunct.Defunct != tc.to {
			t.Errorf("%s → 期望 %s, 实际 %+v", tc.from, tc.to, f.lastNewsDefunct)
		}
	}
}

// TestAdminNewsRoutesAreMounted 通过真实 mux 验证 news 路由已挂上，
// 避免再次出现"handler 写完但没接线"（页面落到 404/占位）。
func TestAdminNewsRoutesAreMounted(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sess_"+newsSID), []byte(seedAdmin()), 0600); err != nil {
		t.Fatal(err)
	}
	f := &fakeQuerier{newsAdminList: []dbgen.ListNewsAdminRow{}}
	srv := newTestServer(t, f, func(c *config.Config) {
		c.OJName = testOJName
		c.SessionBridge = true
		c.SessionDir = dir
	})
	srv.sessionBridge = NewSessionBridge(dir, testOJName)
	srv.sessions = newTestSessionStore(t)
	h := srv.handler()

	for _, p := range []string{
		"/admin/news_list.php",
		"/admin/news_add_page.php",
		"/admin/news_edit.php?id=1",
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
