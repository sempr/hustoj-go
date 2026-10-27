package web

import (
	"archive/zip"
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/sempr/hustoj-go/internal/web/config"
)

// ── 测试脚手架 ──────────────────────────────────────────────────────

// newTestDataServer 构造带管理员会话的测试服务，OJHome 指向临时目录
// （problemDataDir 由 OJHome 推导，因此数据文件全部落在沙箱里）。
func newTestDataServer(t *testing.T, session string, spj string) (*Server, string) {
	t.Helper()
	sessDir := t.TempDir()
	if session != "" {
		if err := os.WriteFile(filepath.Join(sessDir, "sess_"+newsSID), []byte(session), 0600); err != nil {
			t.Fatal(err)
		}
	}
	home := t.TempDir()
	srv := newTestServer(t, &fakeQuerier{spj: spj}, func(c *config.Config) {
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

func seedDataDir(t *testing.T, home string, pid int32, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(home, "data", strconv.Itoa(int(pid)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func dataDir(home string, pid int32) string {
	return filepath.Join(home, "data", strconv.Itoa(int(pid)))
}

// doTestData 直接调用 handler（绕过 nosurf，便于测业务逻辑）。
func doTestData(srv *Server, method, target string, form url.Values, withSession bool) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if withSession {
		req.AddCookie(&http.Cookie{Name: phpSessionCookieName, Value: newsSID})
	}
	rec := httptest.NewRecorder()
	srv.handleTestData(rec, req)
	return rec
}

// doTestDataUpload 组 multipart 上传请求（字段名 files，与页面一致）。
func doTestDataUpload(t *testing.T, srv *Server, pid int32, files map[string][]byte, withSession bool) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("action", "upload"); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		fw, err := mw.CreateFormFile("files", name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost,
		"/admin/test_data.php?pid="+strconv.Itoa(int(pid)), &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if withSession {
		req.AddCookie(&http.Cookie{Name: phpSessionCookieName, Value: newsSID})
	}
	rec := httptest.NewRecorder()
	srv.handleTestData(rec, req)
	return rec
}

func makeZip(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// ── 完整性检查（纯函数）──────────────────────────────────────────────

func TestAnalyzeTestData(t *testing.T) {
	cases := []struct {
		name      string
		names     []string
		spj       int32
		pairs     int
		inNoOut   []string
		outNoIn   []string
		numbering string
		odd       []string
		spjWarn   bool
	}{
		{"齐全", []string{"0.in", "0.out", "1.in", "1.out"}, 0, 2, nil, nil, "0–1 连续", nil, false},
		{"缺 out", []string{"0.in", "1.in", "1.out"}, 0, 1, []string{"0.in"}, nil, "0–1 连续", nil, false},
		{"孤立 out", []string{"0.in", "0.out", "2.out"}, 0, 1, nil, []string{"2.out"}, "0–0 连续", nil, false},
		{"跳号", []string{"0.in", "0.out", "2.in", "2.out"}, 0, 2, nil, nil, "0–2 缺 1", nil, false},
		{"非数字名", []string{"a.in", "a.out"}, 0, 1, nil, nil, "", []string{"a.in"}, false},
		{"SPJ 未提供程序", []string{"0.in", "0.out"}, 1, 1, nil, nil, "0–0 连续", nil, true},
		{"SPJ 提供了 spj", []string{"0.in", "0.out", "spj"}, 1, 1, nil, nil, "0–0 连续", nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rep := analyzeTestData(c.names, c.spj)
			if rep.Total != len(c.names) {
				t.Errorf("Total = %d, 期望 %d", rep.Total, len(c.names))
			}
			if rep.Pairs != c.pairs {
				t.Errorf("Pairs = %d, 期望 %d", rep.Pairs, c.pairs)
			}
			if strings.Join(rep.InWithoutOut, ",") != strings.Join(c.inNoOut, ",") {
				t.Errorf("InWithoutOut = %v, 期望 %v", rep.InWithoutOut, c.inNoOut)
			}
			if strings.Join(rep.OutWithoutIn, ",") != strings.Join(c.outNoIn, ",") {
				t.Errorf("OutWithoutIn = %v, 期望 %v", rep.OutWithoutIn, c.outNoIn)
			}
			if rep.Numbering != c.numbering {
				t.Errorf("Numbering = %q, 期望 %q", rep.Numbering, c.numbering)
			}
			if strings.Join(rep.OddNames, ",") != strings.Join(c.odd, ",") {
				t.Errorf("OddNames = %v, 期望 %v", rep.OddNames, c.odd)
			}
			if rep.SPJWarn != c.spjWarn {
				t.Errorf("SPJWarn = %v, 期望 %v", rep.SPJWarn, c.spjWarn)
			}
			// spj 程序存在时必须被列出
			if c.name == "SPJ 提供了 spj" && strings.Join(rep.SPJPrograms, ",") != "spj" {
				t.Errorf("SPJPrograms = %v, 期望 [spj]", rep.SPJPrograms)
			}
		})
	}
}

func TestAnalyzeTestDataCommunication(t *testing.T) {
	// 通讯题按 X.in／X.ans 配对，并检查 validator 是否存在。
	rep := analyzeTestData([]string{"0.in", "0.ans", "1.in", "1.ans", "validator"}, 4)
	if !rep.CommMode {
		t.Error("CommMode = false, 期望 true")
	}
	if rep.Pairs != 2 {
		t.Errorf("Pairs = %d, 期望 2", rep.Pairs)
	}
	if rep.CommValidator != "validator" {
		t.Errorf("CommValidator = %q, 期望 validator", rep.CommValidator)
	}
	if rep.CommWarn {
		t.Error("CommWarn = true, 期望 false")
	}
	if len(rep.InWithoutOut) != 0 || len(rep.OutWithoutIn) != 0 {
		t.Errorf("配对异常: 缺答案 %v, 孤立答案 %v", rep.InWithoutOut, rep.OutWithoutIn)
	}

	// 空答案文件是常态；缺 .ans 与缺校验器都要报出来。
	rep = analyzeTestData([]string{"0.in", "0.out", "1.in", "1.ans"}, 4)
	if strings.Join(rep.InWithoutOut, ",") != "0.in" {
		t.Errorf("InWithoutOut = %v, 期望 [0.in]", rep.InWithoutOut)
	}
	// .out 在通讯题里不参与判题，不该被当成孤立答案。
	if len(rep.OutWithoutIn) != 0 {
		t.Errorf("OutWithoutIn = %v, 期望空", rep.OutWithoutIn)
	}
	if !rep.CommWarn {
		t.Error("CommWarn = false, 期望 true（缺少 validator）")
	}

	// output_validator 是 validator 的合法替代名。
	rep = analyzeTestData([]string{"0.in", "0.ans", "output_validator"}, 4)
	if rep.CommValidator != "output_validator" {
		t.Errorf("CommValidator = %q, 期望 output_validator", rep.CommValidator)
	}
}

// ── 路径安全 ────────────────────────────────────────────────────────

func TestTestDataPathRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{"", "  ", "../x", "a/b", "..", ".", "/etc/passwd", "a\\b", "x\x00y"} {
		if _, err := testDataPath(dir, bad); err == nil {
			t.Errorf("testDataPath(%q) 应被拒绝", bad)
		}
	}
	for _, ok := range []string{"0.in", "1.out", "spj", "a b.txt", "中文.in"} {
		p, err := testDataPath(dir, ok)
		if err != nil {
			t.Errorf("testDataPath(%q) 应通过，却报 %v", ok, err)
			continue
		}
		if filepath.Dir(p) != dir {
			t.Errorf("testDataPath(%q) 落到了 %q", ok, p)
		}
	}

	for _, bad := range []string{"", "..", "../evil", "a/../../evil", "/abs"} {
		if _, ok := safeZipEntry(dir, bad); ok {
			t.Errorf("safeZipEntry(%q) 应被拒绝", bad)
		}
	}
	if p, ok := safeZipEntry(dir, "sub/1.in"); !ok || filepath.Dir(p) != filepath.Join(dir, "sub") {
		t.Errorf("合法子目录条目被拒: %q %v", p, ok)
	}
}

// ── 页面与权限 ──────────────────────────────────────────────────────

func TestTestDataPageRequiresPrivilege(t *testing.T) {
	srv, _ := newTestDataServer(t, "", "")
	rec := doTestData(srv, http.MethodGet, "/admin/test_data.php?pid=1001", nil, false)
	if !strings.Contains(rec.Body.String(), "Please Login First!") {
		t.Errorf("匿名应被拒，实际:\n%s", rec.Body.String())
	}
}

func TestTestDataPageListsFilesAndReport(t *testing.T) {
	srv, home := newTestDataServer(t, seedAdmin(), "")
	seedDataDir(t, home, 1001, map[string]string{"0.in": "1\n", "0.out": "1\n"})

	rec := doTestData(srv, http.MethodGet, "/admin/test_data.php?pid=1001", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{"0.in", "0.out", "组用例配对齐全", "下载", "打包下载全部"} {
		if !strings.Contains(body, want) {
			t.Errorf("页面缺少 %q", want)
		}
	}
}

// ── 上传（普通文件 / zip / zip slip）─────────────────────────────────

func TestTestDataUploadFile(t *testing.T) {
	srv, home := newTestDataServer(t, seedAdmin(), "")
	rec := doTestDataUpload(t, srv, 1001, map[string][]byte{"1.in": []byte("42\n")}, true)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("状态码 = %d, 期望 303", rec.Code)
	}
	data, err := os.ReadFile(filepath.Join(dataDir(home, 1001), "1.in"))
	if err != nil || string(data) != "42\n" {
		t.Errorf("文件未正确落盘: %v %q", err, data)
	}
}

func TestTestDataZipUploadStripsRoot(t *testing.T) {
	srv, home := newTestDataServer(t, seedAdmin(), "")
	blob := makeZip(t, map[string]string{
		"1001/1.in":  "a\n",
		"1001/1.out": "b\n",
	})
	rec := doTestDataUpload(t, srv, 1001, map[string][]byte{"data.zip": blob}, true)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("状态码 = %d, 期望 303", rec.Code)
	}
	dir := dataDir(home, 1001)
	for _, want := range []string{"1.in", "1.out"} {
		if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
			t.Errorf("顶层目录未被剥离，缺 %s: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "1001")); err == nil {
		t.Errorf("不应保留顶层目录 1001/")
	}
}

func TestTestDataZipSlipSkipped(t *testing.T) {
	srv, home := newTestDataServer(t, seedAdmin(), "")
	blob := makeZip(t, map[string]string{
		"ok.in":       "fine\n",
		"../evil.txt": "pwned",
	})
	rec := doTestDataUpload(t, srv, 1001, map[string][]byte{"data.zip": blob}, true)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("状态码 = %d, 期望 303", rec.Code)
	}
	dir := dataDir(home, 1001)
	if _, err := os.Stat(filepath.Join(dir, "ok.in")); err != nil {
		t.Errorf("合法条目应被写入: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "data", "evil.txt")); err == nil {
		t.Errorf("zip slip：数据目录外被写入 evil.txt")
	}
	if _, err := os.Stat(filepath.Join(home, "evil.txt")); err == nil {
		t.Errorf("zip slip：越界文件被写出")
	}
}

// ── 下载 / 查看 / 打包 ──────────────────────────────────────────────

func TestTestDataDownloadAndView(t *testing.T) {
	srv, home := newTestDataServer(t, seedAdmin(), "")
	seedDataDir(t, home, 1001, map[string]string{"1.in": "hello\n"})

	// 下载：附件头 + 内容一致
	rec := doTestData(srv, http.MethodGet, "/admin/test_data.php?pid=1001&action=download&name=1.in", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("下载状态码 = %d", rec.Code)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if rec.Body.String() != "hello\n" {
		t.Errorf("下载内容 = %q", rec.Body.String())
	}

	// 查看：页面里能看到内容
	rec = doTestData(srv, http.MethodGet, "/admin/test_data.php?pid=1001&action=view&name=1.in", nil, true)
	if !strings.Contains(rec.Body.String(), "hello") {
		t.Errorf("查看页缺内容:\n%s", rec.Body.String())
	}

	// 越界名一律拒绝
	rec = doTestData(srv, http.MethodGet, "/admin/test_data.php?pid=1001&action=download&name=..%2Fgo.mod", nil, true)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("越界下载状态码 = %d, 期望 400", rec.Code)
	}
}

// ── 在线编辑 ────────────────────────────────────────────────────────

func TestTestDataEditAndSave(t *testing.T) {
	srv, home := newTestDataServer(t, seedAdmin(), "")
	seedDataDir(t, home, 1001, map[string]string{"1.in": "1 2\n"})
	dir := dataDir(home, 1001)

	// 打开编辑页：应有 textarea 且回填原内容。
	rec := doTestData(srv, http.MethodGet, "/admin/test_data.php?pid=1001&action=edit&name=1.in", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("编辑页状态码 = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "<textarea") || !strings.Contains(body, "1 2") {
		t.Errorf("编辑页缺 textarea 或原内容:\n%s", body)
	}
	if !strings.Contains(body, `value="save"`) {
		t.Errorf("编辑页缺保存 action")
	}

	// 保存：内容落盘。
	if rec := doTestData(srv, http.MethodPost, "/admin/test_data.php?pid=1001",
		url.Values{"action": {"save"}, "name": {"1.in"}, "content": {"3 4\n"}}, true); rec.Code != http.StatusSeeOther {
		t.Fatalf("保存状态码 = %d", rec.Code)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "1.in")); err != nil || string(data) != "3 4\n" {
		t.Errorf("保存未生效: %v %q", err, data)
	}

	// CRLF 归一为 LF。
	doTestData(srv, http.MethodPost, "/admin/test_data.php?pid=1001",
		url.Values{"action": {"save"}, "name": {"1.in"}, "content": {"a\r\nb\r\n"}}, true)
	if data, _ := os.ReadFile(filepath.Join(dir, "1.in")); string(data) != "a\nb\n" {
		t.Errorf("CRLF 未归一: %q", data)
	}

	// 允许保存为空内容。
	doTestData(srv, http.MethodPost, "/admin/test_data.php?pid=1001",
		url.Values{"action": {"save"}, "name": {"1.in"}, "content": {""}}, true)
	if info, err := os.Stat(filepath.Join(dir, "1.in")); err != nil || info.Size() != 0 {
		t.Errorf("空内容保存失败: %v", err)
	}
}

func TestTestDataEditRejects(t *testing.T) {
	srv, home := newTestDataServer(t, seedAdmin(), "")
	big := strings.Repeat("a", (1<<20)+1)
	seedDataDir(t, home, 1001, map[string]string{
		"spj":    "binary",
		"1.in":   string([]byte{0xff, 0xfe, 0x00}),
		"big.in": big,
	})
	dir := dataDir(home, 1001)

	// 非文本扩展名
	if body := doTestData(srv, http.MethodGet, "/admin/test_data.php?pid=1001&action=edit&name=spj", nil, true).Body.String(); !strings.Contains(body, "无法在线编辑") {
		t.Errorf("非文本文件应拒绝编辑:\n%s", body)
	}
	// 非法 UTF-8
	if body := doTestData(srv, http.MethodGet, "/admin/test_data.php?pid=1001&action=edit&name=1.in", nil, true).Body.String(); !strings.Contains(body, "合法的 UTF-8") {
		t.Errorf("非法 UTF-8 应拒绝编辑:\n%s", body)
	}
	// 超限
	if body := doTestData(srv, http.MethodGet, "/admin/test_data.php?pid=1001&action=edit&name=big.in", nil, true).Body.String(); !strings.Contains(body, "无法在线编辑") {
		t.Errorf("超限文件应拒绝编辑:\n%s", body)
	}

	// 保存到非文本文件：拒绝，内容不变。
	doTestData(srv, http.MethodPost, "/admin/test_data.php?pid=1001",
		url.Values{"action": {"save"}, "name": {"spj"}, "content": {"x"}}, true)
	if data, _ := os.ReadFile(filepath.Join(dir, "spj")); string(data) != "binary" {
		t.Errorf("非文本文件被改写: %q", data)
	}

	// 越界名保存：拒绝，目录外不受影响。
	outside := filepath.Join(home, "data", "keep.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	doTestData(srv, http.MethodPost, "/admin/test_data.php?pid=1001",
		url.Values{"action": {"save"}, "name": {"../keep.txt"}, "content": {"pwned"}}, true)
	if data, _ := os.ReadFile(outside); string(data) != "keep" {
		t.Errorf("越界保存改写了目录外文件: %q", data)
	}
}

func TestTestDataZipDownload(t *testing.T) {
	srv, home := newTestDataServer(t, seedAdmin(), "")
	seedDataDir(t, home, 1001, map[string]string{"0.in": "a\n", "0.out": "b\n"})

	rec := doTestData(srv, http.MethodGet, "/admin/test_data.php?pid=1001&action=zip", nil, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("打包状态码 = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q", ct)
	}
	zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
	if err != nil {
		t.Fatalf("返回的不是可解析的 zip: %v", err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = string(b)
	}
	if got["0.in"] != "a\n" || got["0.out"] != "b\n" {
		t.Errorf("打包内容不符: %v", got)
	}
}

// ── 新建 / 重命名 / 删除 ────────────────────────────────────────────

func TestTestDataCreateRenameDelete(t *testing.T) {
	srv, home := newTestDataServer(t, seedAdmin(), "")
	seedDataDir(t, home, 1001, map[string]string{"0.in": "x\n"})
	dir := dataDir(home, 1001)

	if rec := doTestData(srv, http.MethodPost, "/admin/test_data.php?pid=1001",
		url.Values{"action": {"create"}, "name": {"9.in"}}, true); rec.Code != http.StatusSeeOther {
		t.Fatalf("新建状态码 = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "9.in")); err != nil {
		t.Fatalf("新建未生效: %v", err)
	}

	if rec := doTestData(srv, http.MethodPost, "/admin/test_data.php?pid=1001",
		url.Values{"action": {"rename"}, "old": {"9.in"}, "new": {"9.out"}}, true); rec.Code != http.StatusSeeOther {
		t.Fatalf("重命名状态码 = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "9.in")); err == nil {
		t.Errorf("重命名后旧名仍在")
	}
	if _, err := os.Stat(filepath.Join(dir, "9.out")); err != nil {
		t.Errorf("重命名后新名不在: %v", err)
	}

	// 多选删除：9.out 与 0.in
	rec := doTestData(srv, http.MethodPost, "/admin/test_data.php?pid=1001",
		url.Values{"action": {"delete"}, "names": {"9.out", "0.in"}}, true)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("删除状态码 = %d", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(dir, "9.out")); err == nil {
		t.Errorf("删除未生效")
	}
	// 越界名不应影响目录外文件
	outside := filepath.Join(home, "data", "keep.txt")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	doTestData(srv, http.MethodPost, "/admin/test_data.php?pid=1001",
		url.Values{"action": {"delete"}, "names": {"../keep.txt"}}, true)
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("越界删除影响了目录外文件: %v", err)
	}
}

// ── CSRF ───────────────────────────────────────────────────────────

// POST 不带 CSRF token 必须被 nosurf 拦下（页面表单都带了 csrfInput）。
//
// 注意：本项目刻意不用 nosurf 默认的裸 403，而是渲染"页面已过期"的 alert
// 页（状态码仍是 200），因此这里断言提示文案 + 断言删除没有真正发生。
func TestTestDataPostNeedsCSRF(t *testing.T) {
	srv, home := newTestDataServer(t, seedAdmin(), "")
	dir := seedDataDir(t, home, 1001, map[string]string{"0.in": "x\n"})

	form := url.Values{"action": {"delete"}, "names": {"0.in"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/test_data.php?pid=1001", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{Name: phpSessionCookieName, Value: newsSID})
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)

	if !strings.Contains(rec.Body.String(), "页面已过期") {
		t.Errorf("无 CSRF token 的 POST 应被拦下，实际:\n%s", rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "0.in")); err != nil {
		t.Errorf("CSRF 拦截后不应发生删除: %v", err)
	}
}
