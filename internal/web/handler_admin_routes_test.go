package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAdminDirectoryRedirect 验证 /admin/ 会跳到 /admin/index.php。
//
// 背景：PHP 的页头用相对链接 href="admin/"（phpweb/template/syzoj/header.php:217），
// 靠 Apache 的 DirectoryIndex 落到 admin/index.php。Go 侧没有目录索引，
// 直接访问 /admin/ 原本是 404 —— 页头点「后台管理」就断在这里。
func TestAdminDirectoryRedirect(t *testing.T) {
	srv := newTestServer(t, &fakeQuerier{}, nil)
	h := srv.handler()

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/", nil))

	if rec.Code != http.StatusFound {
		t.Fatalf("状态码 = %d, 期望 302", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/admin/index.php" {
		t.Errorf("Location = %q, 期望 /admin/index.php", got)
	}
}
