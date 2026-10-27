// PHP 会话桥：把 PHP-FPM 写入的会话文件解析成 Identity。
//
// 路径与键名前缀必须与 PHP 侧完全一致。键前缀来自 $OJ_NAME
// （phpweb/include/db_info.inc.php:16），线上实测为
// "吉尔吉斯倔驴大学OJ"，因此会话键形如
// "吉尔吉斯倔驴大学OJ_user_id"。
//
// 曾经这里硬编码 "hustoj_"，后果是：文件读得到、也解析得出键名，
// 但没有一个键能匹配上，表现为所有人都是匿名、CSRF 永远校验失败。
// 所以前缀现在由配置注入，且在 validate() 里强制要求非空。

package web

import (
	"net/http"
	"strconv"
	"strings"
)

// SessionBridge 从 PHP 会话文件解析身份。
type SessionBridge struct {
	store *SessionStore
}

// NewSessionBridge 创建桥接器。sessionDir 为空时返回 nil，
// 调用方据此退回匿名。
func NewSessionBridge(sessionDir, ojName string) *SessionBridge {
	if sessionDir == "" || ojName == "" {
		return nil
	}
	return &SessionBridge{store: NewSessionStore(sessionDir, ojName)}
}

// Resolve 从请求的 Cookie 中取 session_id，读取会话文件并解析 Identity。
func (b *SessionBridge) Resolve(r *http.Request) Identity {
	sid := sessionIDFromRequest(r)
	if sid == "" {
		return Anonymous()
	}
	return b.resolveSID(sid)
}

// resolveSID 按给定的会话 ID 解析身份。
//
// 登录成功后需要用刚写入的新 SID 构造 Identity，此时请求里还没有
// PHPSESSID Cookie，所以不能走 Resolve。
func (b *SessionBridge) resolveSID(sid string) Identity {
	if !sessionIDAllowed(sid) {
		return Anonymous()
	}
	s, err := b.store.get(sid)
	if err != nil {
		// 会话不存在、不可读或 ID 非法 -> 匿名。
		// 解析不了身份时按拒绝处理是安全的：私有比赛的受邀判断
		// 依赖会话内容，误判为"已授权"会泄露题目。
		return Anonymous()
	}
	return b.identityFrom(s, sid)
}

// sessionIDFromRequest 取出合法的会话 ID。
func sessionIDFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(phpSessionCookieName)
	if err != nil {
		return ""
	}
	if !sessionIDAllowed(cookie.Value) {
		return ""
	}
	return cookie.Value
}

// identityFrom 把解析出的会话转成 Identity。
func (b *SessionBridge) identityFrom(s *phpSession, sid string) Identity {
	id := Anonymous()
	id.SessionID = sid

	// CSRF 池必须在"是否已登录"判断之外读取。
	//
	// 登录页本身就要发 token，而此时用户还没登录、user_id 为空。
	// 若跟着上面的早返回一起跳过，登录页会一直发不出 token，
	// 表现为"CSRF 永远校验失败"。
	id.CSRFKeys = s.getStringSlice(b.store.key("csrf_keys"))

	// user_id 存在且非空才算登录。
	//
	// 早期实现是"有会话文件即视为已登录"，这会把匿名会话
	// （只有 csrf_keys / vcode 的文件）也判成登录，
	// 于是登录页第二次 GET 就被重定向走，用户看到"登录后立刻掉登录"。
	uid, _ := s.getString(b.store.key("user_id"))
	if uid == "" {
		return id
	}
	id.UserID = uid
	id.LoggedIn = true

	id.Admin = s.getBool(b.store.key("administrator"))
	id.ContestCreator = s.getBool(b.store.key("contest_creator"))
	id.ProblemEditor = s.getBool(b.store.key("problem_editor"))
	// PHP 侧拼写错误是 problem_verifiter（少一个 i），见 contest-check.php。
	id.ProblemVerifier = s.getBool(b.store.key("problem_verifiter"))
	id.SourceBrowser = s.getBool(b.store.key("source_browser"))
	id.Nick, _ = s.getString(b.store.key("nick"))
	id.GroupName, _ = s.getString(b.store.key("group_name"))

	// 受邀标记 c<cid> 与限时赛已用标记 m<cid>。
	// 键名带数字后缀，不逐个已知 cid 去查，而是扫出所有前缀键。
	for _, k := range s.keys {
		if !strings.HasPrefix(k, b.store.prefix+"_") {
			continue
		}
		suffix := k[len(b.store.prefix)+1:]
		if !s.getBool(k) {
			continue
		}
		if cid, ok := contestIDFromSuffix(suffix, 'c'); ok {
			if id.ContestInvites == nil {
				id.ContestInvites = make(map[int32]bool)
			}
			id.ContestInvites[cid] = true
		}
		if cid, ok := contestIDFromSuffix(suffix, 'm'); ok {
			if id.ContestTimeLimitUsed == nil {
				id.ContestTimeLimitUsed = make(map[int32]bool)
			}
			id.ContestTimeLimitUsed[cid] = true
		}
	}

	return id
}

// contestIDFromSuffix 解析 "c1000" / "m1000" 形式的后缀。
func contestIDFromSuffix(suffix string, kind byte) (int32, bool) {
	if len(suffix) < 2 || suffix[0] != kind {
		return 0, false
	}
	n, err := strconv.Atoi(suffix[1:])
	if err != nil || n <= 0 {
		return 0, false
	}
	return int32(n), true
}
