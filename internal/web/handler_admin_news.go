// 后台公告管理（admin/news_list.php、news_add_page.php、news_add.php、
// news_edit.php、news_df_change.php）。
//
// 权限：这几个页面 PHP 一律要求 administrator（比 admin-header.php 的
// administrator/contest_creator/user_adder/problem_editor/password_setter
// 更窄），因此这里统一用 identity.Admin，不用 hasAdminPrivilege。
//
// CSRF：迁移期 Go 用 nosurf 全局中间件（wrapCSRF），只拦不安全方法。
// PHP 侧的 set_post_key/check_post_key/getkey 旧式令牌不复刻 —— 表单里
// 统一嵌入 nosurf 的 csrf_token（见 csrfInput 子模板）。
//
// setmsg.php（基于文件 msg/<host>.txt 的公告）依赖请求 Host 与 RemoveXSS，
// 本批次不迁移，仍由 PHP 承担。

package web

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

const newsPerPage = 25

// adminNewsRowVM 是列表页一行。
type adminNewsRowVM struct {
	NewsID     int32
	Title      string // 空标题在 PHP 里显示为 "Empty"
	Time       string
	On         bool // defunct=='N'
	Importance int8
	MenuYes    bool
}

// adminNewsLinkVM 是分页里的一个页码。
type adminNewsLinkVM struct {
	Num    int
	Active bool
}

// adminNewsListVM 是列表页视图模型。
type adminNewsListVM struct {
	Rows           []adminNewsRowVM
	Keyword        string
	Page           int
	TotalPages     int
	Pages          []adminNewsLinkVM
	ShowPagination bool
	PrevPage       int
	NextPage       int
}

// adminNewsAddVM 是新增页视图模型。
type adminNewsAddVM struct {
	Title   string
	Content string
	Token   *http.Request
}

// adminNewsEditVM 是编辑页视图模型。
type adminNewsEditVM struct {
	NewsID     int32
	Title      string
	Content    string
	ShowInMenu bool
	Token      *http.Request
}

// newsMenuValue 复刻 `$showInMenu == "on" ? 1 : 0`。
func newsMenuValue(showInMenu string) int32 {
	if showInMenu == "on" {
		return 1
	}
	return 0
}

// newsTitleDisplay 复刻 `$row['title']=="" ? "Empty" : $row['title']`。
func newsTitleDisplay(title string) string {
	if title == "" {
		return "Empty"
	}
	return title
}

// ── admin/news_list.php ──────────────────────────────────────────────

