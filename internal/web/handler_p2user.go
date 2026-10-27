// P2 用户功能（mail.php、modify.php、lostpassword.php、lostpassword2.php）

package web

import (
	"database/sql"
	"fmt"
	"html"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// ── mail.php ─────────────────────────────────────────────────────────

// MailItem 是邮件项。
type MailItem struct {
	MailID   int32
	FromUser string
	ToUser   string
	Title    string
	Content  string
	InDate   string
	NewMail  bool
}

// MailPageVM 是邮件页的视图模型。
type MailPageVM struct {
	Title    string
	OJName   string
	MailList []MailItem
	ViewMail *MailItem
	ToUser   string
	Title2   string
}

func (s *Server) handleMail(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)

	if !identity.LoggedIn {
		s.renderPage(w, r, http.StatusOK, "errorPage", "需要登录", map[string]string{
			"Message": "<a href=\"/loginpage.php\">请先登录</a>",
		})
		return
	}

	var viewMail *MailItem
	if vid := r.URL.Query().Get("vid"); vid != "" {
		id := parseInt32(vid)
		mails, err := s.queries().GetMailByID(ctx, dbgen.GetMailByIDParams{MailID: id, UserID: identity.UserID})
		if err != nil {
			s.log.Error("查询邮件失败", "err", err)
		} else if len(mails) > 0 {
			m := mails[0]
			content := ""
			if m.Content.Valid {
				content = m.Content.String
			}
			inDate := ""
			if m.InDate.Valid {
				inDate = m.InDate.Time.Format("2006-01-02 15:04:05")
			}
			viewMail = &MailItem{
				MailID: m.MailID, FromUser: m.FromUser, ToUser: m.ToUser,
				Title: m.Title, Content: content, InDate: inDate, NewMail: m.NewMail,
			}
			_ = s.queries().MarkMailRead(ctx, id)
		}
	}

	if r.Method == http.MethodPost {
		toUser := r.FormValue("to_user")
		title := r.FormValue("title")
		content := r.FormValue("content")

		privs, err := s.queries().CheckMailPrivilege(ctx, dbgen.CheckMailPrivilegeParams{
			FromUser: identity.UserID, ToUser: toUser,
		})
		if err != nil || len(privs) == 0 {
			s.renderPage(w, r, http.StatusOK, "errorPage", "无权限", map[string]string{
				"Message": "邮件只能在教师和学生之间发送",
			})
			return
		}

		err = s.queries().SendMail(ctx, dbgen.SendMailParams{
			ToUser: toUser, FromUser: identity.UserID, Title: title, Content: sql.NullString{String: content, Valid: true},
		})
		if err != nil {
			s.log.Error("发送邮件失败", "err", err)
			s.renderPage(w, r, http.StatusOK, "errorPage", "发送失败", map[string]string{
				"Message": "Not Mailed!",
			})
			return
		}
	}

	mails, err := s.queries().GetMailList(ctx, dbgen.GetMailListParams{UserID: identity.UserID})
	if err != nil {
		s.log.Error("查询邮件列表失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	mailList := make([]MailItem, 0, len(mails))
	for _, m := range mails {
		content := ""
		if m.Content.Valid {
			content = m.Content.String
		}
		inDate := ""
		if m.InDate.Valid {
			inDate = m.InDate.Time.Format("2006-01-02 15:04:05")
		}
		mailList = append(mailList, MailItem{
			MailID: m.MailID, FromUser: m.FromUser, ToUser: m.ToUser,
			Title: m.Title, Content: content, InDate: inDate, NewMail: m.NewMail,
		})
	}

	toUser := html.EscapeString(r.URL.Query().Get("to_user"))
	title2 := html.EscapeString(r.URL.Query().Get("title"))

	s.renderPage(w, r, http.StatusOK, "mailPage", "邮件", MailPageVM{
		Title: "邮件", OJName: s.cfg.OJName, MailList: mailList,
		ViewMail: viewMail, ToUser: toUser, Title2: title2,
	})
}

// ── modify.php ───────────────────────────────────────────────────────

// ModifyPageVM 是用户信息修改页的视图模型。
type ModifyPageVM struct {
	Title  string
	OJName string
	UserID string
	Nick   string
	School string
	Email  string
	Error  string
}

func (s *Server) handleModify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)

	if !identity.LoggedIn {
		s.renderPage(w, r, http.StatusOK, "errorPage", "需要登录", map[string]string{
			"Message": "<a href=\"/loginpage.php\">请先登录</a>",
		})
		return
	}

	if r.Method != http.MethodPost {
		user, err := s.queries().GetUserByID(ctx, identity.UserID)
		if err != nil {
			s.log.Error("查询用户信息失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		email := ""
		if user.Email.Valid {
			email = user.Email.String
		}
		s.renderPage(w, r, http.StatusOK, "modifyPage", "修改用户信息", ModifyPageVM{
			Title: "修改用户信息", OJName: s.cfg.OJName,
			UserID: user.UserID, Nick: user.Nick, School: user.School, Email: email,
		})
		return
	}

	opassword := r.FormValue("opassword")
	npassword := r.FormValue("npassword")
	rptpassword := r.FormValue("rptpassword")
	nick := strings.TrimSpace(r.FormValue("nick"))
	school := strings.TrimSpace(r.FormValue("school"))
	email := strings.TrimSpace(r.FormValue("email"))

	var errMsg []string

	user, err := s.queries().GetUserByID(ctx, identity.UserID)
	if err != nil {
		s.log.Error("查询用户信息失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	savedPassword := ""
	if user.Password.Valid {
		savedPassword = user.Password.String
	}
	if !verifyPassword(opassword, savedPassword) {
		errMsg = append(errMsg, "旧密码错误")
	}

	if len(npassword) > 0 {
		if tooSimple(npassword) {
			errMsg = append(errMsg, "密码太简单")
		} else if npassword != rptpassword {
			errMsg = append(errMsg, "两次密码不一致")
		}
	}

	if len(nick) > 100 {
		errMsg = append(errMsg, "昵称太长")
	} else if len(nick) == 0 {
		nick = identity.UserID
	}

	if len(school) > 100 {
		errMsg = append(errMsg, "学校太长")
	}
	if len(email) > 100 {
		errMsg = append(errMsg, "邮箱太长")
	}

	if len(errMsg) > 0 {
		s.renderPage(w, r, http.StatusOK, "modifyPage", "修改用户信息", ModifyPageVM{
			Title: "修改用户信息", OJName: s.cfg.OJName,
			UserID: identity.UserID, Nick: nick, School: school, Email: email,
			Error: strings.Join(errMsg, "\\n"),
		})
		return
	}

	newPassword := hashPassword(opassword)
	if len(npassword) > 0 {
		newPassword = hashPassword(npassword)
	}

	nick = html.EscapeString(nick)
	school = html.EscapeString(school)
	email = html.EscapeString(email)

	err = s.queries().UpdateUserProfile(ctx, dbgen.UpdateUserProfileParams{
		Password: sql.NullString{String: newPassword, Valid: true},
		Nick:     nick, School: school, Email: sql.NullString{String: email, Valid: true},
		UserID: identity.UserID,
	})
	if err != nil {
		s.log.Error("更新用户信息失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	if nick != "" {
		_ = s.queries().UpdateUserSolutionNick(ctx, dbgen.UpdateUserSolutionNickParams{Nick: nick, UserID: identity.UserID})
	}

	http.Redirect(w, r, "/", http.StatusFound)
}

// ── lostpassword.php ─────────────────────────────────────────────────

// LostPasswordPageVM 是密码重置页的视图模型。
type LostPasswordPageVM struct {
	Title  string
	OJName string
	Error  string
	Step   int
	UserID string
}

// lostPasswordKeys 存储密码重置的临时密钥。
var lostPasswordKeys = struct {
	sync.RWMutex
	data map[string]string
}{data: make(map[string]string)}

func setLostPasswordKey(userID, key string) {
	lostPasswordKeys.Lock()
	defer lostPasswordKeys.Unlock()
	lostPasswordKeys.data[userID] = key
}

func getLostPasswordKey(userID string) (string, bool) {
	lostPasswordKeys.RLock()
	defer lostPasswordKeys.RUnlock()
	k, ok := lostPasswordKeys.data[userID]
	return k, ok
}

func deleteLostPasswordKey(userID string) {
	lostPasswordKeys.Lock()
	defer lostPasswordKeys.Unlock()
	delete(lostPasswordKeys.data, userID)
}

func (s *Server) handleLostPassword(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method != http.MethodPost {
		s.renderPage(w, r, http.StatusOK, "lostPasswordPage", "密码重置", LostPasswordPageVM{
			Title: "密码重置", OJName: s.cfg.OJName, Step: 1,
		})
		return
	}

	userID := r.FormValue("user_id")
	email := r.FormValue("email")

	emailResult, err := s.queries().GetUserEmail(ctx, userID)
	if err != nil || !emailResult.Valid || emailResult.String != email || !strings.Contains(email, "@") {
		s.renderPage(w, r, http.StatusOK, "lostPasswordPage", "密码重置", LostPasswordPageVM{
			Title: "密码重置", OJName: s.cfg.OJName, Step: 1, Error: "用户名与Email不匹配",
		})
		return
	}

	key := generateToken(16)
	setLostPasswordKey(userID, key)

	mailTitle := "OJ系统密码重置激活"
	mailContent := fmt.Sprintf("%s:\n您好！\n您选择了找回密码服务，请将下面字符串输入口令重置页面以确认身份:\n%s\n注意：验证通过后，上面这个字符串将成为您的新密码!", userID, key)
	_ = s.queries().SendMail(ctx, dbgen.SendMailParams{
		ToUser: userID, FromUser: "system", Title: mailTitle, Content: sql.NullString{String: mailContent, Valid: true},
	})

	s.renderPage(w, r, http.StatusOK, "lostPassword2Page", "密码重置", LostPasswordPageVM{
		Title: "密码重置", OJName: s.cfg.OJName, Step: 2, UserID: userID,
	})
}

// ── lostpassword2.php ────────────────────────────────────────────────

func (s *Server) handleLostPassword2(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if r.Method != http.MethodPost {
		s.renderPage(w, r, http.StatusOK, "lostPassword2Page", "密码重置", LostPasswordPageVM{
			Title: "密码重置", OJName: s.cfg.OJName, Step: 2,
		})
		return
	}

	userID := r.FormValue("user_id")
	lostKey := r.FormValue("lost_key")

	expectedKey, ok := getLostPasswordKey(userID)
	if !ok || expectedKey != lostKey {
		s.renderPage(w, r, http.StatusOK, "errorPage", "密码重置失败", map[string]string{
			"Message": "Password Reset Fail",
		})
		return
	}

	err := s.queries().UpdateUserPasswordByKey(ctx, dbgen.UpdateUserPasswordByKeyParams{
		Password: sql.NullString{String: hashPassword(lostKey), Valid: true}, UserID: userID,
	})
	if err != nil {
		s.log.Error("密码重置失败", "err", err)
		s.renderPage(w, r, http.StatusOK, "errorPage", "密码重置失败", map[string]string{
			"Message": "Password Reset Fail",
		})
		return
	}

	deleteLostPasswordKey(userID)
	s.renderPage(w, r, http.StatusOK, "errorPage", "密码重置成功", map[string]string{
		"Message": "密码已重置为您输入的密钥。点击 <a href=\"/\">这里</a> 登录!",
	})
}

// ── 辅助函数 ─────────────────────────────────────────────────────────

func parseInt32(s string) int32 {
	var n int32
	fmt.Sscanf(s, "%d", &n)
	return n
}

func tooSimple(password string) bool {
	return len(password) < 6
}

func hashPassword(password string) string {
	h, _ := pwGen(password, false)
	return h
}

func verifyPassword(password, saved string) bool {
	return pwCheck(password, saved)
}

func generateToken(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = chars[time.Now().UnixNano()%int64(len(chars))]
	}
	return string(b)
}

// ── setlang.php ───────────────────────────────────────────────────────

var allowedLangs = map[string]bool{"cn": true, "ug": true, "en": true, "fa": true, "ko": true, "th": true}

func (s *Server) handleSetLang(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	if allowedLangs[lang] {
		http.SetCookie(w, &http.Cookie{
			Name:     "lang",
			Value:    lang,
			Path:     "/",
			MaxAge:   604800,
			HttpOnly: false,
			SameSite: http.SameSiteLaxMode,
		})
	}
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

// ── refresh-privilege.php ────────────────────────────────────────────

func (s *Server) handleRefreshPrivilege(w http.ResponseWriter, r *http.Request) {
	identity := s.resolveIdentity(w, r)
	if !identity.LoggedIn {
		http.Redirect(w, r, "/loginpage.php", http.StatusFound)
		return
	}
	http.Redirect(w, r, r.Referer(), http.StatusFound)
}

// ── reinfo.php ────────────────────────────────────────────────────────

// ReinfoPageVM 是运行错误信息页的视图模型。
type ReinfoPageVM struct {
	Title    string
	OJName   string
	SID      int32
	Mark     float64
	ErrorMsg string
}

func (s *Server) handleReinfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)

	if !identity.LoggedIn {
		http.Redirect(w, r, "/loginpage.php", http.StatusFound)
		return
	}

	sid := int32(parseInt(r.URL.Query().Get("sid")))
	if sid <= 0 {
		s.renderPage(w, r, http.StatusOK, "errorPage", "无此代码", map[string]string{
			"Message": "No such code!",
		})
		return
	}

	sol, err := s.queries().GetSolutionStatusByID(ctx, uint32(sid))
	if err != nil {
		s.renderPage(w, r, http.StatusOK, "errorPage", "无此代码", map[string]string{
			"Message": "No such code!",
		})
		return
	}

	if sol.UserID != identity.UserID && !identity.SourceBrowser {
		s.renderPage(w, r, http.StatusOK, "errorPage", "访问被拒绝", map[string]string{
			"Message": "Access Denied",
		})
		return
	}

	mark := sol.PassRate * 100
	if sol.Result == 4 {
		mark = 100
	}

	runtimeErr, _ := s.queries().GetRuntimeError(ctx, sid)
	errorMsg := ""
	if runtimeErr.Valid {
		errorMsg = runtimeErr.String
	}

	s.renderPage(w, r, http.StatusOK, "reinfoPage", "运行错误信息", ReinfoPageVM{
		Title: "运行错误信息", OJName: s.cfg.OJName,
		SID: sid, Mark: mark, ErrorMsg: errorMsg,
	})
}

// ── active.php ────────────────────────────────────────────────────────

func (s *Server) handleActive(w http.ResponseWriter, r *http.Request) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if len(code) == 18 {
		_ = s.queries().ActivateUserByCode(r.Context(), code)
	}
	http.Redirect(w, r, "/loginpage.php", http.StatusFound)
}

func (s *Server) registerP2UserRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/mail.php", s.handleMail)
	mux.HandleFunc("/modify.php", s.handleModify)
	mux.HandleFunc("/modifypage.php", s.handleModify)
	mux.HandleFunc("/lostpassword.php", s.handleLostPassword)
	mux.HandleFunc("/lostpassword2.php", s.handleLostPassword2)
	mux.HandleFunc("/setlang.php", s.handleSetLang)
	mux.HandleFunc("/refresh-privilege.php", s.handleRefreshPrivilege)
	mux.HandleFunc("/reinfo.php", s.handleReinfo)
	mux.HandleFunc("/active.php", s.handleActive)
}
