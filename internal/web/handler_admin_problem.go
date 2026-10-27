// 后台题目管理（admin/problem_list.php、problem_df_change.php、problem_del.php、
// problem_changeid.php）。
//
// 权限：
//   - problem_list / problem_df_change：administrator 或 problem_editor
//     （problem_list 还含 contest_creator）。Identity 不建模 tag_adder，故不涉及。
//   - problem_del / problem_changeid：administrator（del 另允许 p<id> 授权，Go 未建模 p<id>）。
//
// 数据目录：PHP 用 $OJ_DATA/<id>，对应 Go 的 <OJHome>/data/<id>。

package web

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

const problemsPerPageAdmin = 50

type adminSourceTagVM struct {
	Label string
	Theme string
	Href  string
	Title string
}

type adminProblemRowVM struct {
	ProblemID   int32
	Title       string
	Accepted    int64
	InDate      string
	Defunct     string
	Coin        uint32
	HasRemote   bool
	RemoteLabel string
	RemoteHref  string
	Tags        []adminSourceTagVM
}

type adminProblemListVM struct {
	Rows           []adminProblemRowVM
	Keyword        string
	Page           int
	TotalPages     int
	Pages          []adminNewsLinkVM
	ShowPagination bool
	PrevPage       int
	NextPage       int
	CanEdit        bool
	Token          *http.Request
}

func (s *Server) problemDataDir(id int32) string {
	return filepath.Join(s.cfg.OJHome, "data", strconv.Itoa(int(id)))
}

// buildProblemListTags 复刻 problem_list.php 的来源标签：http 开头显示 Remote，
// 其余走主题色标签（md5 取模，与 problemset 同一套 colorTheme）。
func buildProblemListTags(source string) ([]adminSourceTagVM, bool) {
	tags := make([]adminSourceTagVM, 0)
	hasRemote := false
	for _, cat := range SplitSource(source) {
		cat = strings.TrimSpace(cat)
		if cat == "" || cat == "&nbsp" {
			continue
		}
		if strings.HasPrefix(cat, "http") {
			hasRemote = true
			continue
		}
		tags = append(tags, adminSourceTagVM{
			Label: truncateLabel(cat, 10),
			Theme: themeFor(cat),
			Href:  "problem_list.php?keyword=" + urlEncode(cat),
			Title: cat,
		})
	}
	return tags, hasRemote
}

