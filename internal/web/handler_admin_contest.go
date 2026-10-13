// 后台比赛管理（admin/contest_list.php、contest_df_change.php、contest_pr_change.php、
// contest_add.php、contest_edit.php）。
//
// 权限：administrator 或 contest_creator（Identity 不建模 session 里的 m<cid>）。
// langmask 由 Go 侧算：`((1<<len(languageExt))-1) & ^selected`；contest_type 是复选框
// 1<<i 的按位或。description 做 `<p>` 去除、`</p>→<br />`、`,`→`&#44;`。
// 未复刻：kindeditor、langmask 复制下拉（依赖 session 的 m<cid> 列表）。

package web

import (
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// contestLockNames 对应 const.inc.php 的 $contest_locks（9 项，索引即位号）。
var contestLockNames = []string{
	"来源对考试不可见", "禁止下载", "禁止查看排名", "禁止查看比赛状态",
	"NOIP 锁榜", "禁止查看 diff", "禁止补题", "只保留最后一次提交", "禁止 AI 帮助",
}

// contestLangmask 计算存储用的 langmask（掩码位为 1 表示"禁止使用"）。
func contestLangmask(selected []int) uint32 {
	var sum uint32
	for _, t := range selected {
		if t >= 0 && t < len(languageExt) {
			sum |= 1 << uint(t)
		}
	}
	all := uint32(1)<<uint(len(languageExt)) - 1
	return all & ^sum
}

func nullCID(cid int32) sql.NullInt32 { return sql.NullInt32{Int32: cid, Valid: true} }

func parseContestTime(date, hour, minute string) sql.NullTime {
	d := strings.TrimSpace(date)
	if d == "" {
		return sql.NullTime{}
	}
	h := parseInt(hour)
	m := parseInt(minute)
	s := fmt.Sprintf("%s %02d:%02d:00", d, h, m)
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local)
	if err != nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: t, Valid: true}
}

// contestDescription 复刻 contest_add/edit 的正文预处理。
func contestDescription(s string) string {
	s = strings.ReplaceAll(s, "<p>", "")
	s = strings.ReplaceAll(s, "</p>", "<br />")
	return strings.ReplaceAll(s, ",", "&#44;")
}

type adminContestRowVM struct {
	ContestID int32
	Title     string
	StartTime string
	EndTime   string
	Private   bool
	Defunct   string
}

type adminContestListVM struct {
	Rows           []adminContestRowVM
	Keyword        string
	Page           int
	TotalPages     int
	Pages          []adminNewsLinkVM
	ShowPagination bool
	PrevPage       int
	NextPage       int
}

