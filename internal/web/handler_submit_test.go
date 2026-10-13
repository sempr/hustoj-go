// 提交链路的端到端测试。
//
// 重点是三件事，按"错了之后用户会看到什么"排序：
//
//  1. result 的两段式推进（14 → 0）。判题机按 result<2 取件，
//     若插入时就是 0 而源码还没落库，judged-go 会取到空源码并报 CE。
//     断言方式是"事务提交瞬间"的可观测状态，而不是函数返回值。
//
//  2. 测试运行用负 problem_id 编码。改掉这个约定，测试运行会被
//     判题机真的取走，在正式榜单上留下一堆垃圾记录。
//
//  3. PHP 等价性：冷却、语言掩码、私有比赛受邀、OI 只留最后一次。
//     这些分支错了都不报错，只是静默地改变谁能看到什么。

package web

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/sempr/hustoj-go/internal/web/config"
)

// submitFixture 是提交链路的测试环境。
type submitFixture struct {
	srv  *Server
	db   *sql.DB
	dir  string
	user string
}

const (
	submitTestUser = "subm1"
	// submitTestSID 是测试用的 Go 会话 ID，必须能通过 sessionIDAllowed。
	submitTestSID = "submitsid0000000000000000000"
)

// newSubmitFixture 构造一个已登录的提交测试环境。
//
// 直接往 Go 会话里塞一条登录态，而不是走注册/登录流程：
// 提交测试关心的是"已登录用户提交"，登录流程由 handler_auth_test 覆盖，
// 在这里重跑一遍只会让测试变慢且互相干扰。
func newSubmitFixture(t *testing.T) *submitFixture {
	t.Helper()

	db := newAuthDB(t)
	dir := t.TempDir()

	cfg := testConfig()
	cfg.OJHome = dir
	cfg.SessionBridge = true
	cfg.SessionDir = dir
	cfg.OJName = "吉尔吉斯倔驴大学OJ"
	cfg.SubmitCooldown = 0 // 默认关闭，逐个用例按需打开
	cfg.VCodePendingThreshold = 0
	cfg.OIOneSolutionOnly = false
	cfg.AppendCode = false
	cfg.LangMask = 0 // 0 = 全部语言可用

	srv := &Server{
		cfg:      cfg,
		log:      testLogger(),
		db:       db,
		sessions: newTestSessionStore(t),
	}
	srv.sessionBridge = NewSessionBridge(dir, cfg.OJName)

	resetSubmitState(t, db)
	t.Cleanup(func() { resetSubmitState(t, db) })

	// 建一个登录用户，并给 Go 会话塞入其身份。
	if _, err := db.Exec(`INSERT INTO users (user_id, email, ip, accesstime, password,
		reg_time, nick, school, group_name, defunct)
		VALUES (?, '', '0', NOW(), '', NOW(), '提交测试', '测试', 'user', 'N')
		ON DUPLICATE KEY UPDATE nick='提交测试'`, submitTestUser); err != nil {
		t.Fatalf("准备测试用户失败: %v", err)
	}

	if err := srv.sessions.Save(context.Background(), submitTestSID, &GoSession{
		UserID:    submitTestUser,
		Nick:      "提交测试",
		GroupName: "user",
	}); err != nil {
		t.Fatalf("写入会话失败: %v", err)
	}

	return &submitFixture{srv: srv, db: db, dir: dir, user: submitTestUser}
}

// request 构造一个带登录 Cookie 与 nosurf token 的提交请求。
func (f *submitFixture) request(t *testing.T, method, target string, form url.Values) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Real-IP", "203.0.113.9")
	// 带上 sid，让 resolveIdentity 命中 Go 会话。
	r.AddCookie(&http.Cookie{Name: f.srv.cfg.GoSessionCookie, Value: submitTestSID})
	return r
}

// clearSubmitState 清理提交相关表，避免影子库历史数据干扰断言。
func resetSubmitState(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	for _, q := range []string{
		`DELETE FROM source_code WHERE solution_id IN (SELECT solution_id FROM solution WHERE user_id = ?)`,
		`DELETE FROM source_code_user WHERE solution_id IN (SELECT solution_id FROM solution WHERE user_id = ?)`,
		`DELETE FROM custominput WHERE solution_id IN (SELECT solution_id FROM solution WHERE user_id = ?)`,
		`DELETE FROM solution WHERE user_id = ?`,
		`DELETE FROM users WHERE user_id = ?`,
	} {
		if _, err := db.ExecContext(ctx, q, submitTestUser); err != nil {
			t.Fatalf("清理失败 (%s): %v", q, err)
		}
	}
	// 复位题目计数：提交会 +1，测试后必须还回去。
	if _, err := db.ExecContext(ctx,
		`UPDATE problem SET submit = 0, accepted = 0 WHERE problem_id IN (900001, 900002)`); err != nil {
		t.Fatalf("复位题目计数失败: %v", err)
	}
}

