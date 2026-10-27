// 后台用户列表（admin/user_list.php）。
//
// 权限：administrator 或 password_setter（Identity 不建模后者 → 仅 administrator，
// 与 adminMenuPage 的现状一致）。
//
// 表格行的行内编辑（双击字段）由 admin/ajax.php 承载；本页只负责渲染 spans
// （fd=<字段> user_id=<用户>）并把 nosurf token 放到 data-csrf 供内联 JS 使用。
// 迁移期 ajax.php 同批切到 Go，因此行内编辑仍然可用。
//
// 已知差异：PHP 的 parent_phone 悬浮二维码（依赖未迁移的 xiaoke.php 与 qrcode.min.js）
// 与 SaaS 子域名邮箱链接未复刻。

package web

import (
	"database/sql"
	"net/http"
	"net/url"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

const usersPerPage = 25

type adminUserRowVM struct {
	UserID      string
	Nick        string
	Email       string
	IP          string
	IPHref      string
	School      string
	GroupName   string
	ParentPhone string
	AcceTime    string
	RegTime     string
	ExpiryDate  string
	ExpiryClass string
	CoinEarned  uint32
	CoinBonus   uint32
	CoinSpent   uint32
	CoinBalance int64
	Normal      bool // defunct == 'N'
	CanManage   bool // 管理员且非本人
}

type adminUserListVM struct {
	Rows           []adminUserRowVM
	Keyword        string
	TrashSuffix    string
	Page           int
	TotalPages     int
	Pages          []adminNewsLinkVM
	ShowPagination bool
	PrevPage       int
	NextPage       int
	Token          *http.Request
}

func fmtTime(t sql.NullTime) string {
	if !t.Valid {
		return ""
	}
	return t.Time.Format("2006-01-02 15:04:05")
}

// expiryClass 复刻 user_list.php 的三色规则：> now+7d 绿、>= now 蓝、否则红。
func expiryClass(expiry time.Time, now time.Time) string {
	if expiry.After(now.AddDate(0, 0, 7)) {
		return "green"
	}
	if !expiry.Before(now) {
		return "blue"
	}
	return "red"
}

func (s *Server) handleAdminUserList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}

	total, err := s.queries().CountUsers(ctx)
	if err != nil {
		s.log.Error("统计用户数失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}
	pages := int((total + usersPerPage - 1) / usersPerPage)
	page := parsePage(r.URL.Query().Get("page"))

	keyword := r.URL.Query().Get("keyword")
	_, trash := r.URL.Query()["trash"]

	now := time.Now()
	add := func(userID, nick string, email sql.NullString, acce, reg sql.NullTime, expiry time.Time,
		ip, school, group, phone, defunct string, earned, bonus, spent uint32) adminUserRowVM {
		normal := defunct != "Y"
		return adminUserRowVM{
			UserID:      userID,
			Nick:        nick,
			Email:       email.String,
			IP:          ip,
			IPHref:      "/admin/user_list.php?keyword=" + url.QueryEscape(ip),
			School:      school,
			GroupName:   group,
			ParentPhone: phone,
			AcceTime:    fmtTime(acce),
			RegTime:     fmtTime(reg),
			ExpiryDate:  expiry.Format("2006-01-02"),
			ExpiryClass: expiryClass(expiry, now),
			CoinEarned:  earned,
			CoinBonus:   bonus,
			CoinSpent:   spent,
			CoinBalance: int64(earned) + int64(bonus) - int64(spent),
			Normal:      normal,
			CanManage:   identity.Admin && userID != identity.UserID,
		}
	}

	rows := make([]adminUserRowVM, 0)
	switch {
	case keyword != "":
		like := "%" + keyword + "%"
		found, err := s.queries().SearchUsers(ctx, dbgen.SearchUsersParams{
			KwUser: like, KwNick: like, KwSchool: like, KwGroup: like, KwPhone: like, KwIp: like,
		})
		if err != nil {
			s.log.Error("搜索用户失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		for _, u := range found {
			rows = append(rows, add(u.UserID, u.Nick, u.Email, u.Accesstime, u.RegTime, u.ExpiryDate,
				u.Ip, u.School, u.GroupName, u.ParentPhone, u.Defunct, u.CoinEarned, u.CoinBonus, u.CoinSpent))
		}
	case trash:
		found, err := s.queries().ListUsersTrash(ctx, dbgen.ListUsersTrashParams{Limit: usersPerPage, Offset: int32((page - 1) * usersPerPage)})
		if err != nil {
			s.log.Error("查询已删除用户失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		for _, u := range found {
			rows = append(rows, add(u.UserID, u.Nick, u.Email, u.Accesstime, u.RegTime, u.ExpiryDate,
				u.Ip, u.School, u.GroupName, u.ParentPhone, u.Defunct, u.CoinEarned, u.CoinBonus, u.CoinSpent))
		}
	default:
		found, err := s.queries().ListUsersActive(ctx, dbgen.ListUsersActiveParams{Limit: usersPerPage, Offset: int32((page - 1) * usersPerPage)})
		if err != nil {
			s.log.Error("查询用户列表失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		for _, u := range found {
			rows = append(rows, add(u.UserID, u.Nick, u.Email, u.Accesstime, u.RegTime, u.ExpiryDate,
				u.Ip, u.School, u.GroupName, u.ParentPhone, u.Defunct, u.CoinEarned, u.CoinBonus, u.CoinSpent))
		}
	}

	vm := adminUserListVM{
		Rows:    rows,
		Keyword: keyword,
		TrashSuffix: func() string {
			if trash {
				return "&trash"
			}
			return ""
		}(),
		Page:       page,
		TotalPages: pages,
		Token:      r,
	}
	if keyword == "" {
		vm.ShowPagination = true
		vm.PrevPage = page
		if page > 1 {
			vm.PrevPage = page - 1
		}
		vm.NextPage = page
		if page < pages {
			vm.NextPage = page + 1
		}
		const pagesPerFrame = 5
		frame := (page-1)/pagesPerFrame + 1
		spage := (frame-1)*pagesPerFrame + 1
		epage := spage + pagesPerFrame - 1
		if epage > pages {
			epage = pages
		}
		for i := spage; i <= epage; i++ {
			vm.Pages = append(vm.Pages, adminNewsLinkVM{Num: i, Active: i == page})
		}
	}

	s.renderPage(w, r, http.StatusOK, "adminUserListPage", "用户列表", vm)
}

func (s *Server) registerAdminAjaxRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /admin/ajax.php", s.handleAdminAjax)
}
