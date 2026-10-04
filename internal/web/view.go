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
	AcceptRate  string
	RatePercent float64
	HasRate     bool
	// 原始计数，用于测试断言。
	Submit   sql0Int32
	Accepted sql0Int32
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
	PrevHref template.URL
	NextHref template.URL
	HasPrev  bool
	HasNext  bool
}

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
	return vm
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
