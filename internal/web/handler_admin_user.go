// 后台用户/权限管理的自包含页面（不依赖 admin/ajax.php、上传、CSV 导出）：
//   admin/privilege_list.php、privilege_add.php、privilege_delete.php、
//   changepass.php、user_df_change.php、user_set_ip.php
//
// 权限（与 PHP 对齐）：
//   - privilege_*、user_df_change：仅 administrator。
//   - changepass：administrator 或 password_setter —— 但 Identity 目前不建模
//     password_setter（见 session_redis.go 的 GoPrivileges），此处置为仅 administrator，
//     与 adminMenuPage 里 IsPasswordSetter 恒为 false 的现状一致。
//   - user_set_ip：PHP 只由 admin-header.php 把关（五角色之一）；这里用
//     hasAdminPrivilege，是当前 Identity 能表达的最接近的等价。
//
// CSRF：nosurf 全局中间件；privilege_delete 是 POST，天然受保护。
// PHP 的 set_post_key/check_post_key/getkey 不复刻；privilege_add 的管理员邮件
// 通知（email()）未迁移，属 best-effort 且依赖未迁移的邮件配置。

package web

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

const privilegePerPage = 25

// privilegeRightOptions 是 privilege_add 下拉里的可授权限，顺序与 PHP 一致。
var privilegeRightOptions = []string{
	"administrator", "problem_editor", "problem_importer", "tag_adder",
	"problem_verifiter", "source_browser", "contest_creator", "user_adder",
	"http_judge", "password_setter", "printer", "balloon", "vip",
	"problem_start", "problem_end", "service_port",
}

type adminPrivilegeRowVM struct {
	UserID    string
	RightText string // rightstr，valuestr != "true" 时追加 ":valuestr"
	CanDelete bool
}

type adminPrivilegeListVM struct {
	Rows           []adminPrivilegeRowVM
	Keyword        string
	Page           int
	TotalPages     int
	Pages          []adminNewsLinkVM
	ShowPagination bool
	PrevPage       int
	NextPage       int
	Token          *http.Request
}

type adminPrivilegeAddVM struct {
	UserID  string
	Options []string
	Message string
	Token   *http.Request
}

type adminChangePassVM struct {
	UserID  string
	Message string
	IsError bool
	Token   *http.Request
}

type adminUserSetIPVM struct {
	UserID  string
	Message string
	Token   *http.Request
}

// ── admin/privilege_list.php ─────────────────────────────────────────

