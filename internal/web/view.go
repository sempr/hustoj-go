// 视图模型与渲染。
//
// 放在独立文件而非 handler 里，是为了能在不启动 HTTP 的情况下断言
// "某组数据渲染成什么 HTML"。切流事故里最难查的一类就是"页面能打开但
// 少了一列/标签颜色变了"，这类问题必须在纯函数层面能测。
//
// 转义原则：所有进入 HTML 的动态数据一律经模板的 html/template 转义。
// problemset.php 第 2 项把 $row['title'] 直接拼进 HTML（未 htmlentities），
// 属于上游遗留的存储型 XSS；Go 侧一律转义，不复刻该缺陷。
// 当前题库里没有任何含尖括号的标题，所以线上尚未暴露，但补题即可触发。

package web

import (
	"fmt"
	"html/template"
	"net/url"
	"strconv"
	"strings"
)

// colorTheme 对应 problemset.php 的 $color_theme，用于来源标签配色。
// 顺序参与取模，改动会让所有标签颜色变化，必须与 PHP 保持一致。
var colorTheme = []string{"success", "warning", "primary", "info", "danger", "default"}

// md5 的用途仅是"把任意标签名稳定映射到固定颜色"，
// 与安全无关；沿用 PHP 的 md5(substr(...,0,7)) 以保证配色一致。
func themeFor(cat string) string {
	sum := md5Hex(cat)
	// PHP: hexdec(substr(md5($cat), 0, 7)) % count($color_theme)
	// substr(...,0,7) 取 7 个十六进制位，最大 0xFFFFFFF = 268435455，
	// 用 uint32 足够，不会溢出。
	v := parseHexPrefix(sum, 7) % uint32(len(colorTheme))
	t := colorTheme[v]
	if t == "" {
		return "default"
	}
	return t
}

// truncateLabel 对应 PHP 的 mb_substr($cat, 0, 10, 'utf8')。
// 按 rune 计数，不能按字节切 —— 否则会切出半个 UTF-8 字符，输出乱码。
func truncateLabel(s string, n int) string {
	return string([]rune(s)[:min(n, len([]rune(s)))])
}

