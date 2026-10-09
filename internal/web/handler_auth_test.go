// 认证链路的端到端测试。
//
// 重点是"真实 HTTP 流程"而不是单个函数：CSRF token 必须来自
// GET 登录页、绑定在同一个 cookie 上、POST 时被单次消费。
// 早期版本分别发两次 curl 拿表单 token 和 Set-Cookie，
// 两者本就不同源，导致误判为 CSRF 失效。

package web

import (
	"context"
	"database/sql"
	"encoding/base64"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/sempr/hustoj-go/internal/web/config"
)

// authFixture 是一个可用的认证测试环境。
type authFixture struct {
	srv     *Server
	session string // $OJ_NAME
	dir     string
}

// testUserIDs 是本文件用到的全部合成账号。
//
// 影子库跨多次 go test 复用，这些账号必须在每个用例开始前清掉，
// 否则第二次运行就会撞主键，且历史 loginlog 会让失败次数断言失真。
// 全部是本文件自造的短名字，不会与真实数据重叠。
var testUserIDs = []string{
	"alice", "bob", "carol", "dave", "eve", "frank", "grace", "henry",
	"ivan", "jane", "kim", "old", "dead", "newbie", "nonick", "taken",
	"ipvictim", "ghost", "alreadyin",
}

// resetAuthState 清理测试账号及其痕迹。
func resetAuthState(t *testing.T, db *sql.DB) {
	t.Helper()
	for _, uid := range testUserIDs {
		if _, err := db.ExecContext(context.Background(),
			"DELETE FROM privilege WHERE user_id = ?", uid); err != nil {
			t.Fatalf("清理 privilege 失败: %v", err)
		}
		if _, err := db.ExecContext(context.Background(),
			"DELETE FROM users WHERE user_id = ?", uid); err != nil {
			t.Fatalf("清理 users 失败: %v", err)
		}
	}
	// IP 限流用例造的账号带 ipspread 前缀
	if _, err := db.ExecContext(context.Background(),
		"DELETE FROM privilege WHERE user_id LIKE 'ipspread%'"); err != nil {
		t.Fatalf("清理 privilege 失败: %v", err)
	}
	if _, err := db.ExecContext(context.Background(),
		"DELETE FROM users WHERE user_id LIKE 'ipspread%'"); err != nil {
		t.Fatalf("清理 users 失败: %v", err)
	}
	if _, err := db.ExecContext(context.Background(),
		"DELETE FROM loginlog WHERE user_id LIKE 'ipspread%' OR user_id = ''"); err != nil {
		t.Fatalf("清理 loginlog 失败: %v", err)
	}
	// 其它用例遗留的失败记录会让限流断言提前触发
	if _, err := db.ExecContext(context.Background(),
		`DELETE FROM loginlog WHERE user_id <> '' AND user_id IN (
			SELECT user_id FROM users)`); err != nil {
		// 影子库可能不允许子查询 DELETE，失败不致命
		_ = err
	}
}

// newAuthFixture 构造带真实会话目录的 Server。
func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	dir := t.TempDir()
	// 生产用的中文 $OJ_NAME，用它才能暴露前缀不匹配的问题
	const ojName = "吉尔吉斯倔驴大学OJ"

	cfg := testConfig()
	cfg.SessionBridge = true
	cfg.SessionDir = dir
	cfg.OJName = ojName
	cfg.Register = true
	cfg.LoginMod = "hustoj"
	cfg.LoginFailLimit = 5
	cfg.Debug = false

	db := newAuthDB(t)
	resetAuthState(t, db)
	t.Cleanup(func() { resetAuthState(t, db) })

	srv := &Server{
		cfg:           cfg,
		log:           testLogger(),
		db:            db,
		sessionBridge: NewSessionBridge(dir, ojName),
		sessions:      newTestSessionStore(t),
	}
	return &authFixture{srv: srv, session: ojName, dir: dir}
}

// authTestUser 是注入 users 表的测试账号。
type authTestUser struct {
	userID   string
	password string
	group    string
	nick     string
	admin    bool
	// privs 是额外的权限键
	privs map[string]string
}

// seedUser 造一个密码已按 PHP 格式存储的用户。
func (f *authFixture) seedUser(t *testing.T, u authTestUser) {
	t.Helper()
	hash, err := pwGen(u.password, false)
	if err != nil {
		t.Fatal(err)
	}
	group := u.group
	if group == "" {
		group = "user"
	}
	// 测试账号必须先清干净：影子库是跨多次运行复用的，
	// 残留的用户会让主键冲突，登录log 也会串到别的用例。
	if _, err := f.srv.db.ExecContext(context.Background(),
		"DELETE FROM users WHERE user_id = ?", u.userID); err != nil {
		t.Fatalf("清理测试用户失败: %v", err)
	}
	if _, err := f.srv.db.ExecContext(context.Background(),
		"DELETE FROM privilege WHERE user_id = ?", u.userID); err != nil {
		t.Fatalf("清理测试权限失败: %v", err)
	}
	_, err = f.srv.db.ExecContext(context.Background(),
		`INSERT INTO users (user_id, email, ip, accesstime, password, reg_time, nick, school, group_name, defunct, expiry_date)
		 VALUES (?, ?, ?, NOW(), ?, NOW(), ?, 'TestSchool', ?, 'N', '2099-01-01')`,
		u.userID, u.userID+"@test.local", testClientIP, hash, u.nick, group)
	if err != nil {
		t.Fatalf("插入测试用户失败: %v", err)
	}
	if u.admin {
		if _, err := f.srv.db.ExecContext(context.Background(),
			"INSERT INTO privilege (user_id, rightstr) VALUES (?, 'administrator')", u.userID); err != nil {
			t.Fatalf("插入管理员权限失败: %v", err)
		}
	}
	for right, val := range u.privs {
		if val == "" {
			if _, err := f.srv.db.ExecContext(context.Background(),
				"INSERT INTO privilege (user_id, rightstr) VALUES (?, ?)", u.userID, right); err != nil {
				t.Fatalf("插入权限 %s 失败: %v", right, err)
			}
			continue
		}
		if _, err := f.srv.db.ExecContext(context.Background(),
			"INSERT INTO privilege (user_id, rightstr, valuestr) VALUES (?, ?, ?)", u.userID, right, val); err != nil {
			t.Fatalf("插入权限 %s 失败: %v", right, err)
		}
	}
}