func (s *Server) handleAdminProblemList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !(identity.Admin || identity.ProblemEditor || identity.ContestCreator) {
		s.adminLoginRequired(w)
		return
	}
	canEdit := identity.Admin || identity.ProblemEditor

	total, err := s.queries().CountProblemsAll(ctx)
	if err != nil {
		s.log.Error("统计题目失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}
	pages := int((total + problemsPerPageAdmin - 1) / problemsPerPageAdmin)
	page := parsePage(r.URL.Query().Get("page"))
	keyword := r.URL.Query().Get("keyword")

	add := func(id int32, title string, accepted sql.NullInt32, inDate sql.NullTime,
		defunct string, source, remoteOJ, remoteID sql.NullString, coin uint32) adminProblemRowVM {
		tags, hasRemoteTag := buildProblemListTags(source.String)
		row := adminProblemRowVM{
			ProblemID: id, Title: title, Accepted: int64(accepted.Int32),
			Defunct: defunct, Coin: coin, Tags: tags,
		}
		if inDate.Valid {
			row.InDate = inDate.Time.Format("2006-01-02 15:04:05")
		}
		if remoteOJ.Valid && remoteOJ.String != "" {
			row.HasRemote = true
			row.RemoteLabel = remoteOJ.String + remoteID.String
			row.RemoteHref = source.String
		} else if hasRemoteTag {
			row.HasRemote = true
			row.RemoteLabel = "Remote"
			row.RemoteHref = source.String
		}
		return row
	}

	rows := make([]adminProblemRowVM, 0)
	if keyword != "" {
		like := "%" + keyword + "%"
		found, err := s.queries().SearchProblemsAdminList(ctx, dbgen.SearchProblemsAdminListParams{
			KwPid:   like,
			KwTitle: like,
			KwDesc:  like,
			KwSrc:   sql.NullString{String: like, Valid: true},
			KwHint:  like,
		})
		if err != nil {
			s.log.Error("搜索题目失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		for _, p := range found {
			rows = append(rows, add(p.ProblemID, p.Title, p.Accepted, p.InDate, p.Defunct, p.Source, p.RemoteOj, p.RemoteID, p.Coin))
		}
	} else {
		found, err := s.queries().ListProblemsAdminList(ctx, dbgen.ListProblemsAdminListParams{
			Limit:  problemsPerPageAdmin,
			Offset: int32((page - 1) * problemsPerPageAdmin),
		})
		if err != nil {
			s.log.Error("查询题目列表失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		for _, p := range found {
			rows = append(rows, add(p.ProblemID, p.Title, p.Accepted, p.InDate, p.Defunct, p.Source, p.RemoteOj, p.RemoteID, p.Coin))
		}
	}

	vm := adminProblemListVM{
		Rows: rows, Keyword: keyword, Page: page, TotalPages: pages, CanEdit: canEdit, Token: r,
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
		const pagesPerFrame = 25
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

	s.renderPage(w, r, http.StatusOK, "adminProblemListPage", "题目列表", vm)
}

// ── admin/problem_df_change.php ──────────────────────────────────────

func (s *Server) handleAdminProblemDefunctChange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !(identity.Admin || identity.ProblemEditor) {
		s.adminLoginRequired(w)
		return
	}

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		ids := parseProblemIDs(r.Form["pid"], r.FormValue("hlist"))
		if len(ids) > 0 {
			_, enable := r.Form["enable"]
			_, disable := r.Form["disable"]
			defunct := ""
			if enable {
				defunct = "N"
			} else if disable {
				defunct = "Y"
			}
			if defunct != "" {
				for _, id := range ids {
					_ = s.queries().SetProblemDefunctByID(ctx, dbgen.SetProblemDefunctByIDParams{Defunct: defunct, ProblemID: id})
				}
			}
		}
		http.Redirect(w, r, "/admin/problem_list.php", http.StatusFound)
		return
	}

	id := int32(parseInt(r.URL.Query().Get("id")))
	defunct, err := s.queries().GetProblemDefunct(ctx, id)
	if err != nil {
		http.Redirect(w, r, "/admin/problem_list.php", http.StatusFound)
		return
	}
	// PHP 仅对 administrator 或 p<id> 授权才真正写库；此处用 administrator 判断。
	if identity.Admin || identity.ProblemEditor {
		next := "Y"
		if defunct == "Y" {
			next = "N"
		}
		_ = s.queries().SetProblemDefunctByID(ctx, dbgen.SetProblemDefunctByIDParams{Defunct: next, ProblemID: id})
	}
	http.Redirect(w, r, "/admin/problem_list.php", http.StatusFound)
}

// parseProblemIDs 复刻 problem_df_change.php 的 pid[]/hlist 解析：intval、去重、
// 丢弃非正数。
func parseProblemIDs(pid []string, hlist string) []int32 {
	seen := make(map[int32]bool)
	out := make([]int32, 0)
	add := func(v int32) {
		if v > 0 && !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	for _, p := range pid {
		add(int32(parseInt(p)))
	}
	for _, p := range strings.Split(hlist, ",") {
		add(int32(parseInt(p)))
	}
	return out
}

// ── admin/problem_del.php ────────────────────────────────────────────

func (s *Server) handleAdminProblemDel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}

	id := int32(parseInt(r.URL.Query().Get("id")))
	if id <= 0 {
		http.Redirect(w, r, "/admin/problem_list.php", http.StatusFound)
		return
	}

	_ = os.RemoveAll(s.problemDataDir(id))
	_ = s.queries().DeleteProblemByID(ctx, id)
	_ = s.queries().DeleteProblemPrivilege(ctx, "p"+strconv.Itoa(int(id)))
	_ = s.queries().UpdateSolutionProblemToZero(ctx, id)
	s.resetProblemAutoIncrement(ctx)

	http.Redirect(w, r, "/admin/problem_list.php", http.StatusFound)
}

// resetProblemAutoIncrement 复刻 PHP 的 `ALTER TABLE problem AUTO_INCREMENT = max+1`。
func (s *Server) resetProblemAutoIncrement(ctx context.Context) {
	if s.db == nil {
		return
	}
	maxVal, err := s.queries().GetMaxProblemID(ctx)
	if err != nil {
		return
	}
	maxID := int64(0)
	switch v := maxVal.(type) {
	case int64:
		maxID = v
	case int32:
		maxID = int64(v)
	case uint32:
		maxID = int64(v)
	case []byte:
		if n, err := strconv.ParseInt(string(v), 10, 64); err == nil {
			maxID = n
		}
	}
	next := maxID + 1
	if next < 1000 {
		next = 1000
	}
	_, _ = s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE problem AUTO_INCREMENT = %d", next))
}

// ── admin/problem_changeid.php ───────────────────────────────────────

type adminProblemChangeIDVM struct {
	Message  string
	ShowForm bool
	Token    *http.Request
}

func (s *Server) handleAdminProblemChangeID(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}

	vm := adminProblemChangeIDVM{Token: r, ShowForm: dataDirWritable(s.cfg.OJHome)}
	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		from := int32(parseInt(r.FormValue("from")))
		to := int32(parseInt(r.FormValue("to")))
		vm.Message = s.changeProblemID(ctx, from, to)
	}

	s.renderPage(w, r, http.StatusOK, "adminProblemChangeIDPage", "修改题号", vm)
}