func (s *Server) handleAdminContestList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !(identity.Admin || identity.ContestCreator) {
		s.adminLoginRequired(w)
		return
	}

	total, err := s.queries().CountContestsAdmin(ctx)
	if err != nil {
		s.log.Error("统计比赛失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}
	pages := int((total + 24) / 25)
	page := parsePage(r.URL.Query().Get("page"))
	keyword := r.URL.Query().Get("keyword")

	add := func(id int32, title sql.NullString, st, et sql.NullTime, private int8, defunct string) adminContestRowVM {
		row := adminContestRowVM{ContestID: id, Title: title.String, Private: private != 0, Defunct: defunct}
		if st.Valid {
			row.StartTime = st.Time.Format("2006-01-02 15:04:05")
		}
		if et.Valid {
			row.EndTime = et.Time.Format("2006-01-02 15:04:05")
		}
		return row
	}

	rows := make([]adminContestRowVM, 0)
	if keyword != "" {
		like := "%" + keyword + "%"
		found, err := s.queries().SearchContestsAdmin(ctx, dbgen.SearchContestsAdminParams{
			KwTitle: sql.NullString{String: like, Valid: true},
			KwDesc:  sql.NullString{String: like, Valid: true},
		})
		if err != nil {
			s.log.Error("搜索比赛失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		for _, c := range found {
			rows = append(rows, add(c.ContestID, c.Title, c.StartTime, c.EndTime, c.Private, c.Defunct))
		}
	} else {
		found, err := s.queries().ListContestsAdmin(ctx, dbgen.ListContestsAdminParams{Limit: 25, Offset: int32((page - 1) * 25)})
		if err != nil {
			s.log.Error("查询比赛失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		for _, c := range found {
			rows = append(rows, add(c.ContestID, c.Title, c.StartTime, c.EndTime, c.Private, c.Defunct))
		}
	}

	vm := adminContestListVM{Rows: rows, Keyword: keyword, Page: page, TotalPages: pages}
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
	renderPage(w, http.StatusOK, "adminContestListPage", "比赛列表", vm)
}

// ── contest_df_change.php / contest_pr_change.php ────────────────────

func (s *Server) handleAdminContestDefunctChange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !(identity.Admin || identity.ContestCreator) {
		s.adminLoginRequired(w)
		return
	}
	cid := int32(parseInt(r.URL.Query().Get("cid")))
	defunct, err := s.queries().GetContestDefunct(ctx, cid)
	if err != nil {
		renderPage(w, http.StatusOK, "errorPage", "无此比赛", map[string]string{"Message": "No Such Contest!"})
		return
	}
	next := "Y"
	if defunct == "Y" {
		next = "N"
	}
	_ = s.queries().SetContestDefunct(ctx, dbgen.SetContestDefunctParams{Defunct: next, ContestID: cid})
	http.Redirect(w, r, "/admin/contest_list.php", http.StatusFound)
}

func (s *Server) handleAdminContestPrivateChange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !(identity.Admin || identity.ContestCreator) {
		s.adminLoginRequired(w)
		return
	}
	cid := int32(parseInt(r.URL.Query().Get("cid")))
	private, err := s.queries().GetContestPrivate(ctx, cid)
	if err != nil {
		renderPage(w, http.StatusOK, "errorPage", "无此比赛", map[string]string{"Message": "No Such Problem!"})
		return
	}
	next := int8(1)
	if private != 0 {
		next = 0
	}
	_ = s.queries().SetContestPrivate(ctx, dbgen.SetContestPrivateParams{Private: next, ContestID: cid})
	http.Redirect(w, r, "/admin/contest_list.php", http.StatusFound)
}

// ── contest_add.php / contest_edit.php ───────────────────────────────

type contestLangVM struct {
	ID       int
	Name     string
	Selected bool
}

type contestLockVM struct {
	Index   int
	Name    string
	Checked bool
}

type adminContestFormVM struct {
	Editing     bool
	CID         int32
	Title       string
	StartDate   string
	SHour       string
	SMinute     string
	EndDate     string
	EHour       string
	EMinute     string
	Password    string
	Subnet      string
	Description string
	PList       string
	UList       string
	Private     int8
	Langs       []contestLangVM
	Locks       []contestLockVM
	Token       *http.Request
}

func (s *Server) buildFormVM(editing bool, cid int32, title string, st, et sql.NullTime, private int8,
	password, subnet, description, plist, ulist string, langmask uint32, contestType int16, defaultTimes bool) adminContestFormVM {
	now := time.Now()
	vm := adminContestFormVM{
		Editing: editing, CID: cid, Title: title, Private: private, Password: password,
		Subnet: subnet, Description: description, PList: plist, UList: ulist,
	}
	if defaultTimes {
		vm.StartDate = now.Format("2006-01-02")
		vm.SHour = now.Format("15")
		vm.SMinute = "00"
		vm.EndDate = now.Format("2006-01-02")
		vm.EHour = strconv.Itoa((now.Hour() + 4) % 24)
		vm.EMinute = "00"
	} else {
		if st.Valid {
			vm.StartDate = st.Time.Format("2006-01-02")
			vm.SHour = st.Time.Format("15")
			vm.SMinute = st.Time.Format("04")
		}
		if et.Valid {
			vm.EndDate = et.Time.Format("2006-01-02")
			vm.EHour = et.Time.Format("15")
			vm.EMinute = et.Time.Format("04")
		}
	}
	for i := 0; i < len(languageExt); i++ {
		if s.cfg.LangMask&(1<<uint(i)) != 0 {
			continue
		}
		vm.Langs = append(vm.Langs, contestLangVM{ID: i, Name: languageName(int32(i)), Selected: langmask&(1<<uint(i)) == 0})
	}
	for i, name := range contestLockNames {
		vm.Locks = append(vm.Locks, contestLockVM{Index: i, Name: name, Checked: contestType&(1<<uint(i)) != 0})
	}
	return vm
}

// parseContestForm 从表单读出公共字段。
func (s *Server) parseContestForm(r *http.Request) (adminContestFormVM, dbgen.InsertContestParams) {
	selected := make([]int, 0)
	for _, v := range r.Form["lang[]"] {
		selected = append(selected, parseInt(v))
	}
	var ctype int16
	for _, v := range r.Form["contest_type[]"] {
		ctype |= int16(1) << uint(parseInt(v))
	}
	p := dbgen.InsertContestParams{
		Title:       sql.NullString{String: r.FormValue("title"), Valid: true},
		StartTime:   parseContestTime(r.FormValue("startdate"), r.FormValue("shour"), r.FormValue("sminute")),
		EndTime:     parseContestTime(r.FormValue("enddate"), r.FormValue("ehour"), r.FormValue("eminute")),
		Private:     int8(parseInt(r.FormValue("private"))),
		Langmask:    contestLangmask(selected),
		Description: sql.NullString{String: contestDescription(r.FormValue("description")), Valid: true},
		Password:    r.FormValue("password"),
		Subnet:      r.FormValue("subnet"),
		ContestType: sql.NullInt16{Int16: ctype, Valid: true},
	}
	return adminContestFormVM{}, p
}

// plistFromPOST 解析 cproblem 逗号列表。
func plistFromPOST(r *http.Request) []int32 {
	seen := map[int32]bool{}
	out := make([]int32, 0)
	for _, raw := range strings.Split(strings.TrimSpace(r.FormValue("cproblem")), ",") {
		id := int32(parseInt(raw))
		if id > 0 && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (s *Server) handleAdminContestAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !(identity.Admin || identity.ContestCreator) {
		s.adminLoginRequired(w)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodPost && r.FormValue("startdate") != "" {
		_, p := s.parseContestForm(r)
		p.UserID = identity.UserID
		res, err := s.queries().InsertContest(ctx, p)
		if err != nil {
			s.log.Error("新增比赛失败", "err", err)
			http.Error(w, "保存失败", http.StatusInternalServerError)
			return
		}
		cid64, _ := res.LastInsertId()
		cid := int32(cid64)

		_ = s.queries().DeleteContestProblems(ctx, nullCID(cid))
		num := int32(0)
		for _, pid := range plistFromPOST(r) {
			if _, err := s.queries().GetProblemByID(ctx, pid); err != nil {
				continue
			}
			_ = s.queries().InsertContestProblem(ctx, dbgen.InsertContestProblemParams{ProblemID: pid, ContestID: sql.NullInt32{Int32: cid, Valid: true}, Num: num})
			_ = s.queries().SetProblemDefunctByID(ctx, dbgen.SetProblemDefunctByIDParams{Defunct: "N", ProblemID: pid})
			num++
		}
		_ = s.queries().DeleteProblemPrivilege(ctx, "c"+strconv.Itoa(int(cid)))
		_ = s.queries().InsertPrivilege(ctx, dbgen.InsertPrivilegeParams{UserID: identity.UserID, Rightstr: "m" + strconv.Itoa(int(cid)), Valuestr: "true"})
		for _, u := range strings.Split(strings.TrimSpace(r.FormValue("ulist")), "\n") {
			if u = strings.TrimSpace(u); u != "" {
				_ = s.queries().InsertPrivilege(ctx, dbgen.InsertPrivilegeParams{UserID: u, Rightstr: "c" + strconv.Itoa(int(cid)), Valuestr: "true"})
			}
		}
		http.Redirect(w, r, "/admin/contest_list.php", http.StatusFound)
		return
	}

	// GET：复制模式（?cid=）或空表单；problem2contest / spid 预填题号。
	var (
		title    string
		private  int8
		langmask uint32
		ctype    = int16(32)
		desc     string
		plist    string
		ulist    string
	)
	var st, et sql.NullTime
	if cid := int32(parseInt(r.URL.Query().Get("cid"))); cid > 0 {
		if c, err := s.queries().GetContestByID(ctx, cid); err == nil {
			title = c.Title.String + "-Copy"
			private = c.Private
			langmask = c.Langmask
			ctype = c.ContestType.Int16
			desc = c.Description.String
			st, et = c.StartTime, c.EndTime
		}
		if ids, err := s.queries().ListContestProblemIDsOrdered(ctx, nullCID(cid)); err == nil {
			parts := make([]string, 0, len(ids))
			for _, id := range ids {
				parts = append(parts, strconv.Itoa(int(id)))
			}
			plist = strings.Join(parts, ",")
		}
		if users, err := s.queries().ListContestUserIDs(ctx, "c"+strconv.Itoa(int(cid))); err == nil {
			ulist = strings.Join(users, "\n")
		}
	} else if r.Method == http.MethodPost && r.FormValue("problem2contest") != "" {
		ids := plistFromPOST(r)
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		parts := make([]string, 0, len(ids))
		for _, id := range ids {
			parts = append(parts, strconv.Itoa(int(id)))
		}
		plist = strings.Join(parts, ",")
	} else if spid := parseInt(r.URL.Query().Get("spid")); spid > 0 {
		// 预填所有 >= spid 的题目（PHP）。
		parts := make([]string, 0)
		for pid := int32(spid); pid < int32(spid)+100; pid++ {
			if _, err := s.queries().GetProblemByID(ctx, pid); err == nil {
				parts = append(parts, strconv.Itoa(int(pid)))
			}
		}
		plist = strings.Join(parts, ",")
	}

	vm := s.buildFormVM(false, 0, title, st, et, private, "", "", desc, plist, ulist, langmask, ctype, true)
	vm.Token = r
	renderPage(w, http.StatusOK, "adminContestFormPage", "添加比赛", vm)
}

func (s *Server) handleAdminContestEdit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !(identity.Admin || identity.ContestCreator) {
		s.adminLoginRequired(w)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	if r.Method == http.MethodPost && r.FormValue("startdate") != "" {
		cid := int32(parseInt(r.FormValue("cid")))
		_, p := s.parseContestForm(r)
		if err := s.queries().UpdateContest(ctx, dbgen.UpdateContestParams{
			Title: p.Title, Description: p.Description, StartTime: p.StartTime, EndTime: p.EndTime,
			Private: p.Private, Langmask: p.Langmask, Password: p.Password, Subnet: p.Subnet,
			ContestType: p.ContestType, ContestID: cid,
		}); err != nil {
			s.log.Error("更新比赛失败", "err", err)
			http.Error(w, "保存失败", http.StatusInternalServerError)
			return
		}
		_ = s.queries().DeleteContestProblems(ctx, nullCID(cid))
		_ = s.queries().ResetSolutionNum(ctx, nullCID(cid))
		pids := plistFromPOST(r)
		if len(pids) > 0 {
			for num, pid := range pids {
				if _, err := s.queries().GetProblemByID(ctx, pid); err != nil {
					continue
				}
				_ = s.queries().InsertContestProblem(ctx, dbgen.InsertContestProblemParams{ProblemID: pid, ContestID: sql.NullInt32{Int32: cid, Valid: true}, Num: int32(num)})
				_ = s.queries().RecomputeContestProblemAccepted(ctx, dbgen.RecomputeContestProblemAcceptedParams{ProblemID: pid, ContestID: nullCID(cid)})
				_ = s.queries().RecomputeContestProblemSubmit(ctx, dbgen.RecomputeContestProblemSubmitParams{ProblemID: pid, ContestID: nullCID(cid)})
				_ = s.queries().SetSolutionNum(ctx, dbgen.SetSolutionNumParams{Num: int8(num), ContestID: nullCID(cid), ProblemID: pid})
				_ = s.queries().SetProblemDefunctByID(ctx, dbgen.SetProblemDefunctByIDParams{Defunct: "N", ProblemID: pid})
			}
		}
		_ = s.queries().DeleteProblemPrivilege(ctx, "c"+strconv.Itoa(int(cid)))
		for _, u := range strings.Split(strings.TrimSpace(r.FormValue("ulist")), "\n") {
			if u = strings.TrimSpace(u); u != "" {
				_ = s.queries().InsertPrivilege(ctx, dbgen.InsertPrivilegeParams{UserID: u, Rightstr: "c" + strconv.Itoa(int(cid)), Valuestr: "true"})
			}
		}
		http.Redirect(w, r, "/admin/contest_list.php", http.StatusFound)
		return
	}

	cid := int32(parseInt(r.URL.Query().Get("cid")))
	c, err := s.queries().GetContestByID(ctx, cid)
	if err != nil {
		renderPage(w, http.StatusOK, "errorPage", "无此比赛", map[string]string{"Message": "No such Contest!"})
		return
	}
	ids, _ := s.queries().ListContestProblemIDsOrdered(ctx, nullCID(cid))
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.Itoa(int(id)))
	}
	users, _ := s.queries().ListContestUserIDs(ctx, "c"+strconv.Itoa(int(cid)))
	password, _ := s.queries().GetContestPassword(ctx, cid)
	vm := s.buildFormVM(true, cid, c.Title.String, c.StartTime, c.EndTime, c.Private, password,
		c.Subnet, c.Description.String, strings.Join(parts, ","), strings.Join(users, "\n"),
		c.Langmask, c.ContestType.Int16, false)
	vm.Token = r
	renderPage(w, http.StatusOK, "adminContestFormPage", "编辑比赛", vm)
}

func (s *Server) registerAdminContestRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/contest_list.php", s.handleAdminContestList)
	mux.HandleFunc("GET /admin/contest_df_change.php", s.handleAdminContestDefunctChange)
	mux.HandleFunc("GET /admin/contest_pr_change.php", s.handleAdminContestPrivateChange)
	mux.HandleFunc("GET /admin/contest_add.php", s.handleAdminContestAdd)
	mux.HandleFunc("POST /admin/contest_add.php", s.handleAdminContestAdd)
	mux.HandleFunc("GET /admin/contest_edit.php", s.handleAdminContestEdit)
	mux.HandleFunc("POST /admin/contest_edit.php", s.handleAdminContestEdit)
}