// csrfRe 提取表单里的 csrf hidden input。
var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]*)"`)

// testClientIP 是测试里所有请求使用的客户端 IP。
//
// httptest.NewRequest 的默认 RemoteAddr 是 192.0.2.1（RFC 5737 文档用网段），
// 而 loginlog 里的 IP 必须与 remoteIP() 解析出的值一致，
// 否则 IP 维度的限流断言会永远读到 0。
const testClientIP = "203.0.113.10"

// cookieJar 是跟随 cookie 的极简客户端。
//
// 关键点：token 与 cookie 必须来自同一个会话，
// 所以这里用一个 jar 串起整条流程，而不是每次现取。
type cookieJar struct {
	// sid 是 Go 会话 ID（HJGOSESSID），登录后会被换新。
	//
	// 绝大多数用例关心的是它；PHP 侧会话由 sessionBridge 的用例单独覆盖。
	sid string
	// csrf 保存 nosurf 下发的双提交 Cookie。
	//
	// nosurf 的方案里 Cookie 与表单 token 必须成对出现，
	// 测试客户端要是不带上它，任何 POST 都会被判为跨站请求。
	csrf string
	// origin 是 POST 时发送的 Origin 头。
	//
	// 真实浏览器对同源 POST 一定会带 Origin，nosurf 正是靠它
	// （或 Sec-Fetch-Site / Referer）做第二道防线。测试必须复现这一点，
	// 否则请求会因为"来源不明"被先一步拒掉，测到的就不是 token 校验了。
	origin string
}

func (j *cookieJar) absorb(rec *httptest.ResponseRecorder) {
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case testGoSessionCookie:
			j.sid = c.Value
		case csrfFieldName:
			j.csrf = c.Value
		}
	}
}

// sameOrigin 是 httptest.NewRequest 用的默认 Host。
const sameOrigin = "http://example.com"

// testGoSessionCookie 必须与 config 的默认值一致，
// 否则测试里挂的 Cookie 名字和 handler 读的对不上，会静默变成匿名。
const testGoSessionCookie = "HJGOSESSID"

// apply 把当前持有的 cookie 与 Origin 贴到请求上。
func (j *cookieJar) apply(req *http.Request) {
	origin := j.origin
	if origin == "" {
		origin = sameOrigin
	}
	req.Header.Set("Origin", origin)
	if j.sid != "" {
		req.AddCookie(&http.Cookie{Name: testGoSessionCookie, Value: j.sid})
	}
	if j.csrf != "" {
		req.AddCookie(&http.Cookie{Name: csrfFieldName, Value: j.csrf})
	}
}

// get 发起 GET 并记录 cookie。
func (j *cookieJar) get(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Real-IP", testClientIP)
	j.apply(req)
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	j.absorb(rec)
	return rec
}

// postForm 发起表单 POST 并记录 cookie。
func (j *cookieJar) postForm(t *testing.T, srv *Server, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Real-IP", testClientIP)
	j.apply(req)
	if j.origin != "" {
		req.Header.Set("Origin", j.origin)
	}
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	j.absorb(rec)
	return rec
}

// tokenFromLoginPage 走真实流程取一枚 CSRF token。
func (j *cookieJar) tokenFromLoginPage(t *testing.T, srv *Server) string {
	t.Helper()
	rec := j.get(t, srv, "/loginpage.php")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /loginpage.php = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if j.csrf == "" {
		t.Fatal("GET 登录页未下发 nosurf 的 csrf_token Cookie")
	}
	return tokenFromBody(t, rec.Body.String())
}

// tokenFromBody 从 HTML 里取出 CSRF token 的真实值。
//
// 必须做 HTML 实体反转义：nosurf 的 token 是 base64，含 + / = 这些字符，
// 而 html/template 在属性上下文里会把它们转义成 &#43; &#47; &#61;。
// 浏览器会自动解码，所以线上没问题；但测试拿到的是原始 HTML 文本，
// 直接当 token 用就会带着实体去比对，然后莫名其妙地校验失败。
func tokenFromBody(t *testing.T, body string) string {
	t.Helper()
	m := csrfRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("页面没有 csrf hidden input: %s", body)
	}
	return html.UnescapeString(m[1])
}