// changeProblemID 把题号 from 改成 to：先挪数据目录，再改库，失败回滚目录。
func (s *Server) changeProblemID(ctx context.Context, from, to int32) string {
	if from <= 0 || to <= 0 {
		return "fail..."
	}
	n, err := s.queries().ProblemIDExists(ctx, to)
	if err != nil {
		return "fail..."
	}
	if n != 0 {
		return "fail..."
	}
	if s.db == nil {
		return "fail..."
	}
	fromDir := s.problemDataDir(from)
	toDir := s.problemDataDir(to)
	if err := os.Rename(fromDir, toDir); err != nil {
		return "fail..."
	}
	affected, err := execRows(s.db, ctx,
		"UPDATE `problem` SET `problem_id`=? WHERE `problem_id`=?", to, from)
	if err != nil || affected == 0 {
		_ = os.Rename(toDir, fromDir)
		return "fail..."
	}
	_ = s.queries().UpdateSolutionProblemID(ctx, dbgen.UpdateSolutionProblemIDParams{NewID: to, OldID: from})
	_ = s.queries().UpdateContestProblemProblemID(ctx, dbgen.UpdateContestProblemProblemIDParams{NewID: to, OldID: from})
	_ = s.queries().UpdateTopicPID(ctx, dbgen.UpdateTopicPIDParams{NewID: to, OldID: from})
	s.resetProblemAutoIncrement(ctx)
	return "done!"
}

// dataDirWritable 对应 problem_changeid.php 的 writable($OJ_DATA)：尝试创建/删除探测文件。
func dataDirWritable(ojHome string) bool {
	dir := filepath.Join(ojHome, "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	probe := filepath.Join(dir, "testifwritable.tst")
	fp, err := os.Create(probe)
	if err != nil {
		return false
	}
	fp.Close()
	_ = os.Remove(probe)
	return true
}

func (s *Server) registerAdminProblemRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/problem_list.php", s.handleAdminProblemList)
	mux.HandleFunc("GET /admin/problem_df_change.php", s.handleAdminProblemDefunctChange)
	mux.HandleFunc("POST /admin/problem_df_change.php", s.handleAdminProblemDefunctChange)
	mux.HandleFunc("GET /admin/problem_del.php", s.handleAdminProblemDel)
	mux.HandleFunc("GET /admin/problem_changeid.php", s.handleAdminProblemChangeID)
	mux.HandleFunc("POST /admin/problem_changeid.php", s.handleAdminProblemChangeID)
}

// execRows 执行一条语句并返回受影响行数。
func execRows(db *sql.DB, ctx context.Context, query string, args ...any) (int64, error) {
	res, err := db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}
