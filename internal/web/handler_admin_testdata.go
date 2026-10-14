// 后台测试数据管理（对应 PHP admin/phpfm.php 里与题目数据相关的那部分）。
//
// 页面 /admin/test_data.php?pid=N 管理 <OJHome>/data/<pid>/：
// 列表 / 上传（含 zip 自动解压）/ 下载 / 打包下载 / 查看 / 新建 / 重命名 / 删除。
//
// 判题侧的约定（internal/client/run.go、executor.go、interactive.go）：
//   - 扫描 *.in，按同名 X.in → X.out 严格配对；没有 .ans 回退；
//   - SPJ 靠目录内存在 spj / tpj / upj 文件判定：spj/upj 的参数为
//     输入文件 标准输出 选手输出，tpj 为 输入文件 选手输出 标准输出
//     （testlib 的 inf/ouf/ans 顺序）；退出码 0=AC 非 0=WA，upj 的
//     退出码即 0–100 的分值（executor.go handleSpecialJudge）；
//   - 交互评测（SPJ=16）用目录里预编译的 spj 作交互器，附加用例文件
//     作参数，经两个 FIFO 与玩家程序双向通信，退出码 0=AC 非 0=WA
//     （interactive.go）。
//
// 这两条是页面上"完整性检查"横幅的判定依据。
//
// 安全边界：所有操作的文件名都先经 testDataPath / safeZipEntry 限定在本题
// 数据目录内（拦 ../、绝对路径、反斜杠与 \x00），zip 解压额外限条目数、
// 解压总量，并以前缀校验挡 Zip Slip。
package web

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	// testDataViewLimit 是"查看"能显示的字节上限，超出只显示前段。
	testDataViewLimit = 256 << 10
	// testDataUploadLimit 是单次上传（含 zip 包体）的体积上限。
	testDataUploadLimit = 64 << 20
	// testDataZipTotal / testDataZipEntries 防 zip 炸弹。
	testDataZipTotal   = 512 << 20
	testDataZipEntries = 2000
)

// ── 视图模型 ────────────────────────────────────────────────────────

type testDataFile struct {
	Name     string
	SizeText string
	ModTime  string
	IsText   bool
	// ViewURL / DownURL 预先拼好并转义，避免在模板里直接拼文件名（含 &、+ 等会出错）。
	ViewURL template.URL
	DownURL template.URL
}

// testDataReport 是完整性检查结果。
type testDataReport struct {
	Total        int      // 普通文件总数
	Pairs        int      // X.in 与 X.out 齐全的组数
	InWithoutOut []string // 缺 .out 的 .in（判题会异常）
	OutWithoutIn []string // 孤立的 .out（判题用不到）
	Numbering    string   // 形如 "0–4 缺 3" 或 "0–3 连续"
	OddNames     []string // 非数字编号的 .in
	SPJPrograms  []string // 目录内存在的 spj / tpj / upj
	SPJWarn      bool     // 题目标记为 SPJ 但目录内没有判题程序
}

type adminTestDataVM struct {
	Title  string
	OJName string
	PID    int32
	Path   string
	Files  []testDataFile
	Report testDataReport
	Msg    string
	Err    string
	Token  *http.Request
}

type adminTestDataViewVM struct {
	Title     string
	OJName    string
	PID       int32
	Name      string
	Content   string
	Truncated bool
}

// ── 路由 ────────────────────────────────────────────────────────────

func (s *Server) registerTestDataRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/test_data.php", s.handleTestData)
	mux.HandleFunc("POST /admin/test_data.php", s.handleTestData)
}

