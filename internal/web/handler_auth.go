// 认证：密码哈希、登录、注册、登出。
//
// 权威实现是 phpweb/login.php、register.php 与
// phpweb/include/login-hustoj.php，全部行为对齐这些文件。
//
// 路径与表单字段名必须与 PHP 完全一致（user_id / password /
// rptpassword / email / school / nick / csrf），因为迁移期两端会并存，
// 用户可能带着 PHP 页面跳到 Go 页面。

package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-playground/validator/v10"
)

// 认证相关的用户输入约束，对应 register.php:38-108。
const (
	userIDMaxLen = 48
	userIDMinLen = 3
	nickMaxLen   = 20
	schoolMaxLen = 20
	emailMaxLen  = 100
	passwordMin  = 6
)

// errLoginFailed 表示用户名或密码不匹配。
var errLoginFailed = errors.New("用户名或密码错误")

// errNotFound 表示用户不存在或已失效。
var errNotFound = errors.New("用户不存在")

// handleLoginPage 渲染登录页（GET /loginpage.php）。
func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	id := s.resolveIdentity(w, r)
	if id.LoggedIn {
		http.Redirect(w, r, "/index.php", http.StatusSeeOther)
		return
	}
	s.renderPage(w, r, http.StatusOK, "loginForm", "登录", s.formView(r))
}

// handleLogin 处理登录 POST /login.php。
//
// 对应 login.php。差异说明：
//   - 失败频次限制：PHP 查 loginlog 里 5 分钟内的 'login fail' 计数，
//     Go 同样依赖 loginlog，保证两端共享同一份限流状态。
//   - 考试模式 IP 绑定：PHP 在 $OJ_EXAM_CONTEST_ID > 0 时要求考试开始后
//     登录 IP 保持一致，Go 侧按同一条件实现。
//   - 登录成功后 PHP 调 session_regenerate_id()，Go 同样换新 SID，
//     否则旧的匿名会话 ID 仍可被复用（会话固定攻击）。
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := strings.TrimSpace(r.FormValue("user_id"))
	password := r.FormValue("password")
	ip := s.remoteIP(r)

	if userID == "" || password == "" {
		http.Error(w, "用户名和密码不能为空", http.StatusBadRequest)
		return
	}

	if s.cfg.VCode {
		sid := s.goSessionIDFromRequest(r)
		if !s.checkVCode(r.Context(), sid, vcodeFromRequest(r)) {
			if err := s.markVFail(r.Context(), sid); err != nil {
				s.log.Error("记录验证码失败状态出错", "err", err)
			}
			s.alertAndBack(w, "验证码错误！")
			return
		}
	}

	// 失败频次限制，对应 login.php:50-58。
	limited, err := s.loginTooFrequent(r.Context(), userID, ip)
	if err != nil {
		s.log.Error("查询登录失败次数出错", "err", err)
		http.Error(w, "登录失败", http.StatusInternalServerError)
		return
	}
	if limited {
		s.renderErrorPage(w, "Failed login too frequently!")
		return
	}

	loginUser, err := s.checkLogin(r.Context(), userID, password, ip)
	if err != nil && !errors.Is(err, errLoginFailed) && !errors.Is(err, errNotFound) {
		s.log.Error("登录查询失败", "err", err)
		http.Error(w, "登录失败", http.StatusInternalServerError)
		return
	}
	// 每次尝试都写 loginlog，无论成败，与 login.php:135 的无条件 INSERT 一致。
	s.recordLogin(r.Context(), userID, ip, err == nil)

	if err != nil {
		if errors.Is(err, errExamIPMismatch) {
			s.renderErrorPage(w, s.examIPMismatchMessage(r, userID))
			return
		}
		s.alertAndBack(w, "用户名或密码错误！")
		return
	}

	// 换新会话 ID，对应 PHP 的 session_regenerate_id()。
	//
	// 必须换新：登录前的匿名 SID 在浏览器里已存在，攻击者若能预先
	// 让受害者使用一个自己知道的 SID（会话固定），登录后就会共享它。
	sid, err := newGoSessionID()
	if err != nil {
		s.log.Error("生成 Go 会话 ID 失败", "err", err)
		http.Error(w, "登录失败", http.StatusInternalServerError)
		return
	}
	sess, err := s.buildLoginSession(r.Context(), loginUser)
	if err != nil {
		s.log.Error("组装登录会话失败", "err", err)
		http.Error(w, "登录失败", http.StatusInternalServerError)
		return
	}
	if err := s.sessions.Save(r.Context(), sid, sess); err != nil {
		s.log.Error("写入 Go 会话失败", "err", err)
		http.Error(w, "登录失败", http.StatusInternalServerError)
		return
	}
	// 旧会话立即失效：否则拿到旧 SID 的人仍保有那个匿名会话。
	if old := s.goSessionIDFromRequest(r); old != "" && old != sid {
		_ = s.sessions.Destroy(r.Context(), old)
	}
	s.setGoSessionCookie(w, sid)
	s.updateAccessTime(r.Context(), loginUser, ip)

	after := s.identityFromGo(sess, sid)
	http.Redirect(w, r, s.afterLoginRedirect(after), http.StatusSeeOther)
}