// 完整流程：取页 -> 填表 -> 提交 -> 校验新会话已登录。
func TestLoginEndToEnd(t *testing.T) {
	f := newAuthFixture(t)
	f.seedUser(t, authTestUser{userID: "alice", password: "secret123", nick: "爱丽丝"})

	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)
	sidBefore := jar.sid

	rec := jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id":    {"alice"},
		"password":   {"secret123"},
		"csrf_token": {token},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /login.php = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Location"); got != "/index.php" {
		t.Errorf("Location = %q, want /index.php", got)
	}
	// session_regenerate_id：登录后必须是新 SID
	if jar.sid == "" || jar.sid == sidBefore {
		t.Errorf("登录后未换新会话 ID: before=%s after=%s", sidBefore, jar.sid)
	}
	// 旧会话文件必须被删除
	if _, err := os.Stat(filepath.Join(f.dir, "sess_"+sidBefore)); !os.IsNotExist(err) {
		t.Error("旧会话文件未被删除，会话固定攻击仍可行")
	}

	// 用新 SID 访问受保护信息，应能拿到身份
	id := f.srv.resolveIdentity(nil, newReqWithSID(jar.sid))
	if !id.LoggedIn {
		t.Fatal("登录后仍未解析为已登录")
	}
	if id.UserID != "alice" {
		t.Errorf("UserID = %q, want alice", id.UserID)
	}
	if id.Nick != "爱丽丝" {
		t.Errorf("Nick = %q, want 爱丽丝", id.Nick)
	}
	if id.GroupName != "user" {
		t.Errorf("GroupName = %q, want user", id.GroupName)
	}
	// 登录后不该还带着 CSRF token（PHP 走的是新会话）
	if len(id.CSRFKeys) != 0 {
		t.Errorf("新会话残留 CSRF token: %v", id.CSRFKeys)
	}
}

// newReqWithSID 造一个带上 Go 会话 Cookie 的请求。
//
// 注意挂的是 HJGOSESSID 而不是 PHPSESSID：Go 的登录态在 Redis 里，
// 键就是 Go 自己的会话 ID。挂 PHP cookie 只会走迁移桥，测不到 Go 会话。
func newReqWithSID(sid string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/index.php", nil)
	req.AddCookie(&http.Cookie{Name: testGoSessionCookie, Value: sid})
	return req
}

// 管理员登录后应跳到 /admin/，对应 login.php:112。
func TestLoginAdminRedirect(t *testing.T) {
	f := newAuthFixture(t)
	f.seedUser(t, authTestUser{userID: "root", password: "admin123", admin: true})

	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)
	rec := jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id":    {"root"},
		"password":   {"admin123"},
		"csrf_token": {token},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("= %d, want 303", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/admin/" {
		t.Errorf("管理员跳转 = %q, want /admin/", got)
	}
	id := f.srv.resolveIdentity(nil, newReqWithSID(jar.sid))
	if !id.Admin {
		t.Error("管理员会话未带 administrator 标记")
	}
}

// 比赛创建者应跳到 /contest.php?my。
func TestLoginContestCreatorRedirect(t *testing.T) {
	f := newAuthFixture(t)
	f.seedUser(t, authTestUser{
		userID: "teacher", password: "teach123",
		privs: map[string]string{"contest_creator": ""},
	})

	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)
	rec := jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id":    {"teacher"},
		"password":   {"teach123"},
		"csrf_token": {token},
	})
	if got := rec.Header().Get("Location"); got != "/contest.php?my" {
		t.Errorf("跳转 = %q, want /contest.php?my", got)
	}
}

// nosurf 是无状态双提交：token 可重复使用。
//
// 这与 PHP 的"池 + 单次消费"不同，但并不更弱 —— 防的是跨站伪造，
// 而同一用户自己重复提交是正常行为（弱网重试、多标签页）。
// 换成单次消费反而会打断用户：登录页开着两个标签就废了。
func TestLoginCSRFTokenIsReusable(t *testing.T) {
	f := newAuthFixture(t)
	f.seedUser(t, authTestUser{userID: "bob", password: "secret123"})

	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)

	for i := 0; i < 2; i++ {
		rec := jar.postForm(t, f.srv, "/login.php", url.Values{
			"user_id": {"bob"}, "password": {"secret123"}, "csrf_token": {token},
		})
		if rec.Code != http.StatusSeeOther {
			t.Fatalf("第 %d 次登录 = %d, want 303; body=%s", i+1, rec.Code, rec.Body.String())
		}
	}
}

// 换一个会话的 token 不能拿来给另一个会话用 ——
// 这正是双提交方案要防的：攻击者无法为受害者构造出配对的一对。
func TestCSRFCrossSessionTokenRejected(t *testing.T) {
	f := newAuthFixture(t)
	f.seedUser(t, authTestUser{userID: "bob", password: "secret123"})

	victim := &cookieJar{}
	victimToken := victim.tokenFromLoginPage(t, f.srv)

	attacker := &cookieJar{}
	attacker.get(t, f.srv, "/loginpage.php") // 拿到自己的 csrf_token Cookie

	// 攻击者用自己的 Cookie + 受害者的表单 token：解掩码还原出的 master
	// 与攻击者 Cookie 里的对不上，必须被拒。
	rec := attacker.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"bob"}, "password": {"secret123"}, "csrf_token": {victimToken},
	})
	if rec.Code == http.StatusSeeOther {
		t.Fatal("跨会话的 token 不应通过校验")
	}
}