func (s *Server) handleTestData(w http.ResponseWriter, r *http.Request) {
	identity := s.resolveIdentity(w, r)
	if !s.hasAdminPrivilege(identity) {
		s.adminLoginRequired(w)
		return
	}
	pid := int32(parseInt(r.URL.Query().Get("pid")))
	if pid <= 0 {
		http.Error(w, "缺少或非法的 pid", http.StatusBadRequest)
		return
	}
	dir := s.problemDataDir(pid)

	if r.Method == http.MethodGet {
		switch r.URL.Query().Get("action") {
		case "view":
			s.testDataView(w, r, pid, dir)
		case "download":
			s.testDataDownload(w, r, pid, dir)
		case "zip":
			s.testDataZip(w, r, pid, dir)
		default:
			s.testDataList(w, r, pid, dir, r.URL.Query().Get("msg"), r.URL.Query().Get("err"))
		}
		return
	}

	// POST：multipart（上传）与 urlencoded（其余动作）都走 ParseForm 家族。
	if err := r.ParseMultipartForm(testDataUploadLimit); err != nil {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
	}
	msg, errMsg := s.applyTestDataAction(r, dir, r.FormValue("action"))

	q := url.Values{"pid": {strconv.Itoa(int(pid))}}
	if errMsg != "" {
		q.Set("err", errMsg)
	} else {
		q.Set("msg", msg)
	}
	// 303：刷新不会重复提交。
	http.Redirect(w, r, "/admin/test_data.php?"+q.Encode(), http.StatusSeeOther)
}

// ── 动作 ────────────────────────────────────────────────────────────

func (s *Server) applyTestDataAction(r *http.Request, dir, action string) (msg, errMsg string) {
	switch action {
	case "upload":
		return s.testDataUpload(r, dir)
	case "delete":
		names := r.Form["names"]
		if len(names) == 0 {
			return "", "未选择要删除的文件"
		}
		n := 0
		for _, name := range names {
			p, err := testDataPath(dir, name)
			if err != nil {
				continue
			}
			if err := os.Remove(p); err == nil {
				n++
			}
		}
		return fmt.Sprintf("已删除 %d 个文件", n), ""
	case "create":
		name := strings.TrimSpace(r.FormValue("name"))
		p, err := testDataPath(dir, name)
		if err != nil {
			return "", "文件名不合法：" + err.Error()
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", "创建数据目录失败"
		}
		f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			if errors.Is(err, os.ErrExist) {
				return "", "文件已存在：" + name
			}
			return "", "创建文件失败"
		}
		f.Close()
		return "已新建 " + name, ""
	case "rename":
		oldName := strings.TrimSpace(r.FormValue("old"))
		newName := strings.TrimSpace(r.FormValue("new"))
		oldPath, err := testDataPath(dir, oldName)
		if err != nil {
			return "", "原文件名不合法：" + err.Error()
		}
		newPath, err := testDataPath(dir, newName)
		if err != nil {
			return "", "新文件名不合法：" + err.Error()
		}
		if _, err := os.Stat(oldPath); err != nil {
			return "", "原文件不存在：" + oldName
		}
		if _, err := os.Stat(newPath); err == nil {
			return "", "目标已存在：" + newName
		}
		if err := os.Rename(oldPath, newPath); err != nil {
			return "", "重命名失败"
		}
		return fmt.Sprintf("已把 %s 重命名为 %s", oldName, newName), ""
	}
	return "", "未知操作"
}

// testDataUpload 处理多文件上传；.zip 走解压，其余按普通文件落盘。
func (s *Server) testDataUpload(r *http.Request, dir string) (msg, errMsg string) {
	if r.MultipartForm == nil {
		return "", "没有收到上传内容"
	}
	headers := r.MultipartForm.File["files"]
	if len(headers) == 0 {
		return "", "没有选择文件"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "创建数据目录失败"
	}

	saved, unzipped, skipped, failed := 0, 0, 0, []string{}
	for _, h := range headers {
		if h.Size > testDataUploadLimit {
			failed = append(failed, h.Filename+"（超过 64MB）")
			continue
		}
		f, err := h.Open()
		if err != nil {
			failed = append(failed, h.Filename)
			continue
		}
		if strings.EqualFold(filepath.Ext(h.Filename), ".zip") {
			data, err := io.ReadAll(io.LimitReader(f, testDataUploadLimit))
			f.Close()
			if err != nil {
				failed = append(failed, h.Filename)
				continue
			}
			n, sk, err := extractZipInto(dir, data)
			if err != nil {
				failed = append(failed, h.Filename+"（"+err.Error()+"）")
				continue
			}
			unzipped += n
			skipped += len(sk)
			continue
		}
		p, err := testDataPath(dir, filepath.Base(h.Filename))
		if err != nil {
			f.Close()
			failed = append(failed, h.Filename)
			continue
		}
		out, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			f.Close()
			failed = append(failed, h.Filename)
			continue
		}
		_, err = io.Copy(out, io.LimitReader(f, testDataUploadLimit))
		out.Close()
		f.Close()
		if err != nil {
			failed = append(failed, h.Filename)
			continue
		}
		saved++
	}

	parts := []string{}
	if saved > 0 {
		parts = append(parts, fmt.Sprintf("上传 %d 个文件", saved))
	}
	if unzipped > 0 {
		parts = append(parts, fmt.Sprintf("解压出 %d 个文件", unzipped))
	}
	if skipped > 0 {
		parts = append(parts, fmt.Sprintf("跳过 %d 个非法条目", skipped))
	}
	if len(parts) == 0 {
		parts = append(parts, "没有任何文件被写入")
	}
	if len(failed) > 0 {
		return strings.Join(parts, "，"), "以下文件失败：" + strings.Join(failed, "，")
	}
	return strings.Join(parts, "，"), ""
}

