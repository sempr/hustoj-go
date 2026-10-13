package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// /assets/ 下的样式表必须可访问、Content-Type 正确、且带缓存头。
func TestAssetRoutesServeCSS(t *testing.T) {
	s := newTestServer(t, &fakeQuerier{}, nil)

	for _, p := range []string{"/assets/simple.min.css", "/assets/hustoj.css"} {
		rec := httptest.NewRecorder()
		s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))

		if rec.Code != http.StatusOK {
			t.Errorf("%s 状态码 = %d，期望 200", p, rec.Code)
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
			t.Errorf("%s Content-Type = %q，期望 text/css", p, ct)
		}
		if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "max-age") {
			t.Errorf("%s 缺少缓存头：%q", p, cc)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s 返回空内容", p)
		}
	}

	// 未打包的资源应 404，而不是落到别的处理器上。
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/nope.css", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("未知资源状态码 = %d，期望 404", rec.Code)
	}
}

// 页面必须挂上两个样式表，并保留站点导航。
func TestLayoutLinksAssets(t *testing.T) {
	s := newTestServer(t, &fakeQuerier{}, nil)
	rec := httptest.NewRecorder()
	s.renderPage(rec, httptest.NewRequest(http.MethodGet, "/", nil), http.StatusOK,
		"errorPage", "错误", map[string]string{"Message": "x"})

	body := rec.Body.String()
	for _, want := range []string{`href="/assets/simple.min.css"`, `href="/assets/hustoj.css"`, "<main>"} {
		if !strings.Contains(body, want) {
			t.Errorf("页面缺少 %q:\n%s", want, body)
		}
	}
}