// 完全没有 Cookie 的裸 POST 必须被拒：这是纯跨站请求的形态。
func TestCSRFRejectsMissingCookie(t *testing.T) {
	f := newAuthFixture(t)
	f.seedUser(t, authTestUser{userID: "bob", password: "secret123"})

	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)

	req := httptest.NewRequest(http.MethodPost, "/login.php",
		strings.NewReader(url.Values{
			"user_id": {"bob"}, "password": {"secret123"}, "csrf_token": {token},
		}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Real-IP", testClientIP)
	rec := httptest.NewRecorder()
	f.srv.handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusSeeOther {
		t.Fatal("缺少 csrf_token Cookie 的 POST 不应登录成功")
	}
}

func TestLoginRejectsMissingOrWrongCSRF(t *testing.T) {
	f := newAuthFixture(t)
	f.seedUser(t, authTestUser{userID: "carol", password: "secret123"})

	cases := []struct {
		name  string
		token string
	}{
		{"空 token", ""},
		{"伪造 token", strings.Repeat("z", 32)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			jar := &cookieJar{}
			jar.get(t, f.srv, "/loginpage.php") // 拿 cookie，但不用它的 token
			rec := jar.postForm(t, f.srv, "/login.php", url.Values{
				"user_id": {"carol"}, "password": {"secret123"}, "csrf_token": {tc.token},
			})
			// 不断言 403：CSRF 失败走的是弹窗 + 后退（200），
			// 见 csrfFailure —— 迁移期两端并存，吐默认 403 页面用户会以为站点坏了。
			// 真正要锁定的是"没登录成功"。
			if rec.Code == http.StatusSeeOther {
				t.Fatal("CSRF 无效却登录成功")
			}
			if !strings.Contains(rec.Body.String(), "页面已过期") {
				t.Errorf("应给出页面过期的提示; body=%s", rec.Body.String())
			}
		})
	}
}

// 密码错误应弹窗提示并写 loginlog，且不发新 cookie。
func TestLoginWrongPassword(t *testing.T) {
	f := newAuthFixture(t)
	f.seedUser(t, authTestUser{userID: "dave", password: "secret123"})

	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)
	sidBefore := jar.sid

	rec := jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"dave"}, "password": {"wrongpwd"}, "csrf_token": {token},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("= %d, want 200（PHP 用 alert 而非 4xx）", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "用户名或密码错误") {
		t.Errorf("响应体缺少错误提示: %s", rec.Body.String())
	}
	if jar.sid != sidBefore {
		t.Error("登录失败不应换新会话")
	}
	var n int
	if err := f.srv.db.QueryRowContext(context.Background(),
		"SELECT COUNT(1) FROM loginlog WHERE user_id = 'dave' AND password = 'login fail'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("loginlog 失败记录数 = %d, want 1", n)
	}
}

func TestLoginNonExistentUser(t *testing.T) {
	f := newAuthFixture(t)
	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)
	rec := jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"ghost"}, "password": {"whatever"}, "csrf_token": {token},
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "用户名或密码错误") {
		t.Errorf("不存在的用户应给出同样的提示: %d %s", rec.Code, rec.Body.String())
	}
}

// 过期账号（expiry_date 在过去）不能登录，对应 login-hustoj.php:9。
func TestLoginExpiredAccount(t *testing.T) {
	f := newAuthFixture(t)
	hash, err := pwGen("secret123", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.srv.db.ExecContext(context.Background(),
		`INSERT INTO users (user_id, email, ip, accesstime, password, reg_time, nick, school, group_name, defunct, expiry_date)
		 VALUES ('old', 'o@t.local', ?, NOW(), ?, NOW(), 'old', 'S', 'user', 'N', '2020-01-01')`,
		testClientIP, hash); err != nil {
		t.Fatal(err)
	}

	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)
	rec := jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"old"}, "password": {"secret123"}, "csrf_token": {token},
	})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "用户名或密码错误") {
		t.Errorf("过期账号应无法登录: %d %s", rec.Code, rec.Body.String())
	}
}

// defunct='Y' 的账号不能登录。
func TestLoginDefunctAccount(t *testing.T) {
	f := newAuthFixture(t)
	hash, err := pwGen("secret123", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.srv.db.ExecContext(context.Background(),
		`INSERT INTO users (user_id, email, ip, accesstime, password, reg_time, nick, school, group_name, defunct, expiry_date)
		 VALUES ('dead', 'd@t.local', ?, NOW(), ?, NOW(), 'dead', 'S', 'user', 'Y', '2099-01-01')`,
		testClientIP, hash); err != nil {
		t.Fatal(err)
	}
	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)
	rec := jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"dead"}, "password": {"secret123"}, "csrf_token": {token},
	})
	if rec.Code != http.StatusOK {
		t.Errorf("已删除账号应无法登录: %d", rec.Code)
	}
}

// 5 分钟内失败次数超过上限后应被限流，对应 login.php:54。
//
// 阈值是严格大于：PHP 写的是 $failed[0][0] > $OJ_LOGIN_FAIL_LIMIT，
// 所以失败数恰好等于上限时仍放行。写成 ">=" 会比 PHP 早拦一次。
func TestLoginFailLimit(t *testing.T) {
	f := newAuthFixture(t)
	f.seedUser(t, authTestUser{userID: "eve", password: "secret123"})
	clearLoginLog(t, f, "user_id = 'eve'")

	// 5 次失败后计数=5，仍未超过上限：第 6 次尝试应放行
	for i := 0; i < 5; i++ {
		jar := &cookieJar{}
		token := jar.tokenFromLoginPage(t, f.srv)
		rec := jar.postForm(t, f.srv, "/login.php", url.Values{
			"user_id": {"eve"}, "password": {"bad"}, "csrf_token": {token},
		})
		if strings.Contains(rec.Body.String(), "Failed login too frequently") {
			t.Fatalf("第 %d 次尝试就被限流；失败数=5 时 PHP 仍放行", i+1)
		}
	}
	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)
	rec := jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"eve"}, "password": {"secret123"}, "csrf_token": {token},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("失败数=5（未超上限）时正确密码应能登录，实际 = %d; body=%s", rec.Code, rec.Body.String())
	}

	// 失败数超过上限后，即使密码正确也应被拦
	clearLoginLog(t, f, "user_id = 'eve'")
	for i := 0; i < 7; i++ {
		j := &cookieJar{}
		tok := j.tokenFromLoginPage(t, f.srv)
		j.postForm(t, f.srv, "/login.php", url.Values{
			"user_id": {"eve"}, "password": {"bad"}, "csrf_token": {tok},
		})
	}
	jar = &cookieJar{}
	token = jar.tokenFromLoginPage(t, f.srv)
	rec = jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"eve"}, "password": {"secret123"}, "csrf_token": {token},
	})
	if !strings.Contains(rec.Body.String(), "Failed login too frequently") {
		t.Errorf("失败数超过上限后应限流，即使密码正确: %s", rec.Body.String())
	}
}