// ── 列表 / 查看 / 下载 / 打包 ────────────────────────────────────────

func (s *Server) testDataList(w http.ResponseWriter, r *http.Request, pid int32, dir, msg, errMsg string) {
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		s.log.Error("读取数据目录失败", "dir", dir, "err", err)
		http.Error(w, "读取数据目录失败", http.StatusInternalServerError)
		return
	}

	files := make([]testDataFile, 0, len(entries))
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		size := int64(0)
		mod := ""
		if err == nil {
			size = info.Size()
			mod = info.ModTime().Format("2006-01-02 15:04:05")
		}
		files = append(files, testDataFile{
			Name:     e.Name(),
			SizeText: humanSize(size),
			ModTime:  mod,
			IsText:   isTextLikeName(e.Name()),
			ViewURL:  testDataHref(pid, "view", e.Name()),
			DownURL:  testDataHref(pid, "download", e.Name()),
		})
		names = append(names, e.Name())
	}
	sortTestDataFiles(files)

	s.renderPage(w, r, http.StatusOK, "adminTestDataPage", fmt.Sprintf("测试数据 - 题目 %d", pid), adminTestDataVM{
		Title:  fmt.Sprintf("测试数据 - 题目 %d", pid),
		OJName: s.cfg.OJName,
		PID:    pid,
		Path:   dir,
		Files:  files,
		Report: analyzeTestData(names, s.problemSpj(r, pid)),
		Msg:    msg,
		Err:    errMsg,
		Token:  r,
	})
}

func (s *Server) testDataView(w http.ResponseWriter, r *http.Request, pid int32, dir string) {
	name := r.URL.Query().Get("name")
	p, err := testDataPath(dir, name)
	if err != nil {
		http.Error(w, "文件名不合法", http.StatusBadRequest)
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		http.Error(w, "读取文件失败", http.StatusNotFound)
		return
	}
	truncated := false
	if len(data) > testDataViewLimit {
		data = data[:testDataViewLimit]
		truncated = true
	}
	s.renderPage(w, r, http.StatusOK, "adminTestDataViewPage", name, adminTestDataViewVM{
		Title:     name,
		OJName:    s.cfg.OJName,
		PID:       pid,
		Name:      name,
		Content:   string(data),
		Truncated: truncated,
	})
}