// seedProblem 建一道测试题目。
//
// description/input/output/hint 在 production 表结构里是 NOT NULL 且无
// 默认值，必须显式给空串，否则 MySQL 报 1364。这些列不参与提交逻辑，
// 但留着空值最省事 —— 改表结构不会连带改测试。
func seedProblem(t *testing.T, db *sql.DB, id int32, defunct string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO problem (problem_id, title, defunct, time_limit, memory_limit,
		description, input, output, hint)
		VALUES (?, '提交测试题', ?, 1000, 65536, '', '', '', '')
		ON DUPLICATE KEY UPDATE defunct = ?`, id, defunct, defunct); err != nil {
		t.Fatalf("准备题目失败: %v", err)
	}
}

// lastSolution 取出该用户最新一条提交。
func lastSolution(t *testing.T, db *sql.DB, userID string) (solutionID int64, result int, problemID int, codeLength int) {
	t.Helper()
	err := db.QueryRow(`SELECT solution_id, result, problem_id, code_length
		FROM solution WHERE user_id = ? ORDER BY solution_id DESC LIMIT 1`, userID).
		Scan(&solutionID, &result, &problemID, &codeLength)
	if err != nil {
		t.Fatalf("查询提交记录失败: %v", err)
	}
	return
}

// TestSubmitInsertsQueuedThenActivates 验证 result 的两段式推进。
//
// 这是整个提交链路最容易写错、且错了最难查的一步：
// 判题机 fetcher 的取件条件是 `result < 2`。若 INSERT 时直接写 0，
// 在源码还没落库的那个瞬间 judged-go 就能把这道题取走，编译出空程序。
// 因此这里断言的是：源码与 result=0 必须同时可见。
func TestSubmitInsertsQueuedThenActivates(t *testing.T) {
	f := newSubmitFixture(t)
	seedProblem(t, f.db, 900001, "N")
	ctx := context.Background()

	in := submitInput{ProblemID: 900001, Language: 0, Source: "int main(){return 0;}"}
	res, deny := f.srv.submitOnce(httptest.NewRecorder(), f.request(t, "POST", "/submit.php", nil), mustIdentity(t, f), in)
	if deny != submitOK {
		t.Fatalf("提交被拒: %v (%s)", deny, deny.message())
	}
	if res.SolutionID == 0 {
		t.Fatal("未返回 solution_id")
	}

	// 事务已提交，此刻源码与 result 必须同时可见。
	var (
		result    int
		judgedSrc string
		userSrc   string
	)
	if err := f.db.QueryRow(`SELECT result FROM solution WHERE solution_id = ?`, res.SolutionID).
		Scan(&result); err != nil {
		t.Fatalf("读取提交记录失败: %v", err)
	}
	if err := f.db.QueryRow(`SELECT source FROM source_code WHERE solution_id = ?`, res.SolutionID).
		Scan(&judgedSrc); err != nil {
		t.Fatalf("送判源码缺失: %v", err)
	}
	if err := f.db.QueryRow(`SELECT source FROM source_code_user WHERE solution_id = ?`, res.SolutionID).
		Scan(&userSrc); err != nil {
		t.Fatalf("用户源码缺失: %v", err)
	}

	if result != 0 {
		t.Errorf("result = %d，期望 0（OJ_WT0 等待判题）。\n"+
			"若为 14 说明源码没跟上；若为 14 之外的中间值说明两段推进写错了", result)
	}
	if judgedSrc != in.Source {
		t.Errorf("送判源码 = %q，期望 %q", judgedSrc, in.Source)
	}
	if userSrc != in.Source {
		t.Errorf("用户源码 = %q，期望 %q", userSrc, in.Source)
	}

	// 题目提交计数应 +1。
	var submitCount int
	if err := f.db.QueryRow(`SELECT submit FROM problem WHERE problem_id = 900001`).
		Scan(&submitCount); err != nil {
		t.Fatalf("读取题目计数失败: %v", err)
	}
	if submitCount != 1 {
		t.Errorf("problem.submit = %d，期望 1", submitCount)
	}
	_ = ctx
}

// TestSubmitTestRunStaysQueued 验证测试运行进队列、存 problem_id=0。
//
// 客户端把 id 取负后再 POST（submitpage.php:576），服务端据此判定。
// 契约：测试运行写 custominput、result 推进到 0（进队列），problem_id 存 0。
// judged-go 取件后检测到 custominput 行会走"只跑自定义输入、不比对"的路径；
// problem_id=0 与所有 `problem_id>0` 的统计口径天然隔离（不会被算进 solved/accepted）。
func TestSubmitTestRunStaysQueued(t *testing.T) {
	f := newSubmitFixture(t)
	seedProblem(t, f.db, 900001, "N")
	f.srv.cfg.AppendCode = false

	in := submitInput{ProblemID: 900001, TestRun: true, Language: 0,
		Source: "print(1)", InputText: "1 2 3"}
	if _, deny := f.srv.submitOnce(httptest.NewRecorder(), f.request(t, "POST", "/submit.php", nil), mustIdentity(t, f), in); deny != submitOK {
		t.Fatalf("测试运行被拒: %v", deny)
	}

	_, _, problemID, _ := lastSolution(t, f.db, submitTestUser)

	var result int
	if err := f.db.QueryRow(`SELECT result FROM solution WHERE user_id = ? ORDER BY solution_id DESC LIMIT 1`, submitTestUser).
		Scan(&result); err != nil {
		t.Fatalf("读取 result 失败: %v", err)
	}
	if problemID != 0 {
		t.Errorf("problem_id = %d，期望 0（测试运行与统计隔离）", problemID)
	}
	if result != 0 {
		t.Errorf("result = %d，期望 0（进判题队列，由 judged-go 跑 custominput）", result)
	}

	// 自定义输入必须落库（判题机据此识别并执行测试运行）。
	var input string
	if err := f.db.QueryRow(`SELECT input_text FROM custominput WHERE solution_id =
		(SELECT MAX(solution_id) FROM solution WHERE user_id = ?)`, submitTestUser).
		Scan(&input); err != nil {
		t.Fatalf("自定义输入缺失: %v", err)
	}
	if input != "1 2 3" {
		t.Errorf("input_text = %q，期望 %q", input, "1 2 3")
	}

	// 测试运行不影响题目计数。
	var submitCount int
	if err := f.db.QueryRow(`SELECT submit FROM problem WHERE problem_id = 900001`).Scan(&submitCount); err != nil {
		t.Fatalf("读取题目计数失败: %v", err)
	}
	if submitCount != 0 {
		t.Errorf("problem.submit = %d，测试运行不应计入提交数，期望 0", submitCount)
	}
}

// TestSubmitRejectsForbiddenLanguage 验证语言掩码是"命中即拒绝"。
//
// PHP：$langmask & (1 << $language)。掩码里为 0 的语言可用。
// 写反会让站点上所有语言都不可提交，而且不报错。
func TestSubmitRejectsForbiddenLanguage(t *testing.T) {
	f := newSubmitFixture(t)
	seedProblem(t, f.db, 900001, "N")
	// 只禁 0 号语言（C）。
	f.srv.cfg.LangMask = 1

	in := submitInput{ProblemID: 900001, Language: 0, Source: "int main(){return 0;}"}
	_, deny := f.srv.submitOnce(httptest.NewRecorder(), f.request(t, "POST", "/submit.php", nil), mustIdentity(t, f), in)
	if deny != submitLanguageForbidden {
		t.Errorf("被禁语言提交: deny = %v，期望 %v", deny, submitLanguageForbidden)
	}

	// 1 号语言（G++）应放行。
	in.Language = 1
	if _, deny := f.srv.submitOnce(httptest.NewRecorder(), f.request(t, "POST", "/submit.php", nil), mustIdentity(t, f), in); deny != submitOK {
		t.Errorf("未禁语言提交: deny = %v，期望放行", deny)
	}
}

// TestSubmitLanguageOutOfRangeResetsToZero 验证越界语言被归零。
//
// submit.php:164-166：超出语言表范围时归零，随后由掩码检查决定去留。
func TestSubmitLanguageOutOfRangeResetsToZero(t *testing.T) {
	f := newSubmitFixture(t)
	seedProblem(t, f.db, 900001, "N")
	f.srv.cfg.LangMask = 0

	// 语言表只有 26 项，传 99 越界。
	in := submitInput{ProblemID: 900001, Language: 99, Source: "int main(){return 0;}"}
	if _, deny := f.srv.submitOnce(httptest.NewRecorder(), f.request(t, "POST", "/submit.php", nil), mustIdentity(t, f), in); deny != submitOK {
		t.Errorf("越界语言: deny = %v，期望归零后放行", deny)
	}

	var language int
	if err := f.db.QueryRow(`SELECT language FROM solution WHERE user_id = ? ORDER BY solution_id DESC LIMIT 1`, submitTestUser).
		Scan(&language); err != nil {
		t.Fatalf("读取语言失败: %v", err)
	}
	if language != 0 {
		t.Errorf("language = %d，期望归零为 0", language)
	}
}

// TestSubmitCooldownSilentlyRedirects 验证冷却期静默跳转。
//
// submit.php:289-312：冷却期内不报错、不写库，直接跳回自己的 status。
// 这里同时验证"确实没写库"—— 只断言跳转的话，一个"写了库再跳转"的
// 实现也能通过。
//
// 走 handleSubmit 而非 submitOnce：冷却判定在准入层（handleSubmit），
// 不在写入层（submitOnce），直接调 submitOnce 会绕过它。
func TestSubmitCooldownSilentlyRedirects(t *testing.T) {
	f := newSubmitFixture(t)
	seedProblem(t, f.db, 900001, "N")
	f.srv.cfg.SubmitCooldown = 10
	f.srv.cfg.LangMask = 0

	// 先正常提交一次，制造"刚刚提交过"。
	f.post(t, "id=900001&language=0&source="+url.QueryEscape("int main(){return 0;}"))
	if n := f.countSolutions(t); n != 1 {
		t.Fatalf("首次提交后记录数 = %d，期望 1", n)
	}

	// 冷却期内再提交。
	w := f.post(t, "id=900001&language=0&source="+url.QueryEscape("int main(){return 1;}"))

	// PHP 走 header("Location: ...")，PHP 发的是 302（不是 303）。
	if w.Code != http.StatusFound {
		t.Errorf("状态码 = %d，期望 302（PHP header(\"Location:\") 的等价码）", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "status.php?user_id="+submitTestUser) {
		t.Errorf("Location = %q，期望跳回自己的 status", loc)
	}
	if n := f.countSolutions(t); n != 1 {
		t.Errorf("提交记录数 = %d，冷却期不应写库，期望仍为 1", n)
	}
}

// post 以登录态提交一次表单，返回响应记录器。
func (f *submitFixture) post(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/submit.php", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Real-IP", "203.0.113.9")
	r.AddCookie(&http.Cookie{Name: f.srv.cfg.GoSessionCookie, Value: submitTestSID})
	f.srv.handleSubmit(w, r)
	return w
}

// countSolutions 统计该测试用户的提交条数。
func (f *submitFixture) countSolutions(t *testing.T) int {
	t.Helper()
	var n int
	if err := f.db.QueryRow(`SELECT count(1) FROM solution WHERE user_id = ?`, submitTestUser).Scan(&n); err != nil {
		t.Fatalf("统计提交失败: %v", err)
	}
	return n
}

// TestSubmitRejectsTooShortAndTooLong 验证长度上下界。
//
// 下界是 2 字节（PHP 的 $len < 2），上界 65536 字节。
func TestSubmitRejectsTooShortAndTooLong(t *testing.T) {
	f := newSubmitFixture(t)
	seedProblem(t, f.db, 900001, "N")
	f.srv.cfg.LangMask = 0

	for _, tc := range []struct {
		name   string
		source string
		want   submitDeny
	}{
		{"空", "", submitTooShort},
		{"单字节", "a", submitTooShort},
		{"刚好两字节", "ab", submitOK},
		{"刚好上限", strings.Repeat("a", 65535), submitOK},
		{"超一字节", strings.Repeat("a", 65536), submitTooLong},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := submitInput{ProblemID: 900001, Language: 0, Source: tc.source}
			_, deny := f.srv.submitOnce(httptest.NewRecorder(), f.request(t, "POST", "/submit.php", nil), mustIdentity(t, f), in)
			if deny != tc.want {
				t.Errorf("源码长度 %d: deny = %v，期望 %v", len(tc.source), deny, tc.want)
			}
		})
	}
}

// TestSubmitRejectsMissingProblem 验证题目不存在的判定。
func TestSubmitRejectsMissingProblem(t *testing.T) {
	f := newSubmitFixture(t)
	f.srv.cfg.LangMask = 0

	in := submitInput{ProblemID: 999999999, Language: 0, Source: "int main(){return 0;}"}
	_, deny := f.srv.submitOnce(httptest.NewRecorder(), f.request(t, "POST", "/submit.php", nil), mustIdentity(t, f), in)
	if deny != submitProblemMissing {
		t.Errorf("不存在的题目: deny = %v，期望 %v", deny, submitProblemMissing)
	}
}

// TestSubmitRejectsDefunctProblemForNormalUser 验证已删除题目的准入。
//
// 管理员/题目编辑/题目审核可以向已删除题目提交（submit.php:71），
// 普通用户不行。测试运行也不受此限制。
func TestSubmitRejectsDefunctProblemForNormalUser(t *testing.T) {
	f := newSubmitFixture(t)
	seedProblem(t, f.db, 900002, "Y")
	f.srv.cfg.LangMask = 0

	in := submitInput{ProblemID: 900002, Language: 0, Source: "int main(){return 0;}"}
	_, deny := f.srv.submitOnce(httptest.NewRecorder(), f.request(t, "POST", "/submit.php", nil), mustIdentity(t, f), in)
	if deny != submitProblemDefunct {
		t.Errorf("普通用户提交已删除题目: deny = %v，期望 %v", deny, submitProblemDefunct)
	}
}

// TestSubmitAppendsTemplateCode 验证 prepend/append 拼接。
//
// 送判源码与用户原文分开存：source_code 是拼好的，source_code_user
// 是原文。判题读前者，"查看我的提交"读后者。
func TestSubmitAppendsTemplateCode(t *testing.T) {
	f := newSubmitFixture(t)
	seedProblem(t, f.db, 900001, "N")
	f.srv.cfg.LangMask = 0
	f.srv.cfg.AppendCode = true

	// data/<id>/prepend.c 与 append.c
	dir := fmt.Sprintf("%s/data/900001", f.srv.cfg.OJHome)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(dir+"/prepend.c", []byte("#include <stdio.h>"), 0o644); err != nil {
		t.Fatalf("写 prepend 失败: %v", err)
	}
	if err := os.WriteFile(dir+"/append.c", []byte("// tail"), 0o644); err != nil {
		t.Fatalf("写 append 失败: %v", err)
	}

	const userSrc = "int main(){return 0;}"
	in := submitInput{ProblemID: 900001, Language: 0, Source: userSrc}
	res, deny := f.srv.submitOnce(httptest.NewRecorder(), f.request(t, "POST", "/submit.php", nil), mustIdentity(t, f), in)
	if deny != submitOK {
		t.Fatalf("提交被拒: %v", deny)
	}

	var judged, user string
	if err := f.db.QueryRow(`SELECT source FROM source_code WHERE solution_id = ?`, res.SolutionID).Scan(&judged); err != nil {
		t.Fatalf("读送判源码失败: %v", err)
	}
	if err := f.db.QueryRow(`SELECT source FROM source_code_user WHERE solution_id = ?`, res.SolutionID).Scan(&user); err != nil {
		t.Fatalf("读用户源码失败: %v", err)
	}

	want := "#include <stdio.h>\n" + userSrc + "\n// tail"
	if judged != want {
		t.Errorf("送判源码 =\n%q\n期望\n%q", judged, want)
	}
	if user != userSrc {
		t.Errorf("用户源码 = %q，期望保持原文 %q", user, userSrc)
	}

	// code_length 记的是送判源码长度，不是用户原文长度。
	var codeLength int
	if err := f.db.QueryRow(`SELECT code_length FROM solution WHERE solution_id = ?`, res.SolutionID).Scan(&codeLength); err != nil {
		t.Fatalf("读 code_length 失败: %v", err)
	}
	if codeLength != len(want) {
		t.Errorf("code_length = %d，期望 %d（送判源码的字节长度）", codeLength, len(want))
	}
}

// TestStatusURLIncludesTopAndFixed 验证提交后的跳转参数。
//
// submit.php:476-483 会带上 &cid=&top=<id>&fixed=，
// status.php 用 fixed 把新提交固定到可见位置。少了它用户会以为
// 提交没生效。
func TestStatusURLIncludesTopAndFixed(t *testing.T) {
	f := newSubmitFixture(t)

	got := f.srv.statusURL("alice", 1001, 555)
	want := "status.php?user_id=alice&cid=1001&top=555&fixed="
	if got != want {
		t.Errorf("statusURL = %q，期望 %q", got, want)
	}

	// 练习模式不带 cid。
	if got := f.srv.statusURL("alice", 0, 555); got != "status.php?user_id=alice" {
		t.Errorf("练习模式 statusURL = %q，期望不含 cid", got)
	}
}

// TestAllowedLanguagesInvertsMask 验证语言列表是掩码的补集。
//
// 掩码语义是"命中即拒绝"，所以可用语言 = 掩码里为 0 的位。
func TestAllowedLanguagesInvertsMask(t *testing.T) {
	f := newSubmitFixture(t)

	// 掩码全 0：全部可用。
	if got := len(f.srv.allowedLanguages(0)); got != len(languageExt) {
		t.Errorf("掩码 0 时可用语言数 = %d，期望 %d（全部）", got, len(languageExt))
	}
	// 禁掉 0 号：少一个。
	if got := len(f.srv.allowedLanguages(1)); got != len(languageExt)-1 {
		t.Errorf("掩码 1 时可用语言数 = %d，期望 %d", got, len(languageExt)-1)
	}
}

// TestParseSubmitInputModes 验证两种提交模式的字段归一。
func TestParseSubmitInputModes(t *testing.T) {
	t.Run("练习模式读 id", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/submit.php", strings.NewReader("id=123&language=1&source=abc"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		in, err := parseSubmitInput(r)
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if in.IsContest || in.ProblemID != 123 || in.Language != 1 || in.Source != "abc" {
			t.Errorf("解析结果 = %+v，期望练习模式 id=123", in)
		}
	})

	t.Run("比赛模式读 cid+pid", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/submit.php", strings.NewReader("cid=1001&pid=2&language=1&source=abc"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		in, err := parseSubmitInput(r)
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if !in.IsContest || in.ContestID != 1001 || in.Num != 2 {
			t.Errorf("解析结果 = %+v，期望比赛模式 cid=1001 pid=2", in)
		}
	})

	t.Run("负 cid 取绝对值", func(t *testing.T) {
		// 负 cid 表示测试运行（submit.php:61-63）。
		r := httptest.NewRequest("POST", "/submit.php", strings.NewReader("cid=-1001&pid=2&language=1&source=abc"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		in, err := parseSubmitInput(r)
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if in.ContestID != 1001 {
			t.Errorf("ContestID = %d，期望 1001（负 cid 取绝对值）", in.ContestID)
		}
		if !in.TestRun {
			t.Error("TestRun = false，期望 true（负 cid 即测试运行）")
		}
	})

	t.Run("cid=0 退回练习模式", func(t *testing.T) {
		// PHP：isset($_POST['cid']) 成立但 cid<=0 时走练习分支且 id=0。
		r := httptest.NewRequest("POST", "/submit.php", strings.NewReader("cid=0&pid=1&language=1&source=abc"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		in, err := parseSubmitInput(r)
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if in.IsContest {
			t.Errorf("cid=0 时 IsContest = true，期望退回练习模式")
		}
	})

	t.Run("负 id 是测试运行", func(t *testing.T) {
		// submitpage.php:576 把 id 取负后再 POST。
		r := httptest.NewRequest("POST", "/submit.php", strings.NewReader("id=-123&language=1&source=abc"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		in, err := parseSubmitInput(r)
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if !in.TestRun {
			t.Error("TestRun = false，期望 true")
		}
		if in.ProblemID != 123 {
			t.Errorf("ProblemID = %d，期望 123（取绝对值）", in.ProblemID)
		}
	})

	t.Run("ajax 标记", func(t *testing.T) {
		r := httptest.NewRequest("POST", "/submit.php?ajax=1", strings.NewReader("id=1&source=abc"))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		in, err := parseSubmitInput(r)
		if err != nil {
			t.Fatalf("解析失败: %v", err)
		}
		if !in.Ajax {
			t.Error("Ajax = false，期望 true")
		}
	})
}

// TestSubmitRequiresLogin 验证未登录被拒。
func TestSubmitRequiresLogin(t *testing.T) {
	f := newSubmitFixture(t)
	seedProblem(t, f.db, 900001, "N")

	w := httptest.NewRecorder()
	// 不带任何 Cookie：resolveIdentity 判为匿名。
	r := httptest.NewRequest("POST", "/submit.php", strings.NewReader("id=900001&language=1&source=abc"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	f.srv.handleSubmit(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("状态码 = %d，期望 200（PHP 一律 exit(0) 渲染错误页）", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "请先登录") {
		t.Errorf("响应未提示登录：\n%s", body)
	}

	var count int
	if err := f.db.QueryRow(`SELECT count(1) FROM solution WHERE user_id = ?`, submitTestUser).Scan(&count); err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if count != 0 {
		t.Errorf("未登录却写入了 %d 条提交记录", count)
	}
}

// TestSubmitPageRequiresLogin 验证提交页对未登录用户不可见。
func TestSubmitPageRequiresLogin(t *testing.T) {
	f := newSubmitFixture(t)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/submitpage.php?id=900001", nil)
	f.srv.handleSubmitPage(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("状态码 = %d，期望 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "请先登录") {
		t.Errorf("未提示登录：\n%s", w.Body.String())
	}
}

// TestSubmitEndToEndThroughMux 走真实 HTTP 链路提交一次。
//
// 这一条是切流能否成立的关键，必须过 mux 而不是直接调 handler：
//
//  1. nosurf 中间件签发 csrf_token，写进提交页表单；
//  2. 同一份 token 原样回 POST；
//  3. nosurf 校验通过后，handler 拿到表单并入库。
//
// 直接调 handleSubmit 会绕过 nosurf，测不出"表单里的 token 与 Cookie
// 是否配对"这类问题 —— 而这恰好是早期登录测试踩过的坑。
func TestSubmitEndToEndThroughMux(t *testing.T) {
	f := newSubmitFixture(t)
	seedProblem(t, f.db, 900001, "N")
	f.srv.cfg.LangMask = 0
	f.srv.cfg.SubmitCooldown = 0

	// 第一次请求顺带接收 nosurf 下发的 csrf_token Cookie。
	pageRec := httptest.NewRecorder()
	pageReq := httptest.NewRequest("GET", "/submitpage.php?id=900001", nil)
	pageReq.AddCookie(&http.Cookie{Name: f.srv.cfg.GoSessionCookie, Value: submitTestSID})
	f.srv.handler().ServeHTTP(pageRec, pageReq)

	if pageRec.Code != http.StatusOK {
		t.Fatalf("GET /submitpage.php = %d，期望 200：\n%s", pageRec.Code, pageRec.Body.String())
	}
	body := pageRec.Body.String()
	for _, want := range []string{
		`name="csrf_token"`, // nosurf 隐藏字段
		`name="id"`,         // 练习模式发 id
		`name="source"`,
		`name="language"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("提交页缺少 %s：\n%s", want, body)
		}
	}
	token := tokenFromBody(t, body)

	// nosurf 的双提交 Cookie 必须在页面上被下发。
	var csrfCookie *http.Cookie
	for _, c := range pageRec.Result().Cookies() {
		if c.Name == csrfFieldName {
			csrfCookie = c
		}
	}
	if csrfCookie == nil {
		t.Fatal("提交页未下发 nosurf 的 csrf_token Cookie")
	}

	// 带上两个 cookie 原样提交。
	formRec := httptest.NewRecorder()
	formReq := httptest.NewRequest("POST", "/submit.php", strings.NewReader(url.Values{
		"id":         {"900001"},
		"language":   {"0"},
		"source":     {"int main(){return 0;}"},
		"csrf_token": {token},
	}.Encode()))
	formReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	formReq.Header.Set("X-Real-IP", "203.0.113.9")
	// 同源 POST 必须带 Origin：真实浏览器一定带，nosurf 拿它（或
	// Sec-Fetch-Site / Referer）做第二道防线。不带的话请求会因
	// "来源不明"被先一步拒掉，测到的就不是 token 校验本身。
	formReq.Header.Set("Origin", sameOrigin)
	formReq.AddCookie(&http.Cookie{Name: f.srv.cfg.GoSessionCookie, Value: submitTestSID})
	formReq.AddCookie(csrfCookie)
	f.srv.handler().ServeHTTP(formRec, formReq)

	// PHP 的 header("Location: ...") 发的是 302，不是 303。
	if formRec.Code != http.StatusFound {
		t.Fatalf("POST /submit.php = %d，期望 302：\n%s", formRec.Code, formRec.Body.String())
	}
	if loc := formRec.Header().Get("Location"); !strings.Contains(loc, "status.php?user_id="+submitTestUser) {
		t.Errorf("Location = %q，期望跳到自己的 status", loc)
	}

	_, _, problemID, _ := lastSolution(t, f.db, submitTestUser)
	if problemID != 900001 {
		t.Errorf("problem_id = %d，期望 900001", problemID)
	}
	var result int
	if err := f.db.QueryRow(`SELECT result FROM solution WHERE user_id = ?`, submitTestUser).
		Scan(&result); err != nil {
		t.Fatalf("读取 result 失败: %v", err)
	}
	if result != 0 {
		t.Errorf("result = %d，期望 0（进判题队列）", result)
	}
}

