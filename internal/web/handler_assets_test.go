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
	for _, want := range []string{
		`href="/assets/simple.min.css?v=`,
		`href="/assets/hustoj.css?v=`,
		`href="/assets/katex.min.css?v=`, // KaTeX：公式排版（含字体，随仓库走 /assets）
		"<main>",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("页面缺少 %q:\n%s", want, body)
		}
	}
	// 不应再依赖 PHP web 根的 /template/ 目录
	if strings.Contains(body, "/template/syzoj/css/katex.min.css") {
		t.Errorf("KaTeX 样式不应再引用 /template/ 目录:\n%s", body)
	}
	// 缓存击穿哈希不能为空，否则 ?v= 不起作用。
	if assetVersion == "" {
		t.Errorf("assetVersion 为空，缓存击穿失效")
	}
}

// KaTeX 资源随仓库内嵌，且上标字号覆盖必须存在（否则 10^12 的上标偏大）。
func TestKatexAssetsAndScriptSize(t *testing.T) {
	css, err := assetFS.ReadFile("static/katex.min.css")
	if err != nil {
		t.Fatalf("内嵌的 katex.min.css 缺失: %v", err)
	}
	if len(css) < 20000 {
		t.Errorf("katex.min.css 过小: %d 字节", len(css))
	}
	if _, err := assetFS.ReadDir("static/fonts"); err != nil {
		t.Fatalf("内嵌字体目录缺失: %v", err)
	}

	h, err := assetFS.ReadFile("static/hustoj.css")
	if err != nil {
		t.Fatalf("读取 hustoj.css 失败: %v", err)
	}
	s := string(h)
	for _, want := range []string{
		".katex .katex-sizing.reset-size6.size3 { font-size: 0.55em; }",
		".katex .katex-sizing.reset-size6.size2 { font-size: 0.47em; }",
		".katex .katex-sizing.reset-size6.size1 { font-size: 0.39em; }",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("缺少上标字号覆盖 %q", want)
		}
	}
}

// 自定义颜色的链接（结果色、徽章、分页）必须带 :visited 变体。
//
// 浏览器对已访问链接单独跑一遍 :visited 级联，非 :visited 的类规则压不过
// Simple.css 的 a:visited{color:var(--accent)}，会把点过的"通过"染成 accent
// （暗色模式下是黄色）。所以必须显式补 .xxx:visited。
func TestHustojCSSOverridesVisitedLinkColors(t *testing.T) {
	css, err := assetFS.ReadFile("static/hustoj.css")
	if err != nil {
		t.Fatalf("读取 hustoj.css 失败: %v", err)
	}
	s := string(css)

	for _, want := range []string{".ac:visited", ".wa:visited", ".ce:visited", ".label:visited", ".item:visited"} {
		if !strings.Contains(s, want) {
			t.Errorf("缺少 %q：已访问状态会退回 accent 色", want)
		}
	}
	// 普通链接交给 Simple.css（它已有 a, a:visited 同色），
	// 我们自己不再声明裸 a/a:visited 颜色，避免优先级歧义。
	for _, bad := range []string{"\na:visited {", "\na, a:visited {", ":where(a, a:visited)"} {
		if strings.Contains(s, bad) {
			t.Errorf("不应存在 %q", strings.TrimSpace(bad))
		}
	}
}