func (s *Server) testDataDownload(w http.ResponseWriter, r *http.Request, pid int32, dir string) {
	name := r.URL.Query().Get("name")
	p, err := testDataPath(dir, name)
	if err != nil {
		http.Error(w, "文件名不合法", http.StatusBadRequest)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		http.Error(w, "文件不存在", http.StatusNotFound)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", contentDisposition(name))
	_, _ = io.Copy(w, f)
}

// testDataZip 把整个数据目录打包成 zip 流式下发（不落临时文件）。
func (s *Server) testDataZip(w http.ResponseWriter, r *http.Request, pid int32, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		http.Error(w, "读取数据目录失败", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", contentDisposition(fmt.Sprintf("%d.zip", pid)))

	zw := zip.NewWriter(w)
	defer zw.Close()
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		src, err := os.Open(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		dst, err := zw.Create(e.Name())
		if err == nil {
			_, _ = io.Copy(dst, src)
		}
		src.Close()
	}
}

// ── 完整性检查 ──────────────────────────────────────────────────────

// analyzeTestData 依据判题的实际读取方式做检查（纯函数，便于单测）：
// X.in 与 X.out 必须同名成对；编号是否连续；SPJ 题目是否有判题程序。
func analyzeTestData(names []string, spj int32) testDataReport {
	rep := testDataReport{Total: len(names)}
	has := func(n string) bool {
		for _, x := range names {
			if x == n {
				return true
			}
		}
		return false
	}

	var nums []int64
	for _, n := range names {
		switch {
		case strings.HasSuffix(n, ".in"):
			base := strings.TrimSuffix(n, ".in")
			if has(base + ".out") {
				rep.Pairs++
			} else {
				rep.InWithoutOut = append(rep.InWithoutOut, n)
			}
			if v, err := strconv.ParseInt(base, 10, 64); err == nil {
				nums = append(nums, v)
			} else {
				rep.OddNames = append(rep.OddNames, n)
			}
		case strings.HasSuffix(n, ".out"):
			base := strings.TrimSuffix(n, ".out")
			if !has(base + ".in") {
				rep.OutWithoutIn = append(rep.OutWithoutIn, n)
			}
		}
	}
	sort.Strings(rep.InWithoutOut)
	sort.Strings(rep.OutWithoutIn)
	sort.Strings(rep.OddNames)

	rep.Numbering = numberingSummary(nums)

	for _, prog := range []string{"spj", "tpj", "upj"} {
		if has(prog) {
			rep.SPJPrograms = append(rep.SPJPrograms, prog)
		}
	}
	if spj == 1 && len(rep.SPJPrograms) == 0 {
		rep.SPJWarn = true
	}
	return rep
}

// numberingSummary 汇总 .in 的数字编号：全为数字时给"区间 + 缺号"，
// 有空缺才提示，否则只说连续。
func numberingSummary(nums []int64) string {
	if len(nums) == 0 {
		return ""
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
	lo, hi := nums[0], nums[len(nums)-1]
	present := make(map[int64]bool, len(nums))
	for _, n := range nums {
		present[n] = true
	}
	var missing []string
	for v := lo; v <= hi; v++ {
		if !present[v] {
			missing = append(missing, strconv.FormatInt(v, 10))
		}
	}
	if len(missing) == 0 {
		return fmt.Sprintf("%d–%d 连续", lo, hi)
	}
	return fmt.Sprintf("%d–%d 缺 %s", lo, hi, strings.Join(missing, ","))
}

// ── 路径与 zip 安全 ─────────────────────────────────────────────────

// testDataPath 把单个文件名限定在 dir 内：只接受纯文件名，拒绝路径分隔符、
// 上跳、NUL 与空串，并在拼接后再做一次前缀校验。
func testDataPath(dir, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("文件名为空")
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return "", errors.New("文件名不能包含路径分隔符")
	}
	if name == "." || name == ".." || filepath.Base(name) != name {
		return "", errors.New("文件名不合法")
	}
	full := filepath.Join(dir, name)
	cleanDir := filepath.Clean(dir)
	if full != filepath.Join(cleanDir, name) || !strings.HasPrefix(full, cleanDir+string(os.PathSeparator)) {
		return "", errors.New("文件名越界")
	}
	return full, nil
}

// safeZipEntry 校验 zip 条目相对路径：清洗后必须仍是 dir 内的普通文件。
func safeZipEntry(dir, rel string) (string, bool) {
	rel = strings.ReplaceAll(rel, "\\", "/")
	rel = strings.TrimSpace(rel)
	if rel == "" || strings.Contains(rel, "\x00") {
		return "", false
	}
	cleaned := filepath.Clean(rel)
	if cleaned == "." || cleaned == ".." || filepath.IsAbs(cleaned) ||
		strings.HasPrefix(cleaned, ".."+string(os.PathSeparator)) {
		return "", false
	}
	full := filepath.Join(dir, cleaned)
	if !strings.HasPrefix(full, filepath.Clean(dir)+string(os.PathSeparator)) {
		return "", false
	}
	return full, true
}

// extractZipInto 把 zip 解压进 dir，返回写入文件数与被跳过的条目名。
//
// 会剥离"单一顶层目录"（zip -r data.zip 1029/ 的常见形态），并限制条目数与
// 解压总量；任何越界条目一律跳过（挡 Zip Slip）。
func extractZipInto(dir string, data []byte) (int, []string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return 0, nil, errors.New("不是有效的 zip")
	}
	if len(zr.File) > testDataZipEntries {
		return 0, nil, fmt.Errorf("压缩包条目过多（%d 项，上限 %d）", len(zr.File), testDataZipEntries)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, nil, errors.New("创建数据目录失败")
	}

	var names []string
	for _, f := range zr.File {
		if !f.FileInfo().IsDir() {
			names = append(names, f.Name)
		}
	}
	root := commonRootDir(names)

	written := 0
	var skipped []string
	var total int64
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rel := f.Name
		if root != "" {
			rel = strings.TrimPrefix(strings.ReplaceAll(rel, "\\", "/"), root+"/")
		}
		dst, ok := safeZipEntry(dir, rel)
		if !ok {
			skipped = append(skipped, f.Name)
			continue
		}
		total += int64(f.UncompressedSize64)
		if total > testDataZipTotal {
			return written, skipped, fmt.Errorf("解压后总量超过 %d 字节上限", testDataZipTotal)
		}
		rc, err := f.Open()
		if err != nil {
			skipped = append(skipped, f.Name)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			rc.Close()
			skipped = append(skipped, f.Name)
			continue
		}
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
		if err != nil {
			rc.Close()
			skipped = append(skipped, f.Name)
			continue
		}
		// LimitReader 既防伪造的大小字段，也防解压炸弹。
		_, err = io.Copy(out, io.LimitReader(rc, testDataZipTotal))
		out.Close()
		rc.Close()
		if err != nil {
			skipped = append(skipped, f.Name)
			continue
		}
		written++
	}
	return written, skipped, nil
}