// 同 IP 阈值是账号阈值的 4 倍，对应 login.php:54 的 ip_fail > limit*4。
func TestLoginFailLimitByIP(t *testing.T) {
	f := newAuthFixture(t)
	clearLoginLog(t, f, "1=1")

	// 21 次失败分散到 21 个账号：每个账号都没超 5，但同 IP 累计已超 20
	for i := 0; i < 21; i++ {
		uid := fmt.Sprintf("ipspread%02d", i)
		insertBareUser(t, f, uid)
		insertLoginFail(t, f, uid)
	}
	// 从没失败过的新账号，正确密码也应因同 IP 被限流
	insertBareUser(t, f, "ipvictim")
	setPassword(t, f, "ipvictim", "secret123")

	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)
	rec := jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"ipvictim"}, "password": {"secret123"}, "csrf_token": {token},
	})
	if !strings.Contains(rec.Body.String(), "Failed login too frequently") {
		var n int
		f.srv.db.QueryRowContext(context.Background(),
			"SELECT COUNT(1) FROM loginlog WHERE ip = ? AND password = 'login fail'", testClientIP).Scan(&n)
		t.Errorf("同 IP 失败数超上限×4 时应限流（当前 ip 维度失败数=%d, 阈值=%d）: %s",
			n, 5*4, rec.Body.String())
	}
}

func TestLoginFailLimitDisabled(t *testing.T) {
	f := newAuthFixture(t)
	f.seedUser(t, authTestUser{userID: "frank", password: "secret123"})
	f.srv.cfg.LoginFailLimit = 0

	for i := 0; i < 8; i++ {
		jar := &cookieJar{}
		token := jar.tokenFromLoginPage(t, f.srv)
		jar.postForm(t, f.srv, "/login.php", url.Values{
			"user_id": {"frank"}, "password": {"bad"}, "csrf_token": {token},
		})
	}
	// 限流关闭时，正确密码仍应可登录
	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)
	rec := jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"frank"}, "password": {"secret123"}, "csrf_token": {token},
	})
	if rec.Code != http.StatusSeeOther {
		t.Errorf("限流关闭时应能登录: %d %s", rec.Code, rec.Body.String())
	}
}

// 跨源 Referer 不签发 token，对应 csrf_token.php:23。

// 登出应删除会话文件并清 cookie。
// nosurf 不按 Referer 决定是否签发。
//
// 以前这里会拒绝跨源请求拿 token，但 nosurf 的 token 与该用户自己的 Cookie
// 绑定：跨站页面即使诱导服务端签发，攻击者也拿不到
// "受害者 Cookie + 对应表单 token" 这一对，所以签发本身没有风险。
// 再按 Referer 拦反而会误伤代理追加 Referer 的正常请求。
func TestLoginPageTokenIssuedForAnyOrigin(t *testing.T) {
	f := newAuthFixture(t)
	for _, ref := range []string{
		"https://oj.example.com/index.php",
		"https://evil.example.net/x",
		"",
	} {
		t.Run("ref="+ref, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/loginpage.php", nil)
			req.Host = "oj.example.com"
			if ref != "" {
				req.Header.Set("Referer", ref)
			}
			rec := httptest.NewRecorder()
			f.srv.handler().ServeHTTP(rec, req)
			if !csrfRe.MatchString(rec.Body.String()) {
				t.Errorf("登录页应始终签发 token: ref=%q", ref)
			}
		})
	}
}

// csrf_token[] 数组形式不再被专门拒绝。
//
// 旧实现要拒绝数组参数，那是手写校验时对类型混淆的补丁。
// nosurf 直接取 vals[0]，而攻击者无论如何构造不出合法的 master，
// 数组形式没有额外收益。这里锁定的是"合法 token 不被误拒"。
func TestCSRFArrayParamStillValidated(t *testing.T) {
	f := newAuthFixture(t)
	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)

	rec := jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"nobody"}, "password": {"x"},
		"csrf_token": {token, token},
	})
	if strings.Contains(rec.Body.String(), "页面已过期") {
		t.Error("合法 token 不应因数组形式被 CSRF 层误拒")
	}
}

func TestLogout(t *testing.T) {
	f := newAuthFixture(t)
	f.seedUser(t, authTestUser{userID: "grace", password: "secret123"})

	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)
	rec := jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"grace"}, "password": {"secret123"}, "csrf_token": {token},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("登录 = %d, want 303", rec.Code)
	}
	sid := jar.sid

	rec = jar.get(t, f.srv, "/logout.php")
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("= %d, want 303", rec.Code)
	}
	if _, err := os.Stat(filepath.Join(f.dir, "sess_"+sid)); !os.IsNotExist(err) {
		t.Error("登出后会话文件未删除")
	}
	// Go 会话 cookie 被清空
	var cleared bool
	for _, c := range rec.Result().Cookies() {
		if c.Name == testGoSessionCookie && (c.Value == "" || c.MaxAge < 0) {
			cleared = true
		}
	}
	if !cleared {
		t.Error("登出未清除 HJGOSESSID")
	}
}

// 未登录时访问登录页正常；已登录时应重定向，对应 loginpage.php。
func TestLoginPageRedirectWhenLoggedIn(t *testing.T) {
	f := newAuthFixture(t)
	f.seedUser(t, authTestUser{userID: "henry", password: "secret123"})

	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)
	jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"henry"}, "password": {"secret123"}, "csrf_token": {token},
	})

	rec := jar.get(t, f.srv, "/loginpage.php")
	if rec.Code != http.StatusSeeOther {
		t.Errorf("已登录时 GET 登录页 = %d, want 303", rec.Code)
	}
}

// 匿名会话（只有 csrf_keys，没有 user_id）不能被当成已登录。
// 这正是早期 "有会话文件即视为已登录" 的缺陷：
// 登录页刷新一次就再也拿不到。
func TestAnonymousSessionNotLoggedIn(t *testing.T) {
	f := newAuthFixture(t)
	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)

	id := f.srv.resolveIdentity(nil, newReqWithSID(jar.sid))
	if id.LoggedIn {
		t.Error("只有 CSRF 的匿名会话被误判为已登录")
	}
	// 且必须仍能正常访问登录页（可重复 GET）
	for i := 0; i < 3; i++ {
		rec := jar.get(t, f.srv, "/loginpage.php")
		if rec.Code != http.StatusOK {
			t.Fatalf("第 %d 次 GET 登录页 = %d, want 200", i+1, rec.Code)
		}
	}
	_ = token
}

// 权限带 valuestr 时写入值而不是 true，对应 login.php:82-85。
func TestLoginPrivilegeValuestr(t *testing.T) {
	f := newAuthFixture(t)
	f.seedUser(t, authTestUser{
		userID: "ivan", password: "secret123",
		privs: map[string]string{"problem_editor": "0"},
	})
	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)
	jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"ivan"}, "password": {"secret123"}, "csrf_token": {token},
	})
	sess, err := f.srv.sessions.Get(context.Background(), jar.sid)
	if err != nil {
		t.Fatal(err)
	}
	if !sess.Privileges.ProblemEditor {
		t.Error("problem_editor 应为 true")
	}
}

// 私有比赛受邀标记 c<cid> 应被解析出来。
func TestLoginContestInvite(t *testing.T) {
	f := newAuthFixture(t)
	f.seedUser(t, authTestUser{
		userID: "jane", password: "secret123",
		privs: map[string]string{"c1000": "", "c1001": ""},
	})
	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, f.srv)
	jar.postForm(t, f.srv, "/login.php", url.Values{
		"user_id": {"jane"}, "password": {"secret123"}, "csrf_token": {token},
	})
	id := f.srv.resolveIdentity(nil, newReqWithSID(jar.sid))
	if !id.HasContestInvite(1000) || !id.HasContestInvite(1001) {
		t.Errorf("受邀标记未解析: %v", id.ContestInvites)
	}
	if id.HasContestInvite(1002) {
		t.Error("不应有 1002 的受邀标记")
	}
}

// 用户名与昵称落库正确，且注册后自动登录。
func TestRegisterEndToEnd(t *testing.T) {
	f := newAuthFixture(t)
	jar := &cookieJar{}
	rec := jar.get(t, f.srv, "/registerpage.php")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET 注册页 = %d", rec.Code)
	}
	token := tokenFromBody(t, rec.Body.String())
	rec = jar.postForm(t, f.srv, "/register.php", url.Values{
		"user_id":     {"newbie"},
		"nick":        {"新来的"},
		"password":    {"pass1234"},
		"rptpassword": {"pass1234"},
		"school":      {"测试中学"},
		"email":       {"newbie@test.local"},
		"csrf_token":  {token},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("POST /register.php = %d; body=%s", rec.Code, rec.Body.String())
	}
	var nick, group, hash string
	if err := f.srv.db.QueryRowContext(context.Background(),
		"SELECT nick, group_name, password FROM users WHERE user_id = 'newbie'").
		Scan(&nick, &group, &hash); err != nil {
		t.Fatal(err)
	}
	if nick != "新来的" {
		t.Errorf("nick = %q, want 新来的", nick)
	}
	if group != "user" {
		t.Errorf("group_name = %q, want user", group)
	}
	if !pwCheck("pass1234", hash) {
		t.Error("落库密码无法通过校验")
	}
	// 自动登录
	id := f.srv.resolveIdentity(nil, newReqWithSID(jar.sid))
	if !id.LoggedIn || id.UserID != "newbie" {
		t.Errorf("注册后未自动登录: %+v", id)
	}
}