// domainOf 对应 PHP 的 get_domain()：把 http(s) 开头的来源标签压成域名。
func domainOf(s string) string {
	for _, p := range []string{"https://", "http://"} {
		if strings.HasPrefix(s, p) {
			s = s[len(p):]
			break
		}
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 && !strings.Contains(s[i:], "]") {
		// 去掉端口，仅当冒号后全是数字时才当端口，避免砍掉 IPv6 或域名的一部分
		if _, err := fmt.Sscanf(s[i+1:], "%d", new(int)); err == nil {
			s = s[:i]
		}
	}
	return s
}

// problemRowVM 是列表页一行的渲染输入。
type problemRowVM struct {
	// Marker 是首列的状态标记，对应 PHP 的 $view_problemset[$i][0]。
	Marker string
	// MarkerClass 是标记的 CSS 类。
	MarkerClass string
	ProblemID   int32
	Title       string
	Href        string
	Sources     []sourceVM
	// Contests 是这道题参加过的比赛，渲染成"比赛"列里的链接。
	Contests    []contestLinkVM
	AcceptRate  string
	RatePercent float64
	HasRate     bool
	// 原始计数，用于测试断言。
	Submit   sql0Int32
	Accepted sql0Int32
}

// contestLinkVM 是指向一场比赛的链接。
type contestLinkVM struct {
	ContestID int32
	Title     string
	Href      string
}

type sourceVM struct {
	Label  string
	Title  string
	Href   string
	Theme  string
	Trunc  string
	Hidden bool
}

// sql0Int32 是把 sql.NullInt32 摊平成 (值, 是否有效)，
// 让模板不需要 import database/sql。
type sql0Int32 struct {
	Value int32
	Valid bool
}

func (r sql0Int32) String() string {
	if !r.Valid {
		return "0"
	}
	return fmt.Sprintf("%d", r.Value)
}

// buildProblemRows 把仓储结果转成渲染模型。
func buildProblemRows(page ProblemListPage) []problemRowVM {
	rows := make([]problemRowVM, 0, len(page.Problems))
	for _, p := range page.Problems {
		// 首列：AC=Y 绿、提交过=N 红、没提交=空白
		// 字面量取自 problemset.php:170-179：
		//   AC        → "<div class='label label-success'>Y</div>"
		//   提交过未 AC → "<div class='label label-danger'>N</div>"
		//   未提交     → "<div class=none> </div>"
		// 容易写反成 "AC=Y / 提交过=N"，故此处保留原值并断言。
		marker, class := "", "none"
		if p.SolvedByUser {
			marker = "N"
			if p.AcceptedByUser {
				marker, class = "Y", "label label-success"
			} else {
				class = "label label-danger"
			}
		}
		acc := sql0Int32{Value: p.AcceptedStat.Int32, Valid: p.AcceptedStat.Valid}
		sub := sql0Int32{Value: p.SubmitStat.Int32, Valid: p.SubmitStat.Valid}
		// 通过率：PHP 用 accepted/submit，分母为 0 时显示 0.000%
		rate, has := 0.0, true
		if sub.Valid && sub.Value > 0 {
			rate = float64(acc.Value) / float64(sub.Value) * 100
		} else {
			rate, has = 0, false
		}
		rows = append(rows, problemRowVM{
			Marker:      marker,
			MarkerClass: class,
			ProblemID:   p.ProblemID,
			Title:       p.Title,
			Href:        fmt.Sprintf("problem.php?id=%d", p.ProblemID),
			Sources:     buildSources(p.Source),
			Contests:    buildContestLinks(p.Contests),
			AcceptRate:  acc.String() + "/" + sub.String(),
			RatePercent: rate,
			HasRate:     has,
			Submit:      sub,
			Accepted:    acc,
		})
	}
	return rows
}

// buildSources 对应 problemset.php 第 178-196 行的来源标签拼装。
func buildSources(src string) []sourceVM {
	out := []sourceVM{}
	// PHP 用 explode(" ", source) 再逐个 trim，并跳过 "" 与 "&nbsp"
	for _, cat := range SplitSource(src) {
		if cat == "" || cat == "&nbsp" {
			continue
		}
		if strings.HasPrefix(cat, "http") {
			cat = domainOf(cat)
		}
		out = append(out, sourceVM{
			Label:  cat,
			Title:  cat,
			Href:   "problemset.php?search=" + urlEncode(cat),
			Theme:  themeFor(cat),
			Trunc:  truncateLabel(cat, 10),
			Hidden: false,
		})
	}
	return out
}

// buildContestLinks 把比赛关联转成渲染模型。
//
// href 交给模板做 URL 转义会有坑（& 会被二次转义），这里拼好完整相对路径。
func buildContestLinks(links []ContestLink) []contestLinkVM {
	if len(links) == 0 {
		return nil
	}
	out := make([]contestLinkVM, 0, len(links))
	for _, l := range links {
		title := l.Title
		if title == "" {
			// contest.title 可为 NULL，兜底用编号，避免出现空链接。
			title = fmt.Sprintf("比赛 #%d", l.ContestID)
		}
		out = append(out, contestLinkVM{
			ContestID: l.ContestID,
			Title:     title,
			Href:      fmt.Sprintf("contest.php?cid=%d", l.ContestID),
		})
	}
	return out
}

// pageVM 是列表页整体渲染输入。
type pageVM struct {
	Title     string
	Rows      []problemRowVM
	Page      int
	TotalPage int
	Total     int64
	Search    string
	Viewer    string
	// 分页链接
	FirstHref template.URL
	PrevHref  template.URL
	NextHref  template.URL
	LastHref  template.URL
	HasPrev   bool
	HasNext   bool
	// Pages 是页码窗口；为空表示只有一页，不渲染分页条。
	Pages []pageItemVM
}

// pageItemVM 是分页条里的一格：页码，或两端省略号。
//
// 页码在 Go 侧算好而不是丢给模板做区间判断：模板里写区间运算既难读，
// 也没法在纯函数层面测试窗口边界。
type pageItemVM struct {
	Num      int
	Href     template.URL
	Active   bool
	Ellipsis bool
}

// paginationWindow 是当前页左右各显示多少个页码。
//
// PHP 模板用 $section=8（最多 17 个数字），对窄屏太长；这里取 4，
// 配合首页/末页与省略号，既能看到邻近页也能一跳到底。
const paginationWindow = 4

// problemsetHref 拼出 problemset.php 的分页链接。
//
// 在 Go 侧拼完整 URL 再以 template.URL 交给模板，而不是把裸片段塞进
// href —— 后者会被 html/template 的 URL 上下文二次转义，
// "&search=a+b" 变成 "%26search%3da%2bb"，链接直接失效。
//
// Search 来自用户输入，必须经 url.QueryEscape；其余部分是我们自己写的
// 字面量，因此整体可以安全地标为 template.URL。
func problemsetHref(page int, search string) template.URL {
	u := "problemset.php?page=" + strconv.Itoa(page)
	if search != "" {
		u += "&search=" + url.QueryEscape(search)
	}
	return template.URL(u)
}

func buildPageVM(page ProblemListPage) pageVM {
	vm := pageVM{
		Title:     "题目列表",
		Rows:      buildProblemRows(page),
		Page:      page.Page,
		TotalPage: int(page.TotalPage),
		Total:     page.Total,
		Search:    page.Search,
		Viewer:    page.Viewer.String(),
	}
	vm.HasPrev = page.Page > 1
	vm.HasNext = int64(page.Page) < page.TotalPage
	if vm.HasPrev {
		vm.PrevHref = problemsetHref(page.Page-1, page.Search)
	}
	if vm.HasNext {
		vm.NextHref = problemsetHref(page.Page+1, page.Search)
	}
	vm.Pages = buildPageItems(page.Page, vm.TotalPage, page.Search)
	if len(vm.Pages) > 0 {
		vm.FirstHref = problemsetHref(1, page.Search)
		vm.LastHref = problemsetHref(vm.TotalPage, page.Search)
	}
	return vm
}

// buildPageItems 生成页码窗口：首页 + 省略号 + 当前页 ±paginationWindow + 省略号 + 末页。
//
// 越界页（?page=9999）没有对应数据，PHP 那边 for 循环一次都不执行；
// 这里把窗口夹到末页，让用户还能点回去，且不高亮任何一格（因为当前页确实不存在）。
func buildPageItems(page, total int, search string) []pageItemVM {
	if total <= 1 {
		return nil
	}
	cur := page
	if cur > total {
		cur = total
	}
	start, end := cur-paginationWindow, cur+paginationWindow
	if start < 1 {
		start = 1
	}
	if end > total {
		end = total
	}

	items := make([]pageItemVM, 0, end-start+5)
	if start > 1 {
		items = append(items, pageItemVM{Num: 1, Href: problemsetHref(1, search)})
		if start > 2 {
			items = append(items, pageItemVM{Ellipsis: true})
		}
	}
	for n := start; n <= end; n++ {
		items = append(items, pageItemVM{Num: n, Href: problemsetHref(n, search), Active: n == page})
	}
	if end < total {
		if end < total-1 {
			items = append(items, pageItemVM{Ellipsis: true})
		}
		items = append(items, pageItemVM{Num: total, Href: problemsetHref(total, search)})
	}
	return items
}

// parseHexPrefix 解析十六进制字符串的前 n 位，超出部分忽略。
func parseHexPrefix(hexStr string, n int) uint32 {
	if n > len(hexStr) {
		n = len(hexStr)
	}
	var v uint32
	for i := 0; i < n; i++ {
		v <<= 4
		c := hexStr[i]
		switch {
		case c >= '0' && c <= '9':
			v |= uint32(c - '0')
		case c >= 'a' && c <= 'f':
			v |= uint32(c-'a') + 10
		case c >= 'A' && c <= 'F':
			v |= uint32(c-'A') + 10
		default:
			return v
		}
	}
	return v
}

// urlEncode 对应 PHP 的 urlencode()。
// url.QueryEscape 把空格编成 '+'，与 urlencode 一致；
// 而 url.PathEscape 会编成 %20，两者不同 —— 这里必须用 QueryEscape。
func urlEncode(s string) string { return url.QueryEscape(s) }