// errExamIPMismatch 表示考试模式下登录 IP 与考试开始后的首次登录 IP 不一致。
var errExamIPMismatch = errors.New("考试模式 IP 不一致")

// checkLogin 验证凭据，对应 login-hustoj.php:4 check_login()。
//
// SQL 与 PHP 逐字对应：defunct='N' 且 expiry_date >= curdate()
// （有效期当天仍可登录）。
func (s *Server) checkLogin(ctx context.Context, userID, password, ip string) (string, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT user_id, password FROM users WHERE user_id = ? AND defunct = 'N' AND expiry_date >= CURDATE()",
		userID)
	var dbUserID, dbPassword string
	if err := row.Scan(&dbUserID, &dbPassword); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", errNotFound
		}
		return "", err
	}
	if !pwCheck(password, dbPassword) {
		return "", errLoginFailed
	}

	// 考试模式：考试开始后首次登录的 IP 会被记住，之后必须一致。
	// 管理员豁免，便于管理员从任意机器救场。
	if s.cfg.ExamContestID > 0 {
		ok, err := s.examIPMatches(ctx, dbUserID, ip)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", errExamIPMismatch
		}
	}
	return dbUserID, nil
}

// examIPMatches 判断考试模式下的 IP 绑定是否满足。
//
// 对应 login-hustoj.php:22-37：
//  1. 取 start_time <= now() AND end_time >= now() AND contest_id >= $OJ_EXAM_CONTEST_ID
//     的比赛里最早的 start_time - 30 分钟
//  2. 查该用户在 loginlog 里该时间点之后最近一次 'login ok' 的 IP
//  3. 不一致且非管理员 -> 拒绝
func (s *Server) examIPMatches(ctx context.Context, userID, ip string) (bool, error) {
	var since time.Time
	err := s.db.QueryRowContext(ctx,
		`SELECT MIN(start_time - INTERVAL 30 MINUTE) FROM contest
		 WHERE start_time <= NOW() AND end_time >= NOW() AND contest_id >= ?`,
		s.cfg.ExamContestID).Scan(&since)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if since.IsZero() {
		// 没有进行中的考试：start_time 为 NULL，无从比较，放行。
		return true, nil
	}
	var lastIP sql.NullString
	err = s.db.QueryRowContext(ctx,
		`SELECT ip FROM loginlog WHERE user_id = ? AND time > ? AND password = 'login ok'
		 ORDER BY time DESC LIMIT 1`,
		userID, since).Scan(&lastIP)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if !lastIP.Valid || lastIP.String == "" {
		// 还没有考试内的登录记录：本次即为首次，PHP 同样放行。
		return true, nil
	}
	if lastIP.String == ip {
		return true, nil
	}
	var isAdmin bool
	if err := s.db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM privilege WHERE user_id = ? AND rightstr = 'administrator')",
		userID).Scan(&isAdmin); err != nil {
		return false, err
	}
	return isAdmin, nil
}

// examIPMismatchMessage 生成考试 IP 不匹配的提示，对应 login-hustoj.php:34。
func (s *Server) examIPMismatchMessage(r *http.Request, userID string) string {
	var lastIP sql.NullString
	since := s.examStartThreshold(r)
	if err := s.db.QueryRowContext(r.Context(),
		`SELECT ip FROM loginlog WHERE user_id = ? AND time > ? AND password = 'login ok'
		 ORDER BY time DESC LIMIT 1`, userID, since).Scan(&lastIP); err != nil {
		return "考试期间不允许更换 IP 登录！"
	}
	return fmt.Sprintf("考试期间不允许更换 IP 登录！（上次 %s / 本次 %s）", lastIP.String, s.remoteIP(r))
}

