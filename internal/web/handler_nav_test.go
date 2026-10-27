package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sempr/hustoj-go/internal/web/config"
)

// TestSiteHeaderNavAnonymous 匿名页应带站点导航（页面链接 + 登录/注册）。
func TestSiteHeaderNavAnonymous(t *testing.T) {
	f := &fakeQuerier{}
	srv := newTestServer(t, f, func(c *config.Config) { c.OJName = testOJName; c.Register = true })
	rec := httptest.NewRecorder()
	srv.renderPage(rec, httptest.NewRequest(http.MethodGet, "/problemset.php", nil), http.StatusOK,
		"errorPage", "t", map[string]string{"Message": "x"})
	body := rec.Body.String()
	for _, want := range []string{"/problemset.php", "/contest.php", "/status.php", "/ranklist.php", "/faqs.php", "/loginpage.php", "/registerpage.php"} {
		if !strings.Contains(body, want) {
			t.Errorf("导航缺少 %q", want)
		}
	}
	if strings.Contains(body, "/logout.php") {
		t.Errorf("匿名不应出现登出链接")
	}
}

// TestSiteHeaderNavLoggedIn 登录页应显示用户菜单与后台入口。
func TestSiteHeaderNavLoggedIn(t *testing.T) {
	rec := doProblem(t, &fakeQuerier{}, seedAdmin(), http.MethodGet, "/admin/index.php", nil, t.TempDir(), (*Server).handleAdminIndex)
	body := rec.Body.String()
	for _, want := range []string{"/logout.php", "/userinfo.php?user=admin", "/admin/index.php", "/modifypage.php"} {
		if !strings.Contains(body, want) {
			t.Errorf("登录后导航缺少 %q", want)
		}
	}
	if strings.Contains(body, "/loginpage.php\"") {
		t.Errorf("登录后不应出现登录链接")
	}
}

// 后台页应带后台导航条，且不再有 frameset（嵌进 <main> 会被浏览器忽略）。
func TestAdminPagesHaveNavNoFrameset(t *testing.T) {
	rec := doProblem(t, &fakeQuerier{}, seedAdmin(), http.MethodGet, "/admin/index.php", nil, t.TempDir(), (*Server).handleAdminIndex)
	body := rec.Body.String()

	if !strings.Contains(body, `class="admin-nav"`) {
		t.Errorf("后台页缺少导航条:\n%s", body)
	}
	if strings.Contains(body, "frameset") || strings.Contains(body, "<frame ") {
		t.Errorf("后台页不应再有 frameset:\n%s", body)
	}
	for _, want := range []string{"/admin/problem_list.php", "/admin/user_list.php", "/admin/rejudge.php"} {
		if !strings.Contains(body, want) {
			t.Errorf("后台导航缺少 %q", want)
		}
	}
}

// 匿名访问后台不应出现后台导航条（权限不足）。
func TestAdminNavHiddenForAnonymous(t *testing.T) {
	rec := doProblem(t, &fakeQuerier{}, "", http.MethodGet, "/admin/index.php", nil, t.TempDir(), (*Server).handleAdminIndex)
	if strings.Contains(rec.Body.String(), `class="admin-nav"`) {
		t.Errorf("匿名不应出现后台导航")
	}
}
