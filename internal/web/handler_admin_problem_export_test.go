package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

func TestAdminProblemExportPage(t *testing.T) {
	f := &fakeQuerier{}
	rec := doProblem(t, f, seedAdmin(), http.MethodGet, "/admin/problem_export.php", nil, t.TempDir(), (*Server).handleAdminProblemExport)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `action="/admin/problem_export_xml.php"`) {
		t.Errorf("导出表单缺 action")
	}
}

func TestAdminProblemExportXML(t *testing.T) {
	ojhome := t.TempDir()
	dir := filepath.Join(ojhome, "data", "1001")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, "1.in"), []byte("hello"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "1.out"), []byte("world"), 0o644)

	f := &fakeQuerier{rangeProblems: []dbgen.ListProblemsByRangeRow{
		{ProblemID: 1001, Title: "T1", TimeLimit: 1, MemoryLimit: 128, Spj: "0"},
	}}
	form := url.Values{"start": {"1001"}, "end": {"1001"}}
	rec := doProblem(t, f, seedAdmin(), http.MethodPost, "/admin/problem_export_xml.php", form, ojhome, (*Server).handleAdminProblemExportXML)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/xml") {
		t.Errorf("Content-Type = %q", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, ".xml") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"<fps version=", "<item>", "<title><![CDATA[T1]]></title>",
		`<time_limit unit="s"><![CDATA[1]]></time_limit>`,
		`<test_input name="1"><![CDATA[hello]]></test_input>`,
		`<test_output name="1"><![CDATA[world]]></test_output>`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("XML 缺少 %q\n%s", want, body)
		}
	}
}

func TestAdminProblemExportXMLZip(t *testing.T) {
	f := &fakeQuerier{rangeProblems: []dbgen.ListProblemsByRangeRow{{ProblemID: 1, Title: "T", Spj: "0"}}}
	form := url.Values{"start": {"1"}, "end": {"1"}, "zip": {""}}
	rec := doProblem(t, f, seedAdmin(), http.MethodPost, "/admin/problem_export_xml.php", form, t.TempDir(), (*Server).handleAdminProblemExportXML)
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "application/zip") {
		t.Errorf("Content-Type = %q, 期望 zip", ct)
	}
	// zip 头魔数 PK。
	if !strings.HasPrefix(rec.Body.String(), "PK") {
		t.Errorf("响应不是 zip")
	}
}

func TestAdminProblemCopy(t *testing.T) {
	f := &fakeQuerier{}
	rec := doProblem(t, f, seedAdmin(), http.MethodGet, "/admin/problem_copy.php", nil, t.TempDir(), (*Server).handleAdminProblemCopy)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, 期望 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/admin/problem_add_page_uoj.php") {
		t.Errorf("复制页应含各导入入口")
	}
}