// commonRootDir 返回所有文件共享的单一顶层目录名（含末尾斜杠语义的裸名）；
// 不共享则返回空串。
func commonRootDir(names []string) string {
	root := ""
	for _, n := range names {
		n = strings.ReplaceAll(n, "\\", "/")
		i := strings.Index(n, "/")
		if i <= 0 {
			return "" // 有文件不在子目录里 → 无共同顶层
		}
		seg := n[:i]
		if root == "" {
			root = seg
			continue
		}
		if seg != root {
			return ""
		}
	}
	return root
}

// ── 小工具 ──────────────────────────────────────────────────────────

// problemSpj 读题目的 spj 标记（0 普通 / 1 SPJ / …）；查不到按 0。
//
// 走 queries 而不是直连 s.db，这样测试可以注入替身，也不必为了读一个字段
// 在测试里架设真实数据库。
func (s *Server) problemSpj(r *http.Request, pid int32) int32 {
	v, err := s.queries().GetProblemSPJ(r.Context(), pid)
	if err != nil {
		return 0
	}
	return int32(parseInt(v))
}

// contentDisposition 生成同时兼容老浏览器的下载头（非 ASCII 名用 RFC 5987）。
func contentDisposition(name string) string {
	ascii := strings.Map(func(r rune) rune {
		if r < 32 || r > 126 || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", ascii, url.PathEscape(name))
}

// testDataHref 拼一个带 pid/action/name 的转义链接。
func testDataHref(pid int32, action, name string) template.URL {
	q := url.Values{"pid": {strconv.Itoa(int(pid))}, "action": {action}, "name": {name}}
	return template.URL("/admin/test_data.php?" + q.Encode())
}

func humanSize(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/float64(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/float64(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}

// isTextLikeName 决定"查看"链接是否显示：只看扩展名，内容仍按上限截断。
func isTextLikeName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".in", ".out", ".ans", ".txt", ".ini", ".json", ".md", ".c", ".cc", ".cpp", ".py", ".java", ".sh":
		return true
	}
	return false
}

// sortTestDataFiles 按"数字前缀 → 文件名"排序，让 0.in/1.in/2.in 顺序自然。
func sortTestDataFiles(files []testDataFile) {
	key := func(f testDataFile) (int64, string) {
		base := f.Name
		if i := strings.LastIndex(base, "."); i > 0 {
			base = base[:i]
		}
		if v, err := strconv.ParseInt(base, 10, 64); err == nil {
			return v, f.Name
		}
		return 1 << 40, f.Name
	}
	sort.SliceStable(files, func(i, j int) bool {
		ki, si := key(files[i])
		kj, sj := key(files[j])
		if ki != kj {
			return ki < kj
		}
		return si < sj
	})
}