func (s *Server) handleAdminNewsList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}

	total, err := s.queries().CountNewsAdmin(ctx)
	if err != nil {
		s.log.Error("统计公告数失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	pages := int(total) / newsPerPage
	if int(total)%newsPerPage != 0 {
		pages++
	}
	page := parsePage(r.URL.Query().Get("page"))

	keyword := r.URL.Query().Get("keyword")
	var rows []adminNewsRowVM
	if keyword != "" {
		// PHP：带关键词时不加 LIMIT，全量返回。
		like := "%" + keyword + "%"
		found, err := s.queries().SearchNewsAdmin(ctx, dbgen.SearchNewsAdminParams{
			TitleLike:   like,
			ContentLike: like,
		})
		if err != nil {
			s.log.Error("搜索公告失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		rows = make([]adminNewsRowVM, 0, len(found))
		for _, row := range found {
			rows = append(rows, buildAdminNewsRow(row.NewsID, row.Title, row.Time, row.Defunct, row.Importance, row.Menu))
		}
	} else {
		offset := (page - 1) * newsPerPage
		found, err := s.queries().ListNewsAdmin(ctx, dbgen.ListNewsAdminParams{
			Limit:  newsPerPage,
			Offset: int32(offset),
		})
		if err != nil {
			s.log.Error("查询公告列表失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		rows = make([]adminNewsRowVM, 0, len(found))
		for _, row := range found {
			rows = append(rows, buildAdminNewsRow(row.NewsID, row.Title, row.Time, row.Defunct, row.Importance, row.Menu))
		}
	}

	vm := adminNewsListVM{
		Rows:       rows,
		Keyword:    keyword,
		Page:       page,
		TotalPages: pages,
	}
	// PHP：只在无关键词时渲染分页。
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
		// frame = ceil(page/5)，每帧 5 个页码。
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

	s.renderPage(w, r, http.StatusOK, "adminNewsListPage", "新闻管理", vm)
}

// buildAdminNewsRow 把列表/搜索两条查询的行摊平成视图模型。
func buildAdminNewsRow(newsID int32, title string, t time.Time, defunct string, importance int8, menu int32) adminNewsRowVM {
	return adminNewsRowVM{
		NewsID:     newsID,
		Title:      newsTitleDisplay(title),
		Time:       t.Format("2006-01-02 15:04:05"),
		On:         defunct != "Y",
		Importance: importance,
		MenuYes:    menu == 1,
	}
}

// ── admin/news_add_page.php ──────────────────────────────────────────

func (s *Server) handleAdminNewsAddPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}

	vm := adminNewsAddVM{Token: r}
	// ?cid= 时载入原公告，标题回填 "<原标题>-Copy"（news_add_page.php）。
	if raw := r.URL.Query().Get("cid"); raw != "" {
		row, err := s.queries().GetNewsAdmin(ctx, int32(parseInt(raw)))
		if err == nil {
			vm.Title = row.Title + "-Copy"
			vm.Content = row.Content
		} else if !errors.Is(err, sql.ErrNoRows) {
			s.log.Error("读取待复制公告失败", "err", err)
		}
	}

	s.renderPage(w, r, http.StatusOK, "adminNewsAddPage", "添加新闻", vm)
}

// ── admin/news_add.php ───────────────────────────────────────────────

func (s *Server) handleAdminNewsAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	err := s.queries().InsertNews(ctx, dbgen.InsertNewsParams{
		UserID:  identity.UserID,
		Title:   r.FormValue("title"),
		Content: r.FormValue("content"),
		Menu:    newsMenuValue(r.FormValue("showInMenu")),
	})
	if err != nil {
		s.log.Error("保存公告失败", "err", err)
		http.Error(w, "保存失败", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/admin/news_list.php", http.StatusFound)
}

// ── admin/news_edit.php ──────────────────────────────────────────────

func (s *Server) handleAdminNewsEdit(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad Request", http.StatusBadRequest)
			return
		}
		err := s.queries().UpdateNews(ctx, dbgen.UpdateNewsParams{
			Title:   r.FormValue("title"),
			Content: r.FormValue("content"),
			UserID:  identity.UserID,
			Menu:    newsMenuValue(r.FormValue("showInMenu")),
			NewsID:  int32(parseInt(r.FormValue("news_id"))),
		})
		if err != nil {
			s.log.Error("更新公告失败", "err", err)
			http.Error(w, "保存失败", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/admin/news_list.php", http.StatusFound)
		return
	}

	// GET：按 id 载入，不存在时与 PHP 一致地拒绝。
	id := int32(parseInt(r.URL.Query().Get("id")))
	row, err := s.queries().GetNewsAdmin(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		s.renderPage(w, r, http.StatusOK, "errorPage", "无此公告", map[string]string{
			"Message": "No such News!",
		})
		return
	}
	if err != nil {
		s.log.Error("读取公告失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	s.renderPage(w, r, http.StatusOK, "adminNewsEditPage", "编辑新闻", adminNewsEditVM{
		NewsID:     row.NewsID,
		Title:      row.Title,
		Content:    row.Content,
		ShowInMenu: row.Menu == 1,
		Token:      r,
	})
}

// ── admin/news_df_change.php ─────────────────────────────────────────

func (s *Server) handleAdminNewsDefunctChange(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identity := s.resolveIdentity(w, r)
	if !identity.Admin {
		s.adminLoginRequired(w)
		return
	}

	id := int32(parseInt(r.URL.Query().Get("id")))
	row, err := s.queries().GetNewsAdmin(ctx, id)
	if err != nil {
		// PHP 找不到行时会访问未定义的 $result[0]，这里直接忽略返回列表。
		http.Redirect(w, r, "/admin/news_list.php", http.StatusFound)
		return
	}
	next := "Y"
	if row.Defunct == "Y" {
		next = "N"
	}
	if err := s.queries().SetNewsDefunct(ctx, dbgen.SetNewsDefunctParams{
		Defunct: next,
		NewsID:  id,
	}); err != nil {
		s.log.Error("切换公告状态失败", "err", err)
		http.Error(w, "保存失败", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin/news_list.php", http.StatusFound)
}

// adminLoginRequired 渲染与其他后台页一致的需要登录提示。
func (s *Server) adminLoginRequired(w http.ResponseWriter) {
	s.renderPage(w, nil, http.StatusOK, "errorPage", "需要登录", map[string]string{
		"Message": "<a href=\"/loginpage.php\">Please Login First!</a>",
	})
}

func (s *Server) registerAdminNewsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/news_list.php", s.handleAdminNewsList)
	mux.HandleFunc("GET /admin/news_add_page.php", s.handleAdminNewsAddPage)
	mux.HandleFunc("POST /admin/news_add.php", s.handleAdminNewsAdd)
	mux.HandleFunc("GET /admin/news_edit.php", s.handleAdminNewsEdit)
	mux.HandleFunc("POST /admin/news_edit.php", s.handleAdminNewsEdit)
	mux.HandleFunc("GET /admin/news_df_change.php", s.handleAdminNewsDefunctChange)
}
