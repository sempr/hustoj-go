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

// doProblem 与 doNews 相同，但可指定 OJHome（测试用临时目录，避免写系统路径）。
func doProblem(t *testing.T, f *fakeQuerier, session, method, target string, form url.Values, ojhome string,
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
		c.OJHome = ojhome
	})
	srv.sessionBridge = NewSessionBridge(dir, testOJName)
	if session != "" {
		srv.sessions = newTestSessionStore(t)
	}
	var body *strings.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	} else {
		body = strings.NewReader("")
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

func TestAdminProblemAddPage(t *testing.T) {
	f := &fakeQuerier{
		lastProblemSource: sql.NullString{String: "tagX", Valid: true},
		futureContests:    []dbgen.ListFutureContestsRow{{ContestID: 9, Title: sql.NullString{String: "Future", Valid: true}}},
	}
	rec := doProblem(t, f, seedAdmin(), http.MethodGet, "/admin/problem_add_page.php", nil, t.TempDir(), (*Server).handleAdminProblemAddPage)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"tagX", "Future", `action="/admin/problem_add.php"`, `name="spj"`} {
		if !strings.Contains(body, want) {
			t.Errorf("表单缺少 %q", want)
		}
	}
}

func TestAdminProblemAdd(t *testing.T) {
	ojhome := t.TempDir()
	f := &fakeQuerier{nextProblemID: 1050}
	form := url.Values{
		"title": {"Hello,World"}, "time_limit": {"1"}, "memory_limit": {"128"},
		"description": {"D, E"}, "input": {"I"}, "output": {"O"},
		"sample_input": {"A\u00a0B"}, "sample_output": {"out"}, "test_input": {"t"}, "test_output": {"to"},
		"hint": {"H"}, "source": {"src"}, "spj": {"1"}, "coin": {"3"}, "contest_id": {""},
	}
	rec := doProblem(t, f, seedAdmin(), http.MethodPost, "/admin/problem_add.php", form, ojhome, (*Server).handleAdminProblemAdd)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	got := f.lastInsertProblem
	if got.Title != "Hello&#44;World" || got.Spj != "1" || got.SampleInput.String != "A B" {
		t.Errorf("InsertProblem 参数 = %+v", got)
	}
	// Markdown 字段原样入库，不做 `,`→`&#44;`。
	if got.Description != "D, E" || got.Input != "I" || got.Output != "O" || got.Hint != "H" {
		t.Errorf("Markdown 字段应原样存储 = %+v", got)
	}
	if !containsCall(f.called, "SetProblemCoin") {
		t.Errorf("应写 coin")
	}
	// 数据文件应写到 <OJHome>/data/1050/。
	if _, err := os.Stat(filepath.Join(ojhome, "data", "1050", "sample.in")); err != nil {
		t.Errorf("sample.in 未写入: %v", err)
	}
	if !containsCall(f.called, "InsertPrivilege") {
		t.Errorf("应给创建者加 p<pid> 权限")
	}
	if !strings.Contains(rec.Body.String(), "1050") {
		t.Errorf("结果页应包含新题号")
	}
}

func TestAdminProblemAddRequiresRole(t *testing.T) {
	f := &fakeQuerier{}
	rec := doProblem(t, f, "", http.MethodGet, "/admin/problem_add_page.php", nil, t.TempDir(), (*Server).handleAdminProblemAddPage)
	if !strings.Contains(rec.Body.String(), "Please Login First!") {
		t.Errorf("匿名访问应被拒绝")
	}
}

func TestAdminProblemEditGet(t *testing.T) {
	f := &fakeQuerier{detail: dbgen.Problem{ProblemID: 1001, Title: "E", Description: "D", Spj: "1", Coin: 2}}
	rec := doProblem(t, f, seedAdmin(), http.MethodGet, "/admin/problem_edit.php?id=1001", nil, t.TempDir(), (*Server).handleAdminProblemEdit)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `name="problem_id" value="1001"`) || !strings.Contains(body, "E") {
		t.Errorf("编辑表单缺字段")
	}
}

// 新版用轻量 Markdown 编辑器替代 KindEditor，断言 md-source 与编辑器脚本已引入。
func TestAdminProblemPagesLoadMarkdownEditor(t *testing.T) {
	f := &fakeQuerier{
		detail:            dbgen.Problem{ProblemID: 1001, Title: "E", Description: "D", Spj: "1", Coin: 2},
		lastProblemSource: sql.NullString{String: "tagX", Valid: true},
		futureContests:    []dbgen.ListFutureContestsRow{{ContestID: 9, Title: sql.NullString{String: "Future", Valid: true}}},
	}
	for _, tc := range []struct {
		name string
		path string
		h    func(*Server, http.ResponseWriter, *http.Request)
	}{
		{"add", "/admin/problem_add_page.php", (*Server).handleAdminProblemAddPage},
		{"edit", "/admin/problem_edit.php?id=1001", (*Server).handleAdminProblemEdit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := doProblem(t, f, seedAdmin(), http.MethodGet, tc.path, nil, t.TempDir(), tc.h)
			if rec.Code != http.StatusOK {
				t.Fatalf("状态码 = %d, 期望 200", rec.Code)
			}
			body := rec.Body.String()
			for _, want := range []string{
				`class="md-source"`,
				"/assets/editor/upload.php",   // 上传按钮（Go 实现）
				"/admin/markdown_preview.php", // 实时预览（Go 实现）
				"/assets/katex.min.js",        // 预览里渲染公式
			} {
				if !strings.Contains(body, want) {
					t.Errorf("缺少 %q", want)
				}
			}
			// 不得再引用 KindEditor 静态资源。
			if strings.Contains(body, "kindeditor") {
				t.Errorf("不应再引用 KindEditor：%s", body)
			}
		})
	}
}

func TestAdminProblemEditPost(t *testing.T) {
	ojhome := t.TempDir()
	f := &fakeQuerier{}
	form := url.Values{
		"problem_id": {"1001"}, "title": {"T2"}, "time_limit": {"2"}, "memory_limit": {"256"},
		"description": {"D"}, "input": {"I"}, "output": {"O"}, "sample_input": {"S"}, "sample_output": {"O"},
		"hint": {"H"}, "source": {"src"}, "spj": {"0"}, "remote_oj": {""}, "remote_id": {""}, "coin": {"5"},
	}
	rec := doProblem(t, f, seedAdmin(), http.MethodPost, "/admin/problem_edit.php", form, ojhome, (*Server).handleAdminProblemEdit)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	got := f.lastUpdateProblemFull
	if got.ProblemID != 1001 || got.Title != "T2" || got.Coin != 5 {
		t.Errorf("UpdateProblemFull 参数 = %+v", got)
	}
}