func (s *Server) handleAdminPrivilegeList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}

	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	page := parseInt(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	var rows []adminPrivilegeRowVM
	totalPages := 1
	build := func(list []dbgen.Privilege) {
		rows = make([]adminPrivilegeRowVM, 0, len(list))
		for _, p := range list {
			text := p.Rightstr
			if p.Valuestr != "true" {
				text += ":" + p.Valuestr
			}
			rows = append(rows, adminPrivilegeRowVM{
				UserID:    p.UserID,
				RightText: text,
				CanDelete: !(p.Rightstr == "administrator" && p.UserID == identity.UserID),
			})
		}
	}

	if keyword != "" {
		like := "%" + keyword + "%"
		found, err := s.queries().SearchPrivileges(ctx, dbgen.SearchPrivilegesParams{
			UserID:   like,
			Rightstr: like,
		})
		if err != nil {
			s.log.Error("搜索权限失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		build(found)
	} else {
		count, err := s.queries().CountPrivileges(ctx)
		if err != nil {
			s.log.Error("统计权限失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		totalPages = int((count + privilegePerPage - 1) / privilegePerPage)
		if totalPages < 1 {
			totalPages = 1
		}
		if page > totalPages {
			page = totalPages
		}
		found, err := s.queries().ListPrivileges(ctx, dbgen.ListPrivilegesParams{
			Limit:  privilegePerPage,
			Offset: int32((page - 1) * privilegePerPage),
		})
		if err != nil {
			s.log.Error("查询权限失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		build(found)
	}

	vm := adminPrivilegeListVM{
		Rows:       rows,
		Keyword:    keyword,
		Page:       page,
		TotalPages: totalPages,
		Token:      r,
	}
	if keyword == "" {
		vm.ShowPagination = true
		vm.PrevPage = page
		if page > 1 {
			vm.PrevPage = page - 1
		}
		vm.NextPage = page
		if page < totalPages {
			vm.NextPage = page + 1
		}
		const pagesPerFrame = 5
		frame := (page-1)/pagesPerFrame + 1
		spage := (frame-1)*pagesPerFrame + 1
		epage := spage + pagesPerFrame - 1
		if epage > totalPages {
			epage = totalPages
		}
		for i := spage; i <= epage; i++ {
			vm.Pages = append(vm.Pages, adminNewsLinkVM{Num: i, Active: i == page})
		}
	}

	s.renderPage(w, r, http.StatusOK, "adminPrivilegeListPage", "权限列表", vm)
}

// ── admin/privilege_add.php ──────────────────────────────────────────

func (s *Server) handleAdminPrivilegeAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}

	vm := adminPrivilegeAddVM{Options: privilegeRightOptions, Token: r}

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		userID := strings.TrimSpace(r.FormValue("user_id"))
		rightstr := strings.TrimSpace(r.FormValue("rightstr"))
		valuestr := "true"
		if v := r.FormValue("valuestr"); v != "" {
			valuestr = v
		}
		if _, ok := r.Form["contest"]; ok {
			rightstr = "c" + rightstr
		}
		if _, ok := r.Form["psv"]; ok {
			rightstr = "s" + rightstr
		}

		// PHP：仅当 user_id 非空才插入。
		if userID != "" {
			if err := s.queries().InsertPrivilege(ctx, dbgen.InsertPrivilegeParams{
				UserID:   userID,
				Rightstr: rightstr,
				Valuestr: valuestr,
			}); err != nil {
				s.log.Error("添加权限失败", "err", err)
				http.Error(w, "保存失败", http.StatusInternalServerError)
				return
			}
			vm.Message = "User " + userID + "'s Privilege Added!"
		}
		vm.UserID = userID
	}

	if vm.UserID == "" {
		vm.UserID = r.URL.Query().Get("uid")
	}
	s.renderPage(w, r, http.StatusOK, "adminPrivilegeAddPage", "添加权限", vm)
}

// ── admin/privilege_delete.php ───────────────────────────────────────

func (s *Server) handleAdminPrivilegeDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	userID := r.FormValue("uid")
	rightstr := r.FormValue("rightstr")
	if userID == "" || rightstr == "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("Missing uid or rightstr"))
		return
	}
	// PHP：不允许移除自己的 administrator 权限。
	if userID == identity.UserID && rightstr == "administrator" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("Can't remove administrator for yourself!"))
		return
	}

	if err := s.queries().DeletePrivilege(ctx, dbgen.DeletePrivilegeParams{
		UserID:   userID,
		Rightstr: rightstr,
	}); err != nil {
		s.log.Error("删除权限失败", "err", err)
		http.Error(w, "Delete failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(userID + " " + rightstr + " deleted!"))
}

// ── admin/changepass.php ─────────────────────────────────────────────

func (s *Server) handleAdminChangePass(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}

	vm := adminChangePassVM{Token: r}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		userID := r.FormValue("user_id")
		passwd := r.FormValue("passwd")
		if _, ok := r.Form["do"]; ok && userID != "" && passwd != "" {
			hash, err := pwGen(passwd, false)
			if err != nil {
				s.log.Error("生成密码哈希失败", "err", err)
				http.Error(w, "保存失败", http.StatusInternalServerError)
				return
			}
			affected, err := s.queries().UpdateUserPasswordAdmin(ctx, dbgen.UpdateUserPasswordAdminParams{
				Password: sql.NullString{String: hash, Valid: true},
				UserID:   userID,
			})
			if err != nil {
				s.log.Error("修改密码失败", "err", err)
				http.Error(w, "保存失败", http.StatusInternalServerError)
				return
			}
			if affected == 1 {
				vm.Message = "User " + userID + "'s Password Changed!"
			} else {
				vm.Message = "There is No such User " + userID + "! or User " + userID + " is administrator!"
				vm.IsError = true
			}
		}
		vm.UserID = userID
	}

	if vm.UserID == "" {
		vm.UserID = r.URL.Query().Get("uid")
	}
	s.renderPage(w, r, http.StatusOK, "adminChangePassPage", "设置密码", vm)
}

