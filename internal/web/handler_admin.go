// B1 后台骨架（admin/index.php、admin/menu.php、admin/admin-header.php）

package web

import (
	"net/http"
)

// AdminIndexVM 是后台首页的视图模型。
type AdminIndexVM struct {
	Title  string
	OJName string
}

// AdminMenuVM 是后台菜单的视图模型。
type AdminMenuVM struct {
	Title            string
	OJName           string
	IsAdmin          bool
	IsContestCreator bool
	IsProblemEditor  bool
	IsUserAdder      bool
	IsPasswordSetter bool
}

// AdminHeaderVM 是后台头部的视图模型。
type AdminHeaderVM struct {
	Title  string
	OJName string
}

func (s *Server) handleAdminIndex(w http.ResponseWriter, r *http.Request) {
	identity := s.resolveIdentity(w, r)
	if !s.hasAdminPrivilege(identity) {
		renderPage(w, http.StatusOK, "errorPage", "需要登录", map[string]string{
			"Message": "<a href=\"/loginpage.php\">Please Login First!</a>",
		})
		return
	}
	renderPage(w, http.StatusOK, "adminIndexPage", "后台管理", AdminIndexVM{
		Title: "后台管理", OJName: s.cfg.OJName,
	})
}

func (s *Server) handleAdminMenu(w http.ResponseWriter, r *http.Request) {
	identity := s.resolveIdentity(w, r)
	if !s.hasAdminPrivilege(identity) {
		renderPage(w, http.StatusOK, "errorPage", "需要登录", map[string]string{
			"Message": "<a href=\"/loginpage.php\">Please Login First!</a>",
		})
		return
	}
	renderPage(w, http.StatusOK, "adminMenuPage", "后台管理", AdminMenuVM{
		Title: "后台管理", OJName: s.cfg.OJName,
		IsAdmin:          identity.Admin,
		IsContestCreator: identity.ContestCreator,
		IsProblemEditor:  identity.ProblemEditor,
		IsUserAdder:      false,
		IsPasswordSetter: false,
	})
}

func (s *Server) handleAdminHeader(w http.ResponseWriter, r *http.Request) {
	identity := s.resolveIdentity(w, r)
	if !s.hasAdminPrivilege(identity) {
		renderPage(w, http.StatusOK, "errorPage", "需要登录", map[string]string{
			"Message": "<a href=\"/loginpage.php\">Please Login First!</a>",
		})
		return
	}
	renderPage(w, http.StatusOK, "adminHeaderPage", "后台管理", AdminHeaderVM{
		Title: "后台管理", OJName: s.cfg.OJName,
	})
}

func (s *Server) hasAdminPrivilege(identity Identity) bool {
	return identity.Admin || identity.ContestCreator || identity.ProblemEditor || identity.SourceBrowser
}

func (s *Server) registerAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/index.php", s.handleAdminIndex)
	mux.HandleFunc("GET /admin/menu.php", s.handleAdminMenu)
	mux.HandleFunc("GET /admin/admin-header.php", s.handleAdminHeader)
}
