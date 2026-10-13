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

// ── admin/problem_batch_delete.php ───────────────────────────────────

// ProblemBatchDeleteResultVM 是批量删除题目结果页的视图模型。
type ProblemBatchDeleteResultVM struct {
	Title   string
	OJName  string
	Count   int
	Success bool
}

func (s *Server) handleProblemBatchDelete(w http.ResponseWriter, r *http.Request) {
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

	ids := make(map[int32]bool)
	for _, raw := range r.Form["pid"] {
		if id := int32(parseInt(raw)); id > 0 {
			ids[id] = true
		}
	}
	if hlist := r.FormValue("hlist"); hlist != "" {
		for _, raw := range strings.Split(hlist, ",") {
			if id := int32(parseInt(raw)); id > 0 {
				ids[id] = true
			}
		}
	}

	if len(ids) == 0 {
		http.Redirect(w, r, "/admin/problem_list.php", http.StatusFound)
		return
	}

	idList := make([]int32, 0, len(ids))
	for id := range ids {
		idList = append(idList, id)
	}
	if len(idList) > 200 {
		idList = idList[:200]
	}

	deleted := 0
	for _, id := range idList {
		var exists int32
		err := s.db.QueryRowContext(ctx, "SELECT problem_id FROM problem WHERE problem_id=? LIMIT 1", id).Scan(&exists)
		if err != nil {
			continue
		}

		dataDir := filepath.Join(s.cfg.OJHome, "data", strconv.Itoa(int(id)))
		if info, err := os.Stat(dataDir); err == nil && info.IsDir() {
			_ = os.RemoveAll(dataDir)
		}

		_ = s.queries().DeleteContestProblemByPID(ctx, id)
		_ = s.queries().DeleteProblemByID(ctx, id)
		_ = s.queries().DeleteProblemPrivilege(ctx, "p"+strconv.Itoa(int(id)))
		_ = s.queries().UpdateSolutionProblemToZero(ctx, id)

		var gone int32
		err = s.db.QueryRowContext(ctx, "SELECT problem_id FROM problem WHERE problem_id=? LIMIT 1", id).Scan(&gone)
		if err != nil {
			deleted++
		}
	}

	maxRow := s.db.QueryRowContext(ctx, "SELECT MAX(problem_id) FROM problem")
	var maxID sql.NullInt32
	_ = maxRow.Scan(&maxID)
	nextID := int32(1000)
	if maxID.Valid && maxID.Int32 >= 1000 {
		nextID = maxID.Int32 + 1
	}
	_, _ = s.db.ExecContext(ctx, "ALTER TABLE problem AUTO_INCREMENT = ?", nextID)

	renderPage(w, http.StatusOK, "problemBatchDeleteResultPage", "批量删除题目", ProblemBatchDeleteResultVM{
		Title: "批量删除题目", OJName: s.cfg.OJName, Count: deleted, Success: true,
	})
}

func (s *Server) registerAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/index.php", s.handleAdminIndex)
	mux.HandleFunc("GET /admin/menu.php", s.handleAdminMenu)
	mux.HandleFunc("GET /admin/admin-header.php", s.handleAdminHeader)
	mux.HandleFunc("/admin/problem_batch_delete.php", s.handleProblemBatchDelete)
}