// ── admin/user_df_change.php ─────────────────────────────────────────

func (s *Server) handleAdminUserDefunctChange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}

	userID := r.URL.Query().Get("cid")
	// PHP：不能封禁自己。
	if userID == "" || userID == identity.UserID {
		s.renderPage(w, r, http.StatusOK, "errorPage", "操作被拒绝", map[string]string{
			"Message": "Can't defunct yourself!",
		})
		return
	}

	defunct, err := s.queries().GetUserDefunct(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) {
		s.renderPage(w, r, http.StatusOK, "errorPage", "无此用户", map[string]string{
			"Message": "No Such User!",
		})
		return
	}
	if err != nil {
		s.log.Error("读取用户状态失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	next := "Y"
	if defunct == "Y" {
		next = "N"
	}
	if err := s.queries().SetUserDefunct(ctx, dbgen.SetUserDefunctParams{
		Defunct: next,
		UserID:  userID,
	}); err != nil {
		s.log.Error("切换用户状态失败", "err", err)
		http.Error(w, "保存失败", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/user_list.php", http.StatusFound)
}

// ── admin/user_set_ip.php ────────────────────────────────────────────

func (s *Server) handleAdminUserSetIP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !s.hasAdminPrivilege(identity) {
		s.adminLoginRequired(w)
		return
	}

	vm := adminUserSetIPVM{Token: r}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		userID := r.FormValue("user_id")
		ip := r.FormValue("ip")
		if _, ok := r.Form["do"]; ok {
			if err := s.queries().InsertLoginLog(ctx, dbgen.InsertLoginLogParams{
				UserID:   userID,
				Password: sql.NullString{String: "set ip by " + identity.UserID, Valid: true},
				Ip:       sql.NullString{String: ip, Valid: true},
			}); err != nil {
				s.log.Error("写入登录日志失败", "err", err)
				http.Error(w, "保存失败", http.StatusInternalServerError)
				return
			}
			vm.Message = "User " + userID + "'s Login IP Changed!"
		}
		vm.UserID = userID
	}

	if vm.UserID == "" {
		vm.UserID = r.URL.Query().Get("uid")
	}
	s.renderPage(w, r, http.StatusOK, "adminUserSetIPPage", "设置登录 IP", vm)
}

func (s *Server) registerAdminUserRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/privilege_list.php", s.handleAdminPrivilegeList)
	mux.HandleFunc("GET /admin/privilege_add.php", s.handleAdminPrivilegeAdd)
	mux.HandleFunc("POST /admin/privilege_add.php", s.handleAdminPrivilegeAdd)
	mux.HandleFunc("POST /admin/privilege_delete.php", s.handleAdminPrivilegeDelete)
	mux.HandleFunc("GET /admin/changepass.php", s.handleAdminChangePass)
	mux.HandleFunc("POST /admin/changepass.php", s.handleAdminChangePass)
	mux.HandleFunc("GET /admin/user_df_change.php", s.handleAdminUserDefunctChange)
	mux.HandleFunc("GET /admin/user_set_ip.php", s.handleAdminUserSetIP)
	mux.HandleFunc("POST /admin/user_set_ip.php", s.handleAdminUserSetIP)
	mux.HandleFunc("GET /admin/user_list.php", s.handleAdminUserList)
}
