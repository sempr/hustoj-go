package web

import (
	"net/http"
	"strings"
	"testing"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

func TestAdminRanklistExportCSV(t *testing.T) {
	f := &fakeQuerier{
		exportRows: []dbgen.RanklistExportRow{
			{UserID: "u1", Nick: "Nick1", Solved: nullInt32(3), Submit: nullInt32(4)},
			{UserID: "u2", Nick: "=bad", Solved: nullInt32(0), Submit: nullInt32(0)},
		},
	}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/ranklist_export.php", nil, (*Server).handleAdminRanklistExport)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/csv") {
		t.Errorf("Content-Type = %q", ct)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "\xef\xbb\xbf") {
		t.Errorf("CSV 应带 UTF-8 BOM")
	}
	for _, want := range []string{"序号", "u1", "Nick1", "&#39;=bad", "75.00%"} {
		if !strings.Contains(body, want) {
			t.Errorf("CSV 缺少 %q\n%s", want, body)
		}
	}
	if !containsCall(f.called, "ExportRanklistUsers") {
		t.Errorf("无 scope 应走 ExportRanklistUsers")
	}
}

func TestAdminRanklistExportBadPrefix(t *testing.T) {
	f := &fakeQuerier{}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/ranklist_export.php?prefix=bad!", nil, (*Server).handleAdminRanklistExport)
	if !strings.Contains(rec.Body.String(), "invalid user name prefix") {
		t.Errorf("非法前缀应被拒")
	}
	if containsCall(f.called, "ExportRanklistUsers") {
		t.Errorf("非法前缀不应查询")
	}
}

func TestAdminRanklistExportScope(t *testing.T) {
	f := &fakeQuerier{}
	doNews(t, f, seedAdmin(), http.MethodGet, "/admin/ranklist_export.php?scope=d", nil, (*Server).handleAdminRanklistExport)
	if !containsCall(f.called, "ExportRanklistScoped") {
		t.Errorf("scope=d 应走 ExportRanklistScoped")
	}
	if f.lastExportSince == "" {
		t.Errorf("scope 起点日期不应为空")
	}
}

func TestAdminRanklistExportNoipGuard(t *testing.T) {
	f := &fakeQuerier{runningNOIP: 1}
	rec := doNews(t, f, seedAdmin(), http.MethodGet, "/admin/ranklist_export.php", nil, (*Server).handleAdminRanklistExport)
	if !strings.Contains(rec.Body.String(), "NOIP 比赛进行中") {
		t.Errorf("NOIP 进行中应拦截导出")
	}
	if containsCall(f.called, "ExportRanklistUsers") {
		t.Errorf("被拦截时不应查询")
	}
}