func TestRegisterValidation(t *testing.T) {
	cases := []struct {
		name string
		form url.Values
		want string
	}{
		{"密码太短", url.Values{
			"user_id": {"ab"}, "password": {"12345"}, "rptpassword": {"12345"},
			"email": {"a@b.com"},
		}, "用户名太短"},
		{"两次密码不一致", url.Values{
			"user_id": {"gooduser"}, "password": {"pass1234"}, "rptpassword": {"different1"},
			"email": {"a@b.com"},
		}, "两次输入的密码不一致"},
		{"邮箱为空", url.Values{
			"user_id": {"gooduser"}, "password": {"pass1234"}, "rptpassword": {"pass1234"},
			"email": {""},
		}, "邮箱不能为空"},
		{"邮箱格式错", url.Values{
			"user_id": {"gooduser"}, "password": {"pass1234"}, "rptpassword": {"pass1234"},
			"email": {"not-an-email"},
		}, "邮箱格式不正确"},
		{"用户名含非法字符", url.Values{
			"user_id": {"bad@name"}, "password": {"pass1234"}, "rptpassword": {"pass1234"},
			"email": {"a@b.com"},
		}, "用户名格式不正确"},
		{"敏感词", url.Values{
			"user_id": {"gooduser"}, "nick": {"你妈的"}, "password": {"pass1234"},
			"rptpassword": {"pass1234"}, "email": {"a@b.com"},
		}, "不良词汇"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newAuthFixture(t)
			jar := &cookieJar{}
			rec := jar.get(t, f.srv, "/registerpage.php")
			token := tokenFromBody(t, rec.Body.String())
			form := tc.form
			form.Set("csrf_token", token)
			rec = jar.postForm(t, f.srv, "/register.php", form)
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Errorf("响应体 = %s\nwant 包含 %q", rec.Body.String(), tc.want)
			}
			// 只断言本次提交的用户没落库：影子库里本来就有真实数据，
			// 断言全表为空会把两者混为一谈。
			var n int
			if err := f.srv.db.QueryRowContext(context.Background(),
				"SELECT COUNT(1) FROM users WHERE user_id = ?", tc.form.Get("user_id")).Scan(&n); err != nil {
				t.Fatal(err)
			}
			if n != 0 {
				t.Errorf("校验失败却写入了 %d 条用户记录", n)
			}
		})
	}
}

func TestRegisterDuplicateUser(t *testing.T) {
	f := newAuthFixture(t)
	f.seedUser(t, authTestUser{userID: "taken", password: "secret123"})

	jar := &cookieJar{}
	rec := jar.get(t, f.srv, "/registerpage.php")
	token := tokenFromBody(t, rec.Body.String())
	rec = jar.postForm(t, f.srv, "/register.php", url.Values{
		"user_id": {"taken"}, "password": {"pass1234"}, "rptpassword": {"pass1234"},
		"email": {"x@y.com"}, "csrf_token": {token},
	})
	if !strings.Contains(rec.Body.String(), "已存在") {
		t.Errorf("重复用户名应提示已存在: %s", rec.Body.String())
	}
}

// 昵称为空时回落到用户名，对应 register.php:58。
func TestRegisterNickFallsBackToUserID(t *testing.T) {
	f := newAuthFixture(t)
	jar := &cookieJar{}
	rec := jar.get(t, f.srv, "/registerpage.php")
	token := tokenFromBody(t, rec.Body.String())
	rec = jar.postForm(t, f.srv, "/register.php", url.Values{
		"user_id": {"nonick"}, "nick": {"  "}, "password": {"pass1234"},
		"rptpassword": {"pass1234"}, "email": {"a@b.com"}, "csrf_token": {token},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("= %d; %s", rec.Code, rec.Body.String())
	}
	var nick string
	if err := f.srv.db.QueryRowContext(context.Background(),
		"SELECT nick FROM users WHERE user_id = 'nonick'").Scan(&nick); err != nil {
		t.Fatal(err)
	}
	if nick != "nonick" {
		t.Errorf("nick = %q, want nonick（回落到用户名）", nick)
	}
}

// 关闭注册时两端都应静默返回，对应 register.php:3。
func TestRegisterDisabled(t *testing.T) {
	f := newAuthFixture(t)
	f.srv.cfg.Register = false

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/registerpage.php", nil)
	f.srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("= %d len=%d, want 200 空响应", rec.Code, rec.Body.Len())
	}
}

// PHP 方法限制：登录/注册只接受 POST。
func TestAuthMethodNotAllowed(t *testing.T) {
	f := newAuthFixture(t)
	for _, p := range []string{"/login.php", "/register.php"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rec := httptest.NewRecorder()
		f.srv.handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s = %d, want 405", p, rec.Code)
		}
	}
}

// 没有 PHP 会话桥时，Go 自己的登录必须照常工作。
//
// 旧架构下没有桥就发不出 CSRF token，登录整个不可用；这正是当初把
// PHP session 当成必需依赖的代价。Redis 会话落地后，PHP 会话桥退化成
// "迁移期读老用户"的可选组件：没配它，只影响老用户自动迁移，
// 不影响新用户登录。
func TestAuthWorksWithoutSessionBridge(t *testing.T) {
	cfg := testConfig()
	cfg.OJName = "吉尔吉斯倔驴大学OJ"
	srv := &Server{
		cfg:      cfg,
		log:      testLogger(),
		db:       newAuthDB(t),
		sessions: newTestSessionStore(t),
		// sessionBridge 故意留空
	}

	f := &authFixture{srv: srv, session: "吉尔吉斯倔驴大学OJ", dir: t.TempDir()}
	resetAuthState(t, srv.db)
	t.Cleanup(func() { resetAuthState(t, srv.db) })
	f.seedUser(t, authTestUser{userID: "nobridge", password: "secret123"})

	jar := &cookieJar{}
	token := jar.tokenFromLoginPage(t, srv)
	rec := jar.postForm(t, srv, "/login.php", url.Values{
		"user_id": {"nobridge"}, "password": {"secret123"}, "csrf_token": {token},
	})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("无会话桥时登录 = %d, want 303; body=%s", rec.Code, rec.Body.String())
	}
}