// examStartThreshold 取考试 IP 绑定的时间阈值。
func (s *Server) examStartThreshold(r *http.Request) time.Time {
	var since time.Time
	if err := s.db.QueryRowContext(r.Context(),
		`SELECT MIN(start_time - INTERVAL 30 MINUTE) FROM contest
		 WHERE start_time <= NOW() AND end_time >= NOW() AND contest_id >= ?`,
		s.cfg.ExamContestID).Scan(&since); err != nil {
		return time.Time{}
	}
	return since
}

// loginTooFrequent 检查 5 分钟内的失败次数，对应 login.php:50-58。
func (s *Server) loginTooFrequent(ctx context.Context, userID, ip string) (bool, error) {
	limit := s.cfg.LoginFailLimit
	if limit <= 0 {
		return false, nil
	}
	since := time.Now().Add(-5 * time.Minute)
	var userFail, ipFail int
	err := s.db.QueryRowContext(ctx,
		`SELECT (SELECT COUNT(1) FROM loginlog WHERE user_id = ? AND password = 'login fail' AND time >= ?),
		        (SELECT COUNT(1) FROM loginlog WHERE ip = ? AND password = 'login fail' AND time >= ?)`,
		userID, since, ip, since).Scan(&userFail, &ipFail)
	if err != nil {
		return false, err
	}
	return userFail > limit || ipFail > limit*4, nil
}

// recordLogin 写 loginlog，对应 login.php:135。
func (s *Server) recordLogin(ctx context.Context, userID, ip string, ok bool) {
	result := "login fail"
	if ok {
		result = "login ok"
	}
	if _, err := s.db.ExecContext(ctx,
		"INSERT INTO loginlog (user_id, password, ip, time) VALUES (?, ?, ?, NOW())",
		userID, result, ip); err != nil {
		// 审计写入失败不应阻断登录响应。
		s.log.Warn("写 loginlog 失败", "err", err, "user_id", userID)
	}
}

// updateAccessTime 更新登录时间与 IP，对应 login-hustoj.php:39。
func (s *Server) updateAccessTime(ctx context.Context, userID, ip string) {
	if _, err := s.db.ExecContext(ctx,
		"UPDATE users SET accesstime = NOW(), ip = ? WHERE user_id = ?", ip, userID); err != nil {
		s.log.Warn("更新 accesstime 失败", "err", err, "user_id", userID)
	}
}

// buildLoginSession 组装登录态，对应 login.php:61-93。
//
// 返回值是待写入的 Go 会话，由调用方决定 SID 与写入时机 ——
// 换 SID 与写会话必须由调用方排在同一处，拆开容易漏掉"换新"这一步。
func (s *Server) buildLoginSession(ctx context.Context, userID string) (*GoSession, error) {
	var nick, groupName string
	row := s.db.QueryRowContext(ctx,
		"SELECT group_name, nick FROM users WHERE user_id = ?", userID)
	if err := row.Scan(&groupName, &nick); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	var rows []sessionPrivilege
	var err error
	if groupName == "" {
		// 无组：只取该用户自己的权限。
		rows, err = s.loadPrivileges(userID, "", false)
	} else {
		// 有组：额外并入同组用户的 c% 权限（比赛相关）。
		rows, err = s.loadPrivileges(userID, groupName, true)
	}
	if err != nil {
		return nil, err
	}

	sess := &GoSession{UserID: userID}
	if groupName != "" {
		sess.Nick = nick
		sess.GroupName = groupName
	}
	for _, p := range rows {
		switch p.rightstr {
		case "administrator":
			sess.Privileges.Admin = true
		case "contest_creator":
			sess.Privileges.ContestCreator = true
		case "problem_editor":
			sess.Privileges.ProblemEditor = true
		// PHP 侧键名拼写错误是 problem_verifiter（少一个 i），见 contest-check.php。
		case "problem_verifiter", "problem_verifier":
			sess.Privileges.ProblemVerifier = true
		case "source_browser":
			sess.Privileges.SourceBrowser = true
		case "vip":
			// VIP 用户自动获得所有标题含 [VIP] 的比赛权限，对应 login.php:87-93。
			vipRows, err := s.vipContestIDs()
			if err != nil {
				return nil, err
			}
			for _, cid := range vipRows {
				s.addContestID(sess, 'c', cid)
			}
		case "contest_creator_except":
		case "onlinejudge":
		default:
			// 比赛权限以 c<cid> 形式出现在 privilege 表里（'c' + 比赛号），
			// 比赛号不固定，所以只能按前缀认，不能穷举。
			if kind, cid, ok := parseContestPrivilege(p.rightstr); ok {
				s.addContestID(sess, kind, cid)
				continue
			}
			// 其余权限（学校/班级管理员等）暂不进 Go 会话：
			// 它们只影响 PHP 侧的管理页，Go 还没有对应的功能面。
			// 需要时在这里加字段，而不是塞进 Extra 假装兼容。
		}
	}
	return sess, nil
}