// TestSubmitRejectsCrossOrigin 验证跨站提交被 nosurf 拦下。
//
// 这是切流后唯一挡在"任意网站替用户提交"前面的东西。
func TestSubmitRejectsCrossOrigin(t *testing.T) {
	f := newSubmitFixture(t)
	seedProblem(t, f.db, 900001, "N")
	f.srv.cfg.LangMask = 0

	pageRec := httptest.NewRecorder()
	pageReq := httptest.NewRequest("GET", "/submitpage.php?id=900001", nil)
	pageReq.AddCookie(&http.Cookie{Name: f.srv.cfg.GoSessionCookie, Value: submitTestSID})
	f.srv.handler().ServeHTTP(pageRec, pageReq)
	token := tokenFromBody(t, pageRec.Body.String())

	var csrfCookie *http.Cookie
	for _, c := range pageRec.Result().Cookies() {
		if c.Name == csrfFieldName {
			csrfCookie = c
		}
	}
	if csrfCookie == nil {
		t.Fatal("未拿到 csrf Cookie")
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/submit.php", strings.NewReader(url.Values{
		"id":         {"900001"},
		"language":   {"0"},
		"source":     {"int main(){return 0;}"},
		"csrf_token": {token},
	}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://evil.example.com")
	req.AddCookie(&http.Cookie{Name: f.srv.cfg.GoSessionCookie, Value: submitTestSID})
	req.AddCookie(csrfCookie)
	f.srv.handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusFound {
		t.Errorf("跨站提交未被拦截，返回 302 并 Location=%q", rec.Header().Get("Location"))
	}
	if n := f.countSolutions(t); n != 0 {
		t.Errorf("跨站请求写入了 %d 条提交记录，期望 0", n)
	}
}

