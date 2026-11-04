// Go 会话与身份的装配。
//
// 与 PHP 的对应关系（迁移期仍然需要，但方向反过来了）：
//   - PHP 的 PHPSESSID   → 只读，仅用于识别切流前已登录的老用户
//   - Go 的 HJGOSESSID   → 登录态与 CSRF 的唯一真相来源（Redis）
//
// 身份解析顺序见 resolveIdentity：先看 Go 会话，没有才回退到 PHP 会话，
// Go 会话不存在时即按匿名处理，不再回退到 PHP 会话。

package web

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
)

// newGoSessionID 生成一枚 Go 会话 ID。
//
// 用 32 个十六进制字符，字符集落在 [A-Za-z0-9] 内，
// 与 phpSessionIDSafe 的白名单一致，因此可以共用同一套校验。
func newGoSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败属于系统级故障。返回一个固定串会让所有人
		// 共用同一个会话，比返回错误更糟，所以这里必须向上报错。
		return "", fmt.Errorf("生成会话 ID 失败: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// goSessionIDFromRequest 取出请求携带的 Go 会话 ID。
func (s *Server) goSessionIDFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(s.cfg.GoSessionCookie)
	if err != nil {
		return ""
	}
	if !sessionIDAllowed(cookie.Value) {
		return ""
	}
	return cookie.Value
}

// setGoSessionCookie 下发 Go 会话 Cookie。
//
// 属性与 PHPSESSID 一致：Path=/、HttpOnly、SameSite=Lax。
// SameSite 用 Lax 而非 Strict：Strict 会让用户从外部链接点进来时
// 不带 Cookie，表现为"点开邮件里的题链就掉登录"。
// Secure 依赖 HTTPS 部署，此处不开（内网直连 HTTP 的场景仍需可用）。
func (s *Server) setGoSessionCookie(w http.ResponseWriter, sid string) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.GoSessionCookie,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

// clearGoSessionCookie 让浏览器丢弃 Go 会话。
func (s *Server) clearGoSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.GoSessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// resolveIdentity 解析当前请求的身份。
//
// 顺序：
//  1. Go 会话（Redis）—— 这是本系统的真相来源；
//  2. PHP 会话（只读）—— 仅当 Go 会话不存在时兜底识别老用户，
//     识别成功就立刻补发一枚 Go 会话并种下 Cookie；
//  3. 匿名。
//
// 第 2 步是迁移期的桥：它只在用户还带着旧 PHPSESSID 时命中一次，
// 之后请求就走第 1 步，不再读 PHP 目录。
//
// 解析失败一律按匿名处理：私有比赛的受邀判断依赖会话内容，
// 把"读不到"当成"已授权"会泄露题目。
func (s *Server) resolveIdentity(w http.ResponseWriter, r *http.Request) Identity {
	ctx := r.Context()

	if sid := s.goSessionIDFromRequest(r); sid != "" {
		sess, err := s.sessions.Get(ctx, sid)
		if err == nil {
			// 续期：活跃用户不该掉登录。
			_ = s.sessions.Touch(ctx, sid)
			return s.identityFromGo(sess, sid)
		}
		if !errors.Is(err, errSessionNotFound) {
			s.log.Error("读取 Go 会话失败", "err", err)
		}
	}

	return Anonymous()
}

// identityFromGo 把 Go 会话转成 Identity。
func (s *Server) identityFromGo(sess *GoSession, sid string) Identity {
	id := Anonymous()
	id.SessionID = sid
	if sess.UserID == "" {
		return id
	}
	id.UserID = sess.UserID
	id.LoggedIn = true
	id.Admin = sess.Privileges.Admin
	id.ContestCreator = sess.Privileges.ContestCreator
	id.ProblemEditor = sess.Privileges.ProblemEditor
	id.ProblemVerifier = sess.Privileges.ProblemVerifier
	id.SourceBrowser = sess.Privileges.SourceBrowser
	id.Nick = sess.Nick
	id.GroupName = sess.GroupName
	if len(sess.ContestIDs) > 0 {
		id.ContestInvites = make(map[int32]bool, len(sess.ContestIDs))
		for k, v := range sess.ContestIDs {
			if v {
				if cid, ok := contestIDFromSuffix(k, 'c'); ok {
					id.ContestInvites[cid] = true
				}
				if cid, ok := contestIDFromSuffix(k, 'm'); ok {
					if id.ContestTimeLimitUsed == nil {
						id.ContestTimeLimitUsed = make(map[int32]bool)
					}
					id.ContestTimeLimitUsed[cid] = true
				}
			}
		}
	}
	return id
}

// addContestID 在会话里标记一个带数字后缀的比赛状态键。
func (s *Server) addContestID(sess *GoSession, kind byte, cid int32) {
	if sess.ContestIDs == nil {
		sess.ContestIDs = make(map[string]bool)
	}
	sess.ContestIDs[string(kind)+fmt.Sprint(cid)] = true
}

// startGoSession 返回一个可用的 Go 会话 ID，必要时新建并种下 Cookie。
//
// 供"当前可能匿名、但需要往会话里存东西"的场景用（验证码）。
// 已登录的请求不该调它：登录态由 resolveIdentity 负责解析，
// 在这里新建会话会绕过迁移桥，把老用户当成匿名。
func (s *Server) startGoSession(w http.ResponseWriter, r *http.Request) (string, error) {
	if sid := s.goSessionIDFromRequest(r); sid != "" {
		return sid, nil
	}
	sid, err := newGoSessionID()
	if err != nil {
		return "", err
	}
	// 立刻落一条空会话：验证码答案要挂在会话上。
	if err := s.sessions.Save(r.Context(), sid, &GoSession{}); err != nil {
		return "", err
	}
	s.setGoSessionCookie(w, sid)
	return sid, nil
}