// parseContestPrivilege 识别比赛权限键名。
//
// 形式是 <字母><比赛号>：c 是普通参赛、r 是私有赛受邀。
// 比赛号不固定，无法穷举，所以只能按前缀解析。
// 与 phpweb/contest-check.php 的 isContestAdmin/校验方式对齐。
func parseContestPrivilege(rightstr string) (kind byte, cid int32, ok bool) {
	if len(rightstr) < 2 {
		return 0, 0, false
	}
	kind = rightstr[0]
	if kind != 'c' && kind != 'r' {
		return 0, 0, false
	}
	n, err := strconv.ParseInt(rightstr[1:], 10, 32)
	if err != nil || n <= 0 {
		return 0, 0, false
	}
	return kind, int32(n), true
}

// sessionPrivilege 是 privilege 表里的一行权限。
type sessionPrivilege struct {
	rightstr string
	valuestr sql.NullString
}

// loadPrivileges 读权限表。
func (s *Server) loadPrivileges(userID, groupName string, withGroup bool) ([]sessionPrivilege, error) {
	q := "SELECT rightstr, valuestr FROM privilege WHERE user_id = ?"
	args := []any{userID}
	if withGroup && groupName != "" {
		q = "SELECT rightstr, valuestr FROM privilege WHERE user_id = ? OR (user_id = ? AND rightstr LIKE 'c%')"
		args = append(args, groupName)
	}
	rows, err := s.db.QueryContext(context.Background(), q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []sessionPrivilege
	for rows.Next() {
		var p sessionPrivilege
		if err := rows.Scan(&p.rightstr, &p.valuestr); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// vipContestIDs 取标题含 [VIP] 的比赛 ID。
func (s *Server) vipContestIDs() ([]int32, error) {
	rows, err := s.db.QueryContext(context.Background(),
		"SELECT contest_id FROM contest WHERE title LIKE '%[VIP]%'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int32
	for rows.Next() {
		var cid int32
		if err := rows.Scan(&cid); err != nil {
			return nil, err
		}
		out = append(out, cid)
	}
	return out, rows.Err()
}

// afterLoginRedirect 决定登录后的去向，对应 login.php:111-119。
func (s *Server) afterLoginRedirect(id Identity) string {
	switch {
	case id.Admin:
		return "/admin/"
	case id.ContestCreator:
		return "/contest.php?my"
	case s.cfg.NeedLogin:
		return "/index.php"
	default:
		return "/index.php"
	}
}

// handleLogout 处理登出 GET /logout.php。
//
// 只销毁 Go 会话。PHP session 一律不动：它是 PHP 侧自己在用的，
// 登出后浏览器仍可能带着 PHPSESSID 访问 PHP，如果这里顺手删了它，
// 反而会让"回退到 PHP 的请求"莫名掉成匿名。
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sid := s.goSessionIDFromRequest(r); sid != "" {
		if err := s.sessions.Destroy(r.Context(), sid); err != nil && !errors.Is(err, errSessionNotFound) {
			s.log.Error("销毁 Go 会话失败", "err", err)
		}
	}
	s.clearGoSessionCookie(w)
	http.Redirect(w, r, "/index.php", http.StatusSeeOther)
}

// handleRegisterPage 渲染注册页（GET /registerpage.php）。
func (s *Server) handleRegisterPage(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Register {
		// 对应 register.php:3：关闭注册时直接输出空响应。
		w.WriteHeader(http.StatusOK)
		return
	}
	id := s.resolveIdentity(w, r)
	if id.LoggedIn {
		http.Redirect(w, r, "/index.php", http.StatusSeeOther)
		return
	}
	s.renderPage(w, r, http.StatusOK, "registerForm", "注册", s.formView(r))
}

// handleRegister 处理注册 POST /register.php。
//
// 对应 register.php。校验失败时把全部错误一次性回显
// （PHP 用 err_str 累加），而不是遇到第一个错就返回 —— 行为一致。
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Register {
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid form", http.StatusBadRequest)
		return
	}

	userID := strings.TrimSpace(r.FormValue("user_id"))
	email := strings.TrimSpace(r.FormValue("email"))
	school := strings.TrimSpace(r.FormValue("school"))
	nick := strings.TrimSpace(r.FormValue("nick"))
	password := r.FormValue("password")
	rptPassword := r.FormValue("rptpassword")

	var errs []string
	add := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }

	if s.cfg.VCode {
		sid := s.goSessionIDFromRequest(r)
		if !s.checkVCode(r.Context(), sid, vcodeFromRequest(r)) {
			if err := s.markVFail(r.Context(), sid); err != nil {
				s.log.Error("记录验证码失败状态出错", "err", err)
			}
			add("验证码错误！")
		}
	}
	// 非 hustoj 登录模块下 PHP 禁止注册。
	if s.cfg.LoginMod != "" && s.cfg.LoginMod != "hustoj" {
		add("系统当前登录方式不支持注册！")
	}
	if n := utf8.RuneCountInString(userID); n > userIDMaxLen {
		add("用户名太长！")
	} else if n < userIDMinLen {
		add("用户名太短！")
	}
	if !validUsername(userID) {
		add("用户名格式不正确！")
	}
	if n := utf8.RuneCountInString(nick); n > nickMaxLen {
		add("昵称太长！")
	} else if n == 0 {
		nick = userID
	}
	if hasBadWord(userID) {
		add("用户名包含不良词汇！")
	}
	if hasBadWord(school) {
		add("学校包含不良词汇！")
	}
	if hasBadWord(nick) {
		add("昵称包含不良词汇！")
	}
	if password != rptPassword {
		add("两次输入的密码不一致！")
	}
	if len(password) < passwordMin {
		add("密码长度至少 6 位！")
	}
	if n := utf8.RuneCountInString(school); n > schoolMaxLen {
		add("学校名称太长！")
	}
	if email == "" {
		add("邮箱不能为空！")
	} else if !validEmail(email) {
		add("邮箱格式不正确！")
	}
	if n := utf8.RuneCountInString(email); n > emailMaxLen {
		add("邮箱太长！")
	}
	if len(errs) > 0 {
		s.alertAndBack(w, strings.Join(errs, "\n"))
		return
	}

	hash, err := pwGen(password, false)
	if err != nil {
		s.log.Error("生成密码哈希失败", "err", err)
		http.Error(w, "注册失败", http.StatusInternalServerError)
		return
	}

	var exists int
	err = s.db.QueryRowContext(r.Context(),
		"SELECT 1 FROM users WHERE user_id = ?", userID).Scan(&exists)
	switch {
	case err == nil:
		s.alertAndBack(w, "用户名已存在！")
		return
	case !errors.Is(err, sql.ErrNoRows):
		s.log.Error("检查用户名失败", "err", err)
		http.Error(w, "注册失败", http.StatusInternalServerError)
		return
	}
	// 注册页禁止用 $OJ_NAME 本身作为用户名，对应 register.php:136。
	if s.cfg.OJName != "" && userID == s.cfg.OJName {
		s.alertAndBack(w, "用户名已存在！")
		return
	}

	now := time.Now()
	ip := s.remoteIP(r)
	if _, err := s.db.ExecContext(r.Context(),
		`INSERT INTO users (user_id, email, ip, accesstime, password, reg_time, nick, school, group_name, defunct)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'N')`,
		userID, email, ip, now, hash, now, nick, school, "user"); err != nil {
		s.log.Error("插入用户失败", "err", err)
		http.Error(w, "注册失败", http.StatusInternalServerError)
		return
	}

	// 注册后自动登录，对应 register.php 尾部的 session 写入。
	sid, err := newGoSessionID()
	if err != nil {
		s.log.Error("生成 Go 会话 ID 失败", "err", err)
		s.alertAndBack(w, "注册成功，但自动登录失败，请手动登录。")
		return
	}
	sess, err := s.buildLoginSession(r.Context(), userID)
	if err != nil {
		s.log.Error("组装注册后会话失败", "err", err)
		s.alertAndBack(w, "注册成功，但自动登录失败，请手动登录。")
		return
	}
	if err := s.sessions.Save(r.Context(), sid, sess); err != nil {
		s.log.Error("写入 Go 会话失败", "err", err)
		s.alertAndBack(w, "注册成功，但自动登录失败，请手动登录。")
		return
	}
	if old := s.goSessionIDFromRequest(r); old != "" && old != sid {
		_ = s.sessions.Destroy(r.Context(), old)
	}
	s.setGoSessionCookie(w, sid)
	http.Redirect(w, r, "/index.php", http.StatusSeeOther)
}

// validUsername 对应 my_func.inc.php:589 is_valid_user_name()。
//
// 允许 a-z A-Z 0-9 - _，另允许首位是 '*'。
// 注意长度约束由调用方单独检查（PHP 也是分开做的）。
// usernamePattern 是用户名的合法形态：首字符可以是字母数字、- _ 或通配 *，
// 其余字符只能是字母数字、- _。
//
// 首位允许 * 是 PHP 侧的历史行为（配合 SQL 通配查询），去掉会让已注册的
// 通配账号无法登录，所以必须保留。
//
// 用包级预编译的正则：validator 没有内置 regexp tag，每次请求重编译正则
// 是纯粹的浪费，而自己手写逐字节循环又容易漏字符。
var usernamePattern = regexp.MustCompile(`^(?:[A-Za-z0-9_-]+|\*[A-Za-z0-9_-]*)$`)

func validUsername(s string) bool {
	return s != "" && usernamePattern.MatchString(s)
}

// formValidator 集中放注册/登录表单的校验规则。
//
// 用 go-playground/validator 而不是手写 if：它自带 email、长度、字符集、
// 正则等一整套常用 tag，并且会复用编译好的校验计划，
// 比每次请求跑一遍手写判断更省。
var formValidator = func() *validator.Validate {
	// 不能开 WithRequiredStructEnabled：那会让 Var() 对非结构体输入直接 panic，
	// 而这里校验的正是标量（邮箱、用户名）。
	v := validator.New()
	// 报错信息统一用 tag 名，交给上层翻译，避免把英文 tag 泄给用户。
	v.RegisterTagNameFunc(func(f reflect.StructField) string { return f.Tag.Get("json") })
	return v
}()

// validEmail 校验邮箱格式。
//
// 规则全部交给 validator 的 email tag，不自己写解析器。
// 注意 PHP 的 FILTER_VALIDATE_EMAIL 比它严格（会拒 "a@b" 等），
// 差异可以接受：多接受个别格式古怪的地址，代价远低于自维护解析器。
func validEmail(s string) bool {
	return formValidator.Var(s, "required,email,max=254") == nil
}

// badWords 对应 phpweb/include/const.inc.php:8 的 $bad_words。
//
// 匹配语义与 PHP 的 stristr 一致：ASCII 大小写不敏感的子串查找。
// 因此 "jb" 会命中词条 "JB"，而 "xx" 不会命中 "XX狗"（词条里带"狗"）。
var badWords = []string{
	"装逼", "草泥马", "特么的", "撕逼", "玛拉戈壁", "爆菊", "JB", "呆逼",
	"本屌", "齐B短裙", "法克鱿", "丢你老母", "达菲鸡", "装13", "逼格",
	"蛋疼", "傻逼", "绿茶婊", "你妈的", "表砸", "屌爆了", "买了个婊",
	"已撸", "吉跋猫", "妈蛋", "逗比", "我靠", "碧莲", "碧池", "然并卵",
	"日了狗", "吃翔", "XX狗", "淫家", "你妹", "浮尸国", "滚粗",
}

// hasBadWord 报告 s 是否包含敏感词。
func hasBadWord(s string) bool {
	lower := asciiLower(s)
	for _, w := range badWords {
		if strings.Contains(lower, asciiLower(w)) {
			return true
		}
	}
	return false
}

// asciiLower 只对 ASCII 字母转小写，非 ASCII 字节原样保留。
func asciiLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// registerAuthRoutes 挂载认证路由。
func (s *Server) registerAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /loginpage.php", s.handleLoginPage)
	mux.HandleFunc("POST /login.php", s.handleLogin)
	mux.HandleFunc("GET /registerpage.php", s.handleRegisterPage)
	mux.HandleFunc("POST /register.php", s.handleRegister)
	mux.HandleFunc("GET /logout.php", s.handleLogout)
	// 验证码与登录必须一起切：答案存在会话里，两边分开上线必然对不上。
	mux.HandleFunc("GET /vcode.php", s.handleVCode)
}