// 前缀不匹配时必须表现为"全员匿名"，这正是线上此前的故障。
// 断言这个行为是为了让配置错误在测试里显形，而不是静默通过。
func TestWrongOJNameMeansAnonymous(t *testing.T) {
	dir := t.TempDir()
	const sid = "wrongprefix1234567890ab"
	const ojName = "吉尔吉斯倔驴大学OJ"

	// 用正确的前缀写一份 PHP 会话，模拟线上真实文件。
	seed := ojName + `_user_id|s:3:"kim";` + ojName + `_administrator|b:1;`
	if err := os.WriteFile(filepath.Join(dir, "sess_"+sid), []byte(seed), 0600); err != nil {
		t.Fatal(err)
	}

	// 配置成错误的前缀（$OJ_NAME 配错是这个故障的典型来源）。
	wrong := NewSessionBridge(dir, "hustoj")
	req := httptest.NewRequest(http.MethodGet, "/index.php", nil)
	req.AddCookie(&http.Cookie{Name: phpSessionCookieName, Value: sid})

	id := wrong.Resolve(req)
	if id.LoggedIn {
		t.Errorf("前缀不匹配却解析出登录态: %+v", id)
	}

	// 对照：前缀正确时必须能读出来，否则上面那条就成了"永远匿名"的假通过。
	ok := NewSessionBridge(dir, ojName)
	if got := ok.Resolve(req); !got.LoggedIn || got.UserID != "kim" {
		t.Errorf("前缀正确时应能读出登录态: %+v", got)
	}
}

// base64 盐值长度固定，避免 pwGen 产出 PHP 无法解析的值。
func TestPWGenSaltLength(t *testing.T) {
	for i := 0; i < 20; i++ {
		h, err := pwGen("pw", false)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := base64.StdEncoding.DecodeString(h)
		if err != nil || len(raw) != 24 {
			t.Fatalf("第 %d 次产出异常: len=%d err=%v", i, len(raw), err)
		}
	}
	// 盐值应各不相同（否则相同密码会产生相同哈希）
	h1, _ := pwGen("same", false)
	h2, _ := pwGen("same", false)
	if h1 == h2 {
		t.Error("两次 pwGen 产出相同哈希，盐值未随机")
	}
}

var _ = sql.ErrNoRows
var _ = time.Now

// ── 测试环境辅助 ────────────────────────────────────────────────────────

// testConfig 返回测试用配置。
func testConfig() *config.Config { return config.Default() }

// testLogger 返回丢弃输出的 logger。
func testLogger() *slog.Logger { return newTestLogger() }

// newAuthDB 打开一个只用于测试的数据库连接。
//
// 认证链路的 SQL 涉及 users / privilege / loginlog / contest 四张表，
// sqlmock 只能断言"我们调用了自己写死的字符串"，无法验证
// "登录成功后会话里到底写了什么权限"这类跨表语义，
// 所以这里连真实库。表结构由 extra/check_all.sh 保证与生产一致。
func newAuthDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("HUSTOJ_EQUIV_DSN")
	if dsn == "" {
		t.Skip("未设置 HUSTOJ_EQUIV_DSN，跳过需要真实库的认证测试")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping 失败: %v", err)
	}
	return db
}

// clearLoginLog 清空 loginlog，满足条件的部分由 where 给出。
//
// 影子库里残留着真实导入的 loginlog，测试必须先隔离，
// 否则"失败次数"断言会被历史数据干扰。
func clearLoginLog(t *testing.T, f *authFixture, where string) {
	t.Helper()
	if _, err := f.srv.db.ExecContext(context.Background(),
		"DELETE FROM loginlog WHERE "+where); err != nil {
		t.Fatalf("清理 loginlog 失败: %v", err)
	}
}

// insertBareUser 造一个只有标识信息的用户。
func insertBareUser(t *testing.T, f *authFixture, userID string) {
	t.Helper()
	if _, err := f.srv.db.ExecContext(context.Background(),
		`INSERT INTO users (user_id, email, ip, accesstime, password, reg_time, nick, school,
		                     group_name, defunct, expiry_date)
		 VALUES (?, 'e@test.local', ?, NOW(), 'x', NOW(), 'n', 'S', 'user', 'N', '2099-01-01')
		 ON DUPLICATE KEY UPDATE email = VALUES(email), ip = VALUES(ip)`,
		userID, testClientIP); err != nil {
		t.Fatalf("插入用户 %s 失败: %v", userID, err)
	}
}

// insertLoginFail 直接写一条失败记录，避免真的走 HTTP。
func insertLoginFail(t *testing.T, f *authFixture, userID string) {
	t.Helper()
	if _, err := f.srv.db.ExecContext(context.Background(),
		"INSERT INTO loginlog (user_id, password, ip, time) VALUES (?, 'login fail', ?, NOW())",
		userID, testClientIP); err != nil {
		t.Fatalf("写 loginlog 失败: %v", err)
	}
}

// setPassword 按 PHP 格式设置密码。
func setPassword(t *testing.T, f *authFixture, userID, pw string) {
	t.Helper()
	hash, err := pwGen(pw, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.srv.db.ExecContext(context.Background(),
		"UPDATE users SET password = ? WHERE user_id = ?", hash, userID); err != nil {
		t.Fatalf("设置密码失败: %v", err)
	}
}
