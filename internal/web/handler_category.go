// category.php：题目来源分类标签页。
//
// 逐行对应 phpweb/category.php：
//  1. 取所有 defunct='N' 题目的 source（DISTINCT，按 source 排序，LIMIT 5000）；
//  2. 每条 source 按空格切成标签；以 http 开头的压成域名（get_domain）；
//  3. 去重 + 排序后渲染成彩色标签，点击跳 problemset.php?search=<标签>。
//
// 配色沿用 view.go 的 themeFor（与 Go 版 problemset 的来源标签一致）。

package web

import (
	"net/http"
	"sort"
	"strings"
)

// categorySourceLimit 对应 category.php 的 LIMIT 5000。
const categorySourceLimit = 5000

// CategoryItem 是一个来源分类标签。
type CategoryItem struct {
	Label string
	Href  string
	Theme string
}

// CategoryPageVM 是来源分类页的视图模型。
type CategoryPageVM struct {
	Title      string
	OJName     string
	Categories []CategoryItem
}

func (s *Server) registerCategoryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /category.php", s.handleCategory)
}

func (s *Server) handleCategory(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.QueryContext(r.Context(),
		`SELECT DISTINCT COALESCE(source, '') FROM problem WHERE defunct = 'N' ORDER BY source LIMIT ?`,
		categorySourceLimit)
	if err != nil {
		s.log.Error("查询题目来源失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var sources []string
	for rows.Next() {
		var src string
		if err := rows.Scan(&src); err != nil {
			s.log.Error("读取题目来源失败", "err", err)
			http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
			return
		}
		sources = append(sources, src)
	}
	if err := rows.Err(); err != nil {
		s.log.Error("遍历题目来源失败", "err", err)
		http.Error(w, "页面暂时不可用", http.StatusInternalServerError)
		return
	}

	s.renderPage(w, r, http.StatusOK, "categoryPage", "来源/分类", CategoryPageVM{
		Title:      "来源/分类",
		OJName:     s.cfg.OJName,
		Categories: buildCategories(sources),
	})
}

// buildCategories 把 source 列表摊成去重、排序后的标签列表。
//
// 对应 category.php 的 explode(" ") + get_domain + array_unique + sort：
// 标签按空格切分，http 开头的压成域名，去掉空白标签，去重后按字节序排序，
// 并预先算好链接与配色。
func buildCategories(sources []string) []CategoryItem {
	seen := make(map[string]struct{})
	var cats []string
	for _, src := range sources {
		for _, cat := range SplitSource(src) {
			if strings.HasPrefix(cat, "http") {
				cat = domainOf(cat)
			}
			if _, ok := seen[cat]; ok {
				continue
			}
			seen[cat] = struct{}{}
			cats = append(cats, cat)
		}
	}

	sort.Strings(cats)

	items := make([]CategoryItem, 0, len(cats))
	for _, cat := range cats {
		if strings.TrimSpace(cat) == "" {
			continue
		}
		items = append(items, CategoryItem{
			Label: cat,
			Href:  "problemset.php?search=" + urlEncode(cat),
			Theme: themeFor(cat),
		})
	}
	return items
}
