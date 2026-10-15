// Package web 里的 Markdown → HTML 转换。
//
// 题目标述在数据库里以 Markdown 存储，页面渲染时后台转译为 HTML；
// 数学公式的 $…$ / $$…$$ / \(…\) / \[…\] 定界符原样留在 HTML 文本中，
// 交给浏览器端 KaTeX auto-render 渲染。
//
// goldmark 开启 html.WithUnsafe，好处两方面：
//  1. 历史题目已服务器端预渲染的 KaTeX span 原样透传，不被转义；
//  2. 输出的 HTML 可以直接带 <span><table> 等标签。
package web

import (
	"bytes"
	"html/template"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	ghd "github.com/yuin/goldmark/renderer/html"
)

// md 共享的 Markdown 转换器：CommonMark + GFM（表格/删除线/任务列表/
// 自动链接），原始 HTML 透传。
var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithParserOptions(parser.WithAutoHeadingID()),
	goldmark.WithRendererOptions(ghd.WithUnsafe()),
)

// CommonMark 会把反斜杠转义（\( → (、\[ → [）吃掉，使 KaTeX 的
// \(…\)、\[…\] 定界符失效。这里用私用区哨兵先保护这几个定界符，转换后再还原，
// 让四组定界符（$…$、$$…$$、\(…\)、\[…\]）都原样留给前端 auto-render。
var mathEscapePairs = []struct{ sentinel, delim string }{
	{"\uE000", `\(`},
	{"\uE001", `\)`},
	{"\uE002", `\[`},
	{"\uE003", `\]`},
}

func protectMathDelims(s string) string {
	for _, p := range mathEscapePairs {
		s = strings.ReplaceAll(s, p.delim, p.sentinel)
	}
	return s
}

func restoreMathDelims(s string) string {
	for _, p := range mathEscapePairs {
		s = strings.ReplaceAll(s, p.sentinel, p.delim)
	}
	return s
}

// markdownString 把 Markdown 转为 HTML 字符串（不含可信标记），供模板与
// 编辑器实时预览共用，保证预览与题面输出完全一致。
func markdownString(s string) string {
	if s == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := md.Convert([]byte(protectMathDelims(s)), &buf); err != nil {
		return ""
	}
	return restoreMathDelims(buf.String())
}

// markdownHTML 是模板函数 markdown 的实现，把 Markdown 转为可信 HTML。
// 转换失败时返回空，避免把坏 Markdown 当作 HTML 直出。
func markdownHTML(s string) template.HTML {
	return template.HTML(markdownString(s))
}

// katexOptions 返回 KaTeX auto-render 的选项对象字面量（含一对大括号的 JS
// 片段），四组定界符与 mdui 模板保持一致。调用方直接写成
// renderMathInElement(target, {{katexOptions}})，少写大括号会导致整段脚本
// 语法错误、KaTeX 完全不执行。
func katexOptions() template.JS {
	return template.JS(`{delimiters:[{left:"$$",right:"$$",display:true},{left:"$",right:"$",display:false},{left:"\\(",right:"\\)",display:false},{left:"\\[",right:"\\]",display:true}],throwOnError:false,trust:true}`)
}
