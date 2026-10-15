package web

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sempr/hustoj-go/internal/web/config"
)

// newUploadServer 构造上传测试用服务：会话桥 + 可选管理员会话，OJHome 指向
// 临时目录（uploadDir 由 OJHome 推导，从而不会写到系统路径）。
func newUploadServer(t *testing.T, session string) (*Server, string) {
	t.Helper()
	sessDir := t.TempDir()
	if session != "" {
		if err := os.WriteFile(filepath.Join(sessDir, "sess_"+newsSID), []byte(session), 0600); err != nil {
			t.Fatal(err)
		}
	}
	home := t.TempDir()
	srv := newTestServer(t, &fakeQuerier{}, func(c *config.Config) {
		c.OJName = testOJName
		c.SessionBridge = true
		c.SessionDir = sessDir
		c.OJHome = home
	})
	srv.sessionBridge = NewSessionBridge(sessDir, testOJName)
	if session != "" {
		srv.sessions = newTestSessionStore(t)
	}
	return srv, home
}

// multipartRequest 构造带 file 字段的上传请求（Markdown 编辑器上传按钮）。
func multipartRequest(t *testing.T, target, filename string) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("\x89PNG fake image body")); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, target, &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

const uploadPath = "/assets/editor/upload.php"

// withSessionRequest 带上测试会话 cookie（doProblem 里用同一枚 sid）。
func withSessionRequest(req *http.Request) *http.Request {
	req.AddCookie(&http.Cookie{Name: phpSessionCookieName, Value: newsSID})
	return req
}

// 未登录必须被拒（对应 PHP 的角色判定）。
func TestUploadRejectsAnonymous(t *testing.T) {
	srv, _ := newUploadServer(t, "")
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, multipartRequest(t, uploadPath, "pic.png")) // 刻意不带会话

	if rec.Code != http.StatusForbidden {
		t.Errorf("匿名上传状态码 = %d, 期望 403", rec.Code)
	}
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.Error != 1 {
		t.Errorf("应返回 JSON 错误体，实际 %s", rec.Body.String())
	}
}

// 上传接口只接受 POST。
func TestUploadRejectsNonPost(t *testing.T) {
	srv, _ := newUploadServer(t, seedAdmin())
	req := withSessionRequest(httptest.NewRequest(http.MethodGet, uploadPath, nil))
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET 状态码 = %d, 期望 405", rec.Code)
	}
}

// 扩展名白名单：.php 不允许，杜绝上传即执行。
func TestUploadRejectsPhpExtension(t *testing.T) {
	srv, home := newUploadServer(t, seedAdmin())
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, withSessionRequest(multipartRequest(t, uploadPath, "evil.php")))

	if rec.Code != http.StatusBadRequest {
		t.Errorf("上传 .php 状态码 = %d, 期望 400", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(home, "src", "web", "upload")); err == nil {
		t.Errorf("被拒绝的上传不应创建任何目录")
	}
}

// 合法图片落盘到 <uploadDir>/image/<日期>/，并返回 /upload/... 地址；
// 同时验证该请求不带 CSRF token 也能通过（上传按钮带不了 token，走豁免）。
func TestUploadSuccessLandsFile(t *testing.T) {
	srv, home := newUploadServer(t, seedAdmin())
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, withSessionRequest(multipartRequest(t, uploadPath, "示例 image.PNG")))

	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200: %s", rec.Code, rec.Body.String())
	}
	var resp uploadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("返回体不是 JSON: %s", rec.Body.String())
	}
	if resp.Error != 0 || resp.URL == "" {
		t.Fatalf("上传应成功，实际 %+v", resp)
	}

	wantDir := filepath.Join(home, "src", "web", "upload", "image", time.Now().Format("20060102"))
	if _, err := os.Stat(filepath.Join(wantDir, "示例 image.png")); err != nil {
		t.Errorf("文件未落盘: %v", err)
	}
	if !strings.HasPrefix(resp.URL, "/upload/image/") || !strings.HasSuffix(resp.URL, "示例 image.png") {
		t.Errorf("返回地址不符: %q", resp.URL)
	}
}

// 资源与上传接口都在 /assets/ 之下，避免依赖 PHP web 根的路径约定。
func TestUploadAndAssetsPathsAreAssetScoped(t *testing.T) {
	for _, p := range []string{"/assets/jquery.min.js", "/assets/katex.min.js"} {
		srv, _ := newUploadServer(t, seedAdmin())
		rec := httptest.NewRecorder()
		srv.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s 状态码 = %d, 期望 200", p, rec.Code)
		}
	}
}

const previewPath = "/admin/markdown_preview.php"

func previewRequest(t *testing.T, text string) *http.Request {
	t.Helper()
	form := url.Values{"text": {text}}
	req := httptest.NewRequest(http.MethodPost, previewPath, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

// 实时预览接口未登录必须被拒。
func TestMarkdownPreviewRejectsAnonymous(t *testing.T) {
	srv, _ := newUploadServer(t, "")
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, previewRequest(t, "# hi"))
	if rec.Code != http.StatusForbidden {
		t.Errorf("匿名预览状态码 = %d, 期望 403", rec.Code)
	}
}

// 实时预览接口只接受 POST。
func TestMarkdownPreviewRejectsNonPost(t *testing.T) {
	srv, _ := newUploadServer(t, seedAdmin())
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, withSessionRequest(httptest.NewRequest(http.MethodGet, previewPath, nil)))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET 状态码 = %d, 期望 405", rec.Code)
	}
}

// 预览走与题面相同的 goldmark 管线：GFM、原始 HTML，以及 \(…\)、\[…\] 定界符保留。
func TestMarkdownPreviewRenders(t *testing.T) {
	srv, _ := newUploadServer(t, seedAdmin())
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, withSessionRequest(previewRequest(t, "# 标题\n\n\\(a+b\\) 与 \\[c=d\\]")))
	if rec.Code != http.StatusOK {
		t.Fatalf("预览状态码 = %d, 期望 200", rec.Code)
	}
	out := rec.Body.String()
	for _, want := range []string{"<h1", "\\(a+b\\)", "\\[c=d\\]"} {
		if !strings.Contains(out, want) {
			t.Errorf("预览缺少 %q: %s", want, out)
		}
	}
}