// mustIdentity 取出测试用户的登录身份。
func mustIdentity(t *testing.T, f *submitFixture) Identity {
	t.Helper()
	id := Identity{UserID: f.user, Nick: "提交测试", LoggedIn: true, SessionID: submitTestSID}
	return id
}

// TestSubmitConfigDefaultsMatchProduction 钉住提交相关的配置默认值。
//
// 这些值与 db_info.inc.php 一一对应。改默认值等于改变线上判题语义，
// 而这类改动不会让任何测试变红 —— 所以显式钉住。
func TestSubmitConfigDefaultsMatchProduction(t *testing.T) {
	cfg := config.Default()

	if cfg.LangMask != 16579600 {
		t.Errorf("LangMask = %d，期望 16579600（db_info.inc.php:29）", cfg.LangMask)
	}
	if cfg.SubmitCooldown != 10 {
		t.Errorf("SubmitCooldown = %d，期望 10（db_info.inc.php:90）", cfg.SubmitCooldown)
	}
	if !cfg.AppendCode {
		t.Error("AppendCode = false，期望 true（db_info.inc.php:35）")
	}
	if cfg.VCodePendingThreshold != 50 {
		t.Errorf("VCodePendingThreshold = %d，期望 50（submit.php:26）", cfg.VCodePendingThreshold)
	}
	// 注意是 65535 而不是 PHP 的 65536：source 表的 source 列是 TEXT
	// （65535 字节），且线上开着 STRICT_TRANS_TABLES。放行 65536 会让
	// INSERT 报 1406，PHP 侧表现为"点了提交没反应"。详见 config.go。
	if cfg.MaxSourceLength != 65535 {
		t.Errorf("MaxSourceLength = %d，期望 65535（TEXT 列容量，非 PHP 的 65536）", cfg.MaxSourceLength)
	}
}
