// 系统题单的独立展示页（/problem_lists.php）。
//
// 题单数据并不单独落表：PHP 侧的"创建新题单"会把勾选的题号写进一条
// news 的 content（`[plist=1,2,3]题单名[/plist]`），这里按同样的约定
// 把所有未停用的此类公告解析成题单卡片，让用户不必先打开某个人的档案页
// 就能浏览全部系统题单，点击题号进入题目。登录用户按其已 AC 的题标绿。
package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// problemListEntry 一个系统题单卡片。
type problemListEntry struct {
	Name     string
	Problems []problemListProblem
}

// problemListProblem 是题单里的一道题。
type problemListProblem struct {
	ProblemID int32
	Title     string
	Passed    bool
}

// problemListsPageVM 是 /problem_lists.php 的视图模型。
type problemListsPageVM struct {
	Lists    []problemListEntry
	LoggedIn bool
	User     string
}

// handleProblemLists 渲染系统题单页。
func (s *Server) handleProblemLists(w http.ResponseWriter, r *http.Request) {
	id := s.resolveIdentity(w, r)
	q := s.queries()
	ctx := r.Context()

	passed := map[int32]bool{}
	if id.LoggedIn {
		ids, err := q.ListViewerACProblems(ctx, id.UserID, []int32{})
		if err != nil {
			s.log.Error("查询已通过题号失败", "err", err)
		}
		for _, pid := range ids {
			passed[pid] = true
		}
	}

	lists, err := loadSystemProblemLists(ctx, q, passed)
	if err != nil {
		s.log.Error("查询系统题单失败", "err", err)
		http.Error(w, "题单暂时不可用", http.StatusInternalServerError)
		return
	}

	s.renderPage(w, r, http.StatusOK, "problemListsPage", "系统题单", problemListsPageVM{
		Lists:    lists,
		LoggedIn: id.LoggedIn,
		User:     id.UserID,
	})
}

// loadSystemProblemLists 从 news 的 plist 块解析出系统题单列表。
func loadSystemProblemLists(ctx context.Context, q dbgen.FullQuerier, passed map[int32]bool) ([]problemListEntry, error) {
	newsRows, err := q.ListNewsWithPlist(ctx)
	if err != nil {
		return nil, err
	}
	lists := make([]problemListEntry, 0)
	for _, content := range newsRows {
		for _, m := range systemPlistRe.FindAllStringSubmatch(content, -1) {
			seen := map[int32]bool{}
			entry := problemListEntry{Name: strings.TrimSpace(m[2])}
			for _, raw := range strings.Split(m[1], ",") {
				pid := int32(parseInt(strings.TrimSpace(raw)))
				if pid <= 0 || seen[pid] {
					continue
				}
				seen[pid] = true
				title := ""
				if p, err := q.GetProblemByID(ctx, pid); err == nil {
					title = p.Title
				}
				entry.Problems = append(entry.Problems, problemListProblem{
					ProblemID: pid,
					Title:     title,
					Passed:    passed[pid],
				})
			}
			if len(entry.Problems) > 0 {
				lists = append(lists, entry)
			}
		}
	}
	return lists, nil
}

// registerProblemListsRoutes 挂载题单页路由。
func (s *Server) registerProblemListsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /problem_lists.php", s.handleProblemLists)
}