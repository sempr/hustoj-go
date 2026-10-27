package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// buildCategories 对应 category.php 的 explode(" ") + get_domain + unique + sort。
func TestBuildCategoriesExtractsDedupesSorts(t *testing.T) {
	got := buildCategories([]string{
		"POJ https://codeforces.com/problemset/problem/1",
		"POJ 简单",
		"",
		"简单",
	})

	want := []string{"POJ", "codeforces.com", "简单"} // http 压成域名；去重；按字节序
	if len(got) != len(want) {
		t.Fatalf("标签数 = %d，期望 %d：%+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Label != w {
			t.Errorf("labels[%d] = %q，期望 %q", i, got[i].Label, w)
		}
		if !strings.HasPrefix(got[i].Href, "problemset.php?search=") {
			t.Errorf("labels[%d] 链接应为 problemset.php?search=：%q", i, got[i].Href)
		}
		if got[i].Theme == "" {
			t.Errorf("labels[%d] 缺少配色", i)
		}
	}

	// 非 ASCII 标签的搜索词必须转义，且与 problemset 的 urlEncode 一致。
	if got[2].Href != "problemset.php?search="+urlEncode("简单") {
		t.Errorf("中文标签链接转义不正确：%q", got[2].Href)
	}
}

// 空题库也要返回非 nil，模板才能走 {{else}} 显示 "No Category Now!"。
func TestBuildCategoriesEmpty(t *testing.T) {
	for _, in := range [][]string{nil, {""}} {
		got := buildCategories(in)
		if got == nil {
			t.Fatalf("buildCategories(%v) 返回 nil，模板无法区分空集", in)
		}
		if len(got) != 0 {
			t.Errorf("buildCategories(%v) 期望空，得到 %+v", in, got)
		}
	}
}

// /category.php 必须已注册路由（404 说明 nginx 切到 Go 后该页会挂掉）。
func TestCategoryRouteRegistered(t *testing.T) {
	s := newTestServer(t, &fakeQuerier{}, nil)
	rec := httptest.NewRecorder()
	s.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/category.php", nil))
	if rec.Code == http.StatusNotFound {
		t.Errorf("/category.php 返回 404，路由未注册")
	}
}

// 模板必须把标签渲染成指向 problemset.php?search= 的链接。
func TestCategoryTemplateRendersLabels(t *testing.T) {
	s := newTestServer(t, &fakeQuerier{}, nil)
	rec := httptest.NewRecorder()
	data := CategoryPageVM{
		Title: "来源/分类",
		Categories: []CategoryItem{
			{Label: "POJ", Href: "problemset.php?search=POJ", Theme: "success"},
		},
	}
	s.renderPage(rec, httptest.NewRequest(http.MethodGet, "/category.php", nil), http.StatusOK, "categoryPage", "来源/分类", data)

	body := rec.Body.String()
	if !strings.Contains(body, `href="problemset.php?search=POJ"`) {
		t.Errorf("未渲染来源链接：%s", body)
	}
	if !strings.Contains(body, ">POJ<") {
		t.Errorf("未渲染来源标签文本：%s", body)
	}
}
