// 模板引擎与页面模板定义。
//
// 所有 HTML 页面都通过 html/template 渲染，确保自动转义。
// 所有模板在启动时一次性 Parse 完毕，模板名拼写错误会在启动时报错。

package web

import (
	"bytes"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// toInt 把模板传入的数值统一成 int。
//
// 模板函数不做隐式类型转换：形参写 int 时，传进来的 int32 会直接报
// "wrong type for value; expected int; got int32"，整页渲染成 500。
// 视图模型里 int32 / int64 混用很常见（数据库列大多是 int32），
// 所以加减乘这类辅助函数统一收口到这里，按值转换而不是按类型报错。
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int8:
		return int(n)
	case int16:
		return int(n)
	case int32:
		return int(n)
	case int64:
		return int(n)
	case uint:
		return int(n)
	case uint8:
		return int(n)
	case uint16:
		return int(n)
	case uint32:
		return int(n)
	case uint64:
		return int(n)
	case float32:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

// templateFuncs 汇总模板可用的辅助函数。
//
// 所有函数必须是纯函数、无副作用，且不直接输出 HTML（由 template 自动转义）。
var templateFuncs = template.FuncMap{
	// raw 声明一段内容为可信 HTML，按原文输出。见包注释的适用范围说明。
	"raw": func(s string) template.HTML { return template.HTML(s) },
	// markdown 把题目标述的 Markdown 转为 HTML。goldmark 开启 raw-HTML
	// 透传（WithUnsafe），所以历史题目里已预渲染的 KaTeX span 原样保留；
	// 数学定界符 $…$ / $$…$$ / \(…\) / \[…\] 也原样留在 HTML 文本里，交给
	// 浏览器端 KaTeX auto-render 渲染。
	"markdown": markdownHTML,
	// katexOptions 是 KaTeX auto-render 的选项对象（JS 片段）。
	"katexOptions": katexOptions,
	// nullInt 把可空计数渲染成字符串；NULL 显示为空，与 PHP 把 NULL
	// 拼成空串的行为一致。
	"nullInt": nullIntStr,
	// trimFloat 去掉时限末尾的 .0，与 PHP 的 float 打印习惯对齐。
	"trimFloat": trimFloat,
	// maskStat 在 NOIP 锁榜时把统计数字替换成问号。
	"maskStat": maskStat,
	// contestErrorTitle 让错误原因与页面标题共用同一份文案。
	"contestErrorTitle": contestErrorTitle,
	"contestTypeName":   contestTypeName,
	"csrfToken":         csrfToken,
	"navActive":         navActive,
	"judgeResultLabel":  judgeResultLabel,
	"judgeResultClass":  judgeResultClass,
	"judgeTypeName":     judgeTypeName,
	"judgeTypeTip":      judgeTypeTip,
	"languageName":      languageName,
	// asset 给静态资源 URL 拼上内容哈希（?v=...），用于缓存击穿。
	"asset":                 func(p string) template.URL { return template.URL(p + "?v=" + assetVersion) },
	"contestErrorIsLimited": contestErrorIsLimited,
	"msgContestID":          func() string { return msgContestID },
	"msgPrivateWarning":     func() string { return msgPrivateWarning },
	"msgStanding":           func() string { return msgStanding },
	"msgStatistics":         func() string { return msgStatistics },
	"msgSubmit":             func() string { return msgSubmit },
	"msgPassword":           func() string { return msgPassword },
	"msgContestWord":        func() string { return msgContestWord },
	"msgContestName":        func() string { return msgContestName },
	"msgContestIDWord":      func() string { return msgContestID },
	"msgTime":               func() string { return msgTime },
	"msgContestOpen":        func() string { return msgContestOpen },
	"msgContestCreator":     func() string { return msgContestCreator },
	"msgPublic":             func() string { return msgPublic },
	"msgPrivate":            func() string { return msgPrivate },
	"msgProblemState":       func() string { return msgProblemState },
	"msgProblemID":          func() string { return msgProblemID },
	"msgTitle":              func() string { return msgTitle },
	"msgAC":                 func() string { return msgAC },
	"msgSubmitNum":          func() string { return msgSubmitNum },
	"msgAllContests":        func() string { return msgAllContests },
	"msgMyContests":         func() string { return msgMyContests },
	"msgBack":               func() string { return msgBack },
	"msgCoin":               func() string { return msgCoin },
	"msgNum":                func() string { return msgNum },
	"msgStartTime":          func() string { return msgStartTime },
	// 统计页用到的文案（conteststatistics.go 的 const 块）。
	"msgEndTime":       func() string { return msgEndTime },
	"msgServerTime":    func() string { return msgServerTime },
	"msgEnded":         func() string { return msgEnded },
	"msgRunning":       func() string { return msgRunning },
	"msgStart":         func() string { return msgStart },
	"msgEnd":           func() string { return msgEnd },
	"msgTotalTime":     func() string { return msgTotalTime },
	"msgLeftTime":      func() string { return msgLeftTime },
	"msgContestStatus": func() string { return msgContestStatus },
	"msgProblems":      func() string { return msgProblems },
	"msgIPVerify":      func() string { return msgIPVerify },
	"msgSetLoginIP":    func() string { return msgSetLoginIP },
	// 全局排名页用到的文案（handler_ranklist.go 的 const 块）。
	"msgRanklist":                  func() string { return msgRanklist },
	"msgRanklistDay":               func() string { return msgRanklistDay },
	"msgRanklistWeek":              func() string { return msgRanklistWeek },
	"msgRanklistMonth":             func() string { return msgRanklistMonth },
	"msgRanklistLastMonth":         func() string { return msgRanklistLastMonth },
	"msgRanklistYear":              func() string { return msgRanklistYear },
	"msgUser":                      func() string { return msgUser },
	"msgNick":                      func() string { return msgNick },
	"msgGroupName":                 func() string { return msgGroupName },
	"msgNumber":                    func() string { return msgNumber },
	"msgSolved":                    func() string { return msgSolved },
	"msgRatio":                     func() string { return msgRatio },
	"msgSearch":                    func() string { return msgSearch },
	"contestErrorShowContestLinks": contestErrorShowContestLinks,
	"add":                          func(a, b any) int { return toInt(a) + toInt(b) },
	"sub":                          func(a, b any) int { return toInt(a) - toInt(b) },
	// until 返回 [0, n) 的整数序列，供模板 range 分页用。
	"until": func(n int) []int {
		result := make([]int, n)
		for i := range result {
			result[i] = i
		}
		return result
	},
	// even 判断整数是否为偶数，供模板交替行样式用。
	"even": func(i int) bool { return i%2 == 0 },
	// int 转换，供模板中将 int32 转为 int 以便比较。
	"int": func(i int32) int { return int(i) },
	// languageNames 和 resultLabels 供 statusPage 模板的 select 选项用。
	// languageNames 只列本机已装语言（存在 <id>.lang.toml），来自注册表。
	"languageNames": func() map[int32]string { return installedLanguageMap() },
	// coinStars 复刻 contest.php:95 的 min(coin, 5)：题面最多 5 颗星。
	"coinStars":    coinStars,
	"resultLabels": func() []resultOption { return resultOptions },
	// judgeLabelsJSON / judgeClassesJSON 供状态页把结果标签与配色嵌进
	// 自动刷新的 JS（对应 PHP 模板里的 judge_result / judge_color）。
	// 结果文案统一走 judgeResultLabel，避免与页面显示的标签不一致。
	"judgeLabelsJSON": func() template.JS {
		m := make(map[int32]string, 16)
		for i := int32(0); i < 16; i++ {
			m[i] = judgeResultLabel(i)
		}
		b, _ := json.Marshal(m)
		return template.JS(b)
	},
	"judgeClassesJSON": func() template.JS {
		b, _ := json.Marshal(judgeResultClasses)
		return template.JS(b)
	},
}

// resultOption 是结果下拉的一项。
type resultOption struct {
	Value int32
	Label string
}

// resultOptions 是 statusPage 的结果下拉框。
//
// 顺序与 PHP status.php 一致：先 4..11（AC/PE/WA/…/CE），再 0..3
// （等待/等待重判/编译中/运行并评判），12..15 不列（PHP 也不列）。
// 文案统一取自 judgeResultLabel，不再维护第二张表 —— 之前那张表把
// 1/2/3 写错位，"等待重判"在筛选里根本不出现。
var resultOptions = func() []resultOption {
	order := []int32{4, 5, 6, 7, 8, 9, 10, 11, 0, 1, 2, 3}
	out := make([]resultOption, 0, len(order))
	for _, v := range order {
		out = append(out, resultOption{Value: v, Label: judgeResultLabel(v)})
	}
	return out
}()

// pageTemplates 汇集全部页面模板。
//
// 结构约定：
//   - "layout" 是所有页面共用的外壳，负责 <head> 与容器；
//   - 每个页面是一个独立的具名模板，只输出内容片段；
//   - 页面模板之间用 {{ template "x" }} 复用片段（如分页条）。
//
// 一次性 Parse 全部模板，好处是模板间的引用在启动时就能校验，
// 不会出现"某个页面第一次被访问才报 no such template"。
var pageTemplates = template.Must(
	template.New("hustoj").Funcs(templateFuncs).Parse(layoutTmpl + pageContentTmpls))

const layoutTmpl = `
{{define "layout"}}<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="color-scheme" content="light dark">
<meta name="theme-color" content="#3b5bdb" media="(prefers-color-scheme: light)">
<meta name="theme-color" content="#0f141c" media="(prefers-color-scheme: dark)">
<link rel="stylesheet" href="{{asset "/assets/simple.min.css"}}">
<link rel="stylesheet" href="{{asset "/assets/hustoj.css"}}">
{{/* KaTeX：题面/新闻里的公式是服务端预渲染好的（同时含 .katex-mathml 与
     .katex-html），缺这份 CSS 两者都会显示，公式因此乱掉。
     资源（CSS + 字体）随仓库放在 internal/web/static/，经 /assets/ 提供，
     不再依赖 PHP web 根的 /template/ 目录，也不用题面里那个老 CDN。 */}}
<link rel="stylesheet" href="{{asset "/assets/katex.min.css"}}">
<title>{{.Title}}</title>
</head>
<body>
{{template "siteHeader" .Nav}}
{{if .Nav.Admin}}{{template "adminNav" .Nav.Admin}}{{end}}
<main>
<div class="page-head"><h1>{{.Title}}</h1></div>
{{.Content}}
</main>
{{template "siteFooter" .Nav}}
</body>
</html>
{{end}}`

const pageContentTmpls = `
{{/* siteHeader 是站点导航头，对应 template/syzoj/header.php 的固定菜单。
     结构与配色全部交给 hustoj.css（.site-header / .site-nav / .site-user），
     模板里不再写内联样式；当前页由 navActive 标记 aria-current。 */}}
{{define "siteHeader"}}<header class="site-header">
<div class="site-header-inner">
<a class="site-brand" href="/" aria-label="{{.OJName}} 首页">
<span class="site-brand-mark">OJ</span>
<span class="site-brand-name">{{.OJName}}</span>
</a>
<nav class="site-nav" aria-label="主导航">
<a href="/problemset.php"{{if navActive .Path "/problemset.php"}} class="current" aria-current="page"{{end}}>题目</a>
<a href="/category.php"{{if navActive .Path "/category.php"}} class="current" aria-current="page"{{end}}>来源</a>
<a href="/contest.php{{if .LoggedIn}}?my{{end}}"{{if navActive .Path "/contest.php"}} class="current" aria-current="page"{{end}}>竞赛</a>
<a href="/status.php"{{if navActive .Path "/status.php"}} class="current" aria-current="page"{{end}}>状态</a>
<a href="/ranklist.php"{{if navActive .Path "/ranklist.php"}} class="current" aria-current="page"{{end}}>排名</a>
<a href="/faqs.php"{{if navActive .Path "/faqs.php"}} class="current" aria-current="page"{{end}}>常见问答</a>
</nav>
<div class="site-user">
{{if .LoggedIn}}
<a class="site-user-name" href="/userinfo.php?user={{.UserID}}"><span class="site-user-dot" aria-hidden="true"></span>{{.UserID}}{{if .Nick}}<span class="site-user-nick">（{{.Nick}}）</span>{{end}}</a>
<a href="/portal.php">待办</a>
<a href="/modifypage.php">修改资料</a>
{{if .IsAdmin}}<a href="/admin/index.php">后台</a>{{end}}
<a class="site-logout" href="/logout.php">退出</a>
{{else}}
<a href="/loginpage.php">登录</a>
{{if .ShowRegister}}<a class="site-register" href="/registerpage.php">注册</a>{{end}}
{{end}}
</div>
</div>
{{if .CID}}<nav class="site-subnav" aria-label="比赛导航">
<span class="site-subnav-label">比赛 #{{.CID}}</span>
<a href="/contest.php?cid={{.CID}}">赛题</a>
<a href="/status.php?cid={{.CID}}">比赛状态</a>
<a href="/contestrank.php?cid={{.CID}}">比赛排名</a>
<a href="/contestrank-oi.php?cid={{.CID}}">OI 排名</a>
</nav>{{end}}
</header>
{{end}}

{{/* siteFooter 是页面底栏：站点名 + 常用入口 + 技术栈说明。 */}}
{{define "siteFooter"}}<footer class="site-footer">
<div class="site-footer-inner">
<span class="site-footer-brand">{{.OJName}}</span>
<nav class="site-footer-nav" aria-label="页脚导航">
<a href="/problemset.php">题库</a>
<a href="/contest.php">竞赛</a>
<a href="/status.php">状态</a>
<a href="/ranklist.php">排名</a>
<a href="/faqs.php">帮助</a>
</nav>
<span class="site-footer-note">Powered by HUSTOJ-Go</span>
</div>
</footer>
{{end}}

{{/* adminNav 是后台导航条：/admin/ 下的所有页面在公共导航下方显示一条，
     按角色列出可用功能。取代原先 frameset + menu.php(target=main) 的做法——
     frameset 嵌进普通 <main> 里会被浏览器忽略，页面因此空白。 */}}
{{define "adminNav"}}
<nav class="admin-nav" aria-label="后台导航">
<a class="admin-nav-home" href="/admin/index.php">后台首页</a>
{{if or .IsAdmin .IsProblemEditor .IsContestCreator}}<a href="/admin/problem_list.php">题目列表</a>{{end}}
{{if or .IsAdmin .IsProblemEditor}}<a href="/admin/problem_add_page.php">添加题目</a><a href="/admin/problem_export.php">导出题目</a><a href="/admin/problem_copy.php">复制题目</a>{{end}}
{{if or .IsAdmin .IsContestCreator}}<a href="/admin/contest_list.php">比赛列表</a><a href="/admin/contest_add.php">添加比赛</a>{{end}}
{{if or .IsAdmin .IsPasswordSetter}}<a href="/admin/user_list.php">用户列表</a>{{end}}
{{if or .IsAdmin .IsUserAdder}}<a href="/admin/user_add.php">添加用户</a><a href="/admin/user_import.php">导入用户</a>{{end}}
{{if or .IsAdmin .IsPasswordSetter}}<a href="/admin/changepass.php">修改密码</a>{{end}}
{{if .IsAdmin}}<a href="/admin/privilege_list.php">权限</a>{{end}}
{{if .IsAdmin}}<a href="/admin/news_list.php">新闻</a>{{end}}
{{if .IsAdmin}}<a href="/admin/rejudge.php">重新判题</a><a href="/admin/solution_statistics.php">解题统计</a>{{end}}
{{if .IsAdmin}}<a href="/admin/ranklist_export.php">导出榜单</a>{{end}}
{{if .IsAdmin}}<a href="/online.php">在线用户</a>{{end}}
<a href="/admin/help.php">帮助</a>
<a href="/status.php">查看 OJ</a>
</nav>
{{end}}

{{define "problemset"}}{{template "problemsetTable" .}}{{template "pagination" .}}{{end}}

{{define "problemsetTable"}}<table class="ui celled table">
<thead><tr>
<th class="one wide"></th>
<th class="one wide">编号</th>
<th class="left aligned">标题</th>
<th class="one wide">来源</th>
<th>比赛</th>
<th class="one wide">通过/提交</th>
<th class="one wide">通过率</th>
</tr></thead>
<tbody>
{{range .Rows}}<tr>
<td><div class="{{.MarkerClass}}">{{.Marker}}</div></td>
<td><b>{{.ProblemID}}</b></td>
<td><a href="{{.Href}}">{{.Title}}</a></td>
<td>{{range .Sources}}<a class="ui label {{.Theme}}" href="{{.Href}}" title="{{.Title}}">{{.Trunc}}</a> {{end}}</td>
<td>{{range .Contests}}<a class="problemset-contest" href="{{.Href}}" title="{{.Title}}">{{.Title}}</a>{{end}}</td>
<td>{{if .Submit.Valid}}{{.Accepted.Value}}/{{.Submit.Value}}{{else}}0/0{{end}}</td>
<td>{{if .HasRate}}{{printf "%.3f" .RatePercent}}%{{else}}0.000%{{end}}</td>
</tr>
{{end}}</tbody></table>
{{end}}

{{/* problemActions 是题面下方的操作区。

   未结束的比赛必须带 cid/pid/langmask 提交，否则提交页丢失比赛上下文；
   结束后 PHP 退回练习方式提交（problem.php:92-95），此时不能再带 langmask，
   否则会按比赛的受限语言列表提交。 */}}
{{define "problemActions"}}<div class="ui buttons">
{{if .Contest}}
{{if .Contest.IsOver}}
<a class="ui primary button" href="submitpage.php?id={{.ProblemID}}">提交</a>
{{else}}
<a class="ui primary button" href="submitpage.php?cid={{.Contest.CID}}&pid={{.Contest.PID}}&langmask={{.Contest.Langmask}}">提交</a>
<a class="ui button" href="status.php?problem_id={{.ProblemID}}&cid={{.Contest.CID}}">本比赛提交记录</a>
<a class="ui button" href="contest.php?cid={{.Contest.CID}}">返回比赛</a>
{{end}}
{{else}}
<a class="ui primary button" href="submitpage.php?id={{.ProblemID}}">提交</a>
<a class="ui button" href="status.php?problem_id={{.ProblemID}}">提交记录</a>
{{end}}
</div>{{end}}

{{/* problemDetail 是题面正文。

   Description 以 Markdown 存、后台转 HTML 输出，其余字段仍按 PHP 行为
   原样输出（富文本）。数学定界符留到 HTML 文本里由浏览器端 KaTeX
   auto-render 渲染。Defunct='Y' 的题目只给提示，不展示任何内容。 */}}
{{define "problemDetail"}}<div class="ui container problem-detail">
{{if .Contest}}<div class="ui message">
本题目来自比赛 <a href="contest.php?cid={{.Contest.CID}}">{{.Contest.Title}}</a> 第 {{.Contest.PID}} 题
</div>{{end}}
{{/* 标题由 layout 统一渲染 <h1>{{.Title}}</h1>，这里不再重复，否则题目标题出现两次。 */}}
<div class="ui label">命题人：<span id="creator"></span></div>
{{if eq .Defunct "Y"}}
<div class="ui warning message">该题目已被删除或尚未公开</div>
{{else}}
{{/* 时限/内存/评测类型排成一行，放在描述上方、命题人下方。 */}}
<div class="problem-meta">
<span class="ui label">Time Limit：{{trimFloat .TimeLimit}} 秒</span>
<span class="ui label">Memory Limit：{{.MemoryLimit}} MiB</span>
{{$spj := .SPJ}}{{with judgeTypeName $spj}}<span class="ui label" title="{{judgeTypeTip $spj}}">评测类型：{{.}}</span>{{end}}
</div>
<div class="problem-meta">
<span class="ui label">提交：{{if .StatsHidden}}?{{else}}{{if .SubmitStat.Valid}}{{.SubmitStat.Int32}}{{else}}0{{end}}{{end}}</span>
<span class="ui label">通过：{{if .StatsHidden}}?{{else}}{{if .AcceptedStat.Valid}}{{.AcceptedStat.Int32}}{{else}}0{{end}}{{end}}</span>
</div>
{{if or .InputFileName .OutputFileName}}
<div class="ui warning message">
<strong>本题需要文件读写（freopen）</strong>：{{if .InputFileName}}从文件 <code>{{.InputFileName}}</code> 读入{{end}}{{if and .InputFileName .OutputFileName}}，{{end}}{{if .OutputFileName}}结果写出到文件 <code>{{.OutputFileName}}</code>{{end}}，请勿使用标准输入/输出。
</div>
{{end}}
{{if .Description}}<div class="problem-description md-body">{{markdown .Description}}</div>{{end}}
{{if .Input}}<h3 class="ui header">输入</h3><div class="md-body">{{markdown .Input}}</div>{{end}}
{{if .Output}}<h3 class="ui header">输出</h3><div class="md-body">{{markdown .Output}}</div>{{end}}
{{if or .SampleInput.Valid .SampleOutput.Valid}}<div class="sample-grid">
{{if .SampleInput.Valid}}<div class="sample-card"><h4>样例输入</h4><pre>{{.SampleInput.String}}</pre></div>{{end}}
{{if .SampleOutput.Valid}}<div class="sample-card"><h4>样例输出</h4><pre>{{.SampleOutput.String}}</pre></div>{{end}}
</div>{{end}}
{{if .Hint}}{{if not .HintHidden}}<h3 class="ui header">提示</h3><div class="md-body">{{markdown .Hint}}</div>{{end}}{{end}}
{{if .Source.Valid}}<h3 class="ui header">来源</h3><div>{{.Source.String}}</div>{{end}}
{{template "problemActions" .}}
{{if .Description}}
{{/* KaTeX auto-render：Markdown 渲染出的数学定界符原样留在 HTML 文本里，
     由浏览器端 KaTeX auto-render 对 .md-body 整体或局部渲染。 */}}
<script src="{{asset "/assets/katex.min.js"}}" defer></script>
<script src="{{asset "/assets/auto-render.min.js"}}" defer></script>
<script>
document.addEventListener('DOMContentLoaded',function(){
  var nodes=document.querySelectorAll('.md-body');
  for(var i=0;i<nodes.length;i++){
    if(typeof renderMathInElement!=='undefined'){
      try{renderMathInElement(nodes[i],{{katexOptions}});}catch(e){}
    }
  }
});
</script>
{{end}}
{{end}}

{{/* 命题人异步加载：problem.php 用 $("#creator").load("problem-ajax.php?pid=")。*/}}
<script>
(function () {
  var el = document.getElementById('creator');
  if (!el) return;
  var x = new XMLHttpRequest();
  x.onreadystatechange = function () {
    if (x.readyState === 4 && x.status === 200) el.innerHTML = x.responseText;
  };
  x.open('GET', 'problem-ajax.php?pid={{.ProblemID}}', true);
  x.send();
})();
</script>
</div>{{end}}

{{/* contestError 是比赛准入失败的错误页。

   PHP 在比赛不可访问时一律 exit(0)，即状态码仍是 200，因此这里的页面
   正常渲染而不是走 errorPage 的红色错误框。 */}}
{{define "contestError"}}<div class="ui container">
<center>
<h3>{{msgContestID}} : {{.CID}} - {{if .Title}}{{.Title}}{{else}}比赛{{.CID}}{{end}}</h3>
<div class="md-body">{{markdown .Description}}</div>
{{if not (contestErrorIsLimited .Deny)}}<span class="text-danger">{{.Warning}}</span>
<br><br>
{{if contestErrorShowContestLinks .Deny}}<div class="btn-group">
<a href="contestrank.php?cid={{.CID}}" class="btn btn-primary">{{msgStanding}}</a>
<a href="contestrank-oi.php?cid={{.CID}}" class="btn btn-primary">OI{{msgStanding}}</a>
<a href="conteststatistics.php?cid={{.CID}}" class="btn btn-primary">{{msgStatistics}}</a>
</div>
<br><br>{{end}}
<table align="center" width="80%"><tr align="center"><td>
<form class="form-inline" method="post" action="contest.php?cid={{.CID}}">
<input class="input-mini" type="password" name="password" value="" placeholder="{{msgContestWord}}{{msgPassword}}">
<button class="form-control">{{msgSubmit}}</button>
</form>
</td></tr></table>
<br>{{end}}
<a href="javascript:history.go(-1)">{{msgBack}}</a>
</center>
</div>{{end}}

{{/* pagination 是题目列表的分页条：首页/末页、上一页/下一页，以及
   当前页附近的页码窗口。只给“上一页/下一页”没法一眼看到自己在哪、
   也跳不到远处，PHP 模板原本就有这一排页码。 */}}
{{define "pagination"}}{{if .Pages}}
<div class="ui pagination menu">
{{if .HasPrev}}<a class="item" href="{{.FirstHref}}" title="第一页">«</a>
<a class="item" href="{{.PrevHref}}" rel="prev">‹ 上一页</a>{{end}}
{{range .Pages}}{{if .Ellipsis}}<span class="item" style="border-color:transparent;">…</span>{{else}}<a class="{{if .Active}}active {{end}}item" href="{{.Href}}">{{.Num}}</a>{{end}}{{end}}
{{if .HasNext}}<a class="item" href="{{.NextHref}}" rel="next">下一页 ›</a>
<a class="item" href="{{.LastHref}}" title="最后一页">»</a>{{end}}
<span class="item" style="border-color:transparent;color:#888;">第 {{.Page}} / {{.TotalPage}} 页（共 {{.Total}} 条）</span>
</div>{{end}}{{end}}

{{define "alertAndBack"}}<script language="javascript">
alert({{.Message}});
history.go(-1);
</script>{{end}}

{{define "submitPage"}}<div class="submit-page">
<form id="frmSolution" action="submit.php{{if .Ajax}}?ajax{{end}}" method="post" enctype="multipart/form-data">
<input type="hidden" name="csrf_token" value="{{csrfToken .Token}}">
<div class="submit-meta">
{{if .IsContest}}
Problem <span class="blue"><b>{{add .Num 65 | printf "%c"}}</b></span> of Contest <span class="blue"><b>{{.ContestID}}</b></span>
<input id="cid" type="hidden" value="{{.ContestID}}" name="cid">
<input id="pid" type="hidden" value="{{.Num}}" name="pid">
{{else}}
<span class="text-primary">Problem <b>{{.ProblemID}}</b></span>
<input id="problem_id" type="hidden" value="{{.ProblemID}}" name="id">
{{end}}
<span id="language_span">Language:
<select id="language" name="language">
{{range .Languages}}
<option value="{{.ID}}"{{if eq .ID $.DefaultLanguage}} selected{{end}}>{{.Name}}</option>
{{end}}
</select>
</span>
</div>
<textarea cols="180" rows="20" id="source" name="source"></textarea>
<button type="submit" class="ui primary button">Submit</button>
</form>
</div>{{end}}


{{define "contestList"}}
<form method="post" action="contest.php">
<input type="text" name="keyword" value="{{.Keyword}}" placeholder="{{msgContestName}} …">
<button type="submit">搜索</button>
{{if .MyOnly}}<a href="contest.php">{{msgAllContests}}</a>{{else}}<a href="contest.php?my">{{msgMyContests}}</a>{{end}}
</form>

<nav class="ui pagination menu">
{{if gt .Page 1}}<a class="item" href="contest.php?page={{sub .Page 1}}">‹</a>{{end}}
{{range .PageNumbers}}
{{if eq . $.Page}}<a class="active item" href="contest.php?page={{.}}">{{.}}</a>{{else}}<a class="item" href="contest.php?page={{.}}">{{.}}</a>{{end}}
{{end}}
{{if lt .Page .TotalPage}}<a class="item" href="contest.php?page={{add .Page 1}}">›</a>{{end}}
</nav>

<table class="ui very basic center aligned table">
<thead><tr><th>{{msgContestIDWord}}</th><th>{{msgContestName}}</th><th>{{msgTime}}</th><th>{{msgContestOpen}}</th><th>{{msgContestCreator}}</th></tr></thead>
<tbody>
{{range .Contests}}
<tr>
<td>{{.CID}}</td>
<td><a href="contest.php?cid={{.CID}}">{{.Title}}</a></td>
<td><span class="text-{{.StatusTone}}">{{.StatusText}}</span> {{.DateText}}&nbsp;{{if .StatusLength}}<span class="text-{{.StatusTone}}">{{.StatusLabel}} {{.StatusLength}}</span>{{end}}</td>
<td>{{if .IsPrivate}}<span class="text-danger">{{msgPrivate}}</span>{{else}}<span class="text-primary">{{msgPublic}}</span>{{end}}</td>
<td>{{.Owner}}</td>
</tr>
{{else}}
<tr><td colspan="5">暂无比赛</td></tr>
{{end}}
</tbody>
</table>
{{end}}

{{define "contestProblems"}}<h3>比赛 #{{.CID}} - {{.Contest.Title.String}}</h3>
{{if .Flags.NOIP}}<div class="ui warning message">NOIP 模式：仅显示是否提交，不显示结果</div>{{end}}
{{if .Flags.HideOthers}}<div class="ui warning message">本比赛屏蔽他人提交</div>{{end}}
<table class="ui selectable celled table">
<thead><tr>
<th class="one wide">{{if .Flags.LoggedIn}}{{msgProblemState}}{{end}}</th>
<th class="two wide">{{msgProblemID}}</th>
<th>{{msgTitle}}</th>
<th>{{msgAC}}</th>
<th>{{msgSubmitNum}}</th>
</tr></thead>
<tbody>
{{range .Problems}}
<tr>
<td>{{if .Mark}}<span class="label label-{{if eq .Mark "Y"}}success{{else if eq .Mark "N"}}danger{{else}}default{{end}}">{{.Mark}}</span>{{end}}</td>
<td>{{if .LetterLink}}<a href="{{.LetterLink}}">{{.Letter}}</a>{{else}}{{.Letter}}{{end}}</td>
<td>{{if .TitleHidden}}{{.TitleNote}}{{else}}{{if .TitleLink}}<a href="{{.TitleLink}}">{{.Title}}</a>{{else}}{{.Title}}{{end}}{{if .IsRunning}}{{if .Coin}}<span style="color:orange">{{coinStars .Coin}}</span>{{end}}{{end}}{{end}}</td>
<td>{{if .Masked}}?{{else}}{{.Accepted}}{{end}}</td>
<td>{{if .Masked}}?{{else}}{{.SubmitCount}}{{end}}</td>
</tr>
{{end}}
</tbody>
</table>
{{end}}

{{define "contestStatistics"}}
<h3>{{msgContestID}} : {{.CID}} - {{.Title}}</h3>
<div class="md-body">{{markdown .Description}}</div>
{{msgServerTime}} : {{.ServerTime}}<br>
{{if .LockTime}}Lock Board Time: {{.LockTime}}<br>{{end}}
<span class="text-{{.StatusTone}}">{{.StatusText}}</span>{{if .StatusExtra}}&nbsp;<span class="text-{{.StatusTone}}">{{.StatusExtra}}</span> {{.StatusLength}}{{end}}
<br><br>
{{msgContestStatus}} : <span class="text-{{.ContestStatusTone}}">{{.ContestStatusText}}</span>&nbsp;&nbsp;{{msgContestOpen}} : {{if .IsPrivate}}<span class="text-danger">{{msgPrivate}}</span>{{else}}<span class="text-primary">{{msgPublic}}</span>{{end}}
<br>
{{msgStartTime}} : {{.StartTime}}<br>
{{msgEndTime}} : {{.EndTime}}<br><br>
<div class="btn-group">
<a href="contest.php?cid={{.CID}}" class="btn btn-primary btn-sm">{{msgProblems}}</a>
<a href="status.php?cid={{.CID}}" class="btn btn-primary btn-sm">{{msgSubmit}}</a>
<a href="contestrank.php?cid={{.CID}}" class="btn btn-primary btn-sm">{{msgStanding}}</a>
<a href="contestrank-oi.php?cid={{.CID}}" class="btn btn-primary btn-sm">OI{{msgStanding}}</a>
<a href="conteststatistics.php?cid={{.CID}}" class="btn btn-primary btn-sm">{{msgStatistics}}</a>
{{if .CanManage}}<a href="suspect_list.php?cid={{.CID}}" class="btn btn-warning btn-sm">{{msgIPVerify}}</a>
<a href="user_set_ip.php?cid={{.CID}}" class="btn btn-success btn-sm">{{msgSetLoginIP}}</a>
<a target="_blank" href="../../admin/contest_edit.php?cid={{.CID}}" class="btn btn-success btn-sm">EDIT</a>{{end}}
</div>
<br>
<center>
<h4>{{.LockedMsg}}</h4>
<table id="cs" class="table-hover table-striped" align="center" width="90%" border="0">
<thead><tr class="toprow">
<th class="text-center"></th>
{{range .Header}}<th class="text-center">{{.}}</th>{{end}}
</tr></thead>
<tbody>
{{range .Rows}}<tr class="{{if .IsOdd}}oddrow{{else}}evenrow{{end}}">
<td class="text-center">{{if .Link}}<a href="{{.Link}}">{{.Label}}</a>{{else}}{{.Label}}{{end}}</td>
{{range .Cells}}<td class="text-center">{{.}}</td>{{end}}
</tr>{{end}}
<tr class="evenrow"><td class="text-center">{{.Total.Label}}</td>{{range .Total.Cells}}<td class="text-center">{{.}}</td>{{end}}</tr>
</tbody>
</table>
{{if .Chart}}<br>
<table class="ui celled table">
<thead><tr><th>日期</th><th>{{msgSubmit}}</th><th>{{msgAC}}</th></tr></thead>
<tbody>{{range .Chart}}<tr><td>{{.Day}}</td><td>{{.Submit}}</td><td>{{.AC}}</td></tr>{{end}}</tbody>
</table>{{end}}
</center>
{{end}}

{{define "contestStatsError"}}<div class="ui negative message">
<div class="content">
<div class="header" style="margin-bottom: 10px;">{{raw .Message}}</div>
<p><a href="javascript:history.go(-1)">{{msgBack}}</a></p>
</div></div>{{end}}

{{define "loginForm"}}<div class="auth-card">
<form action="/login.php" method="post">
{{template "vcodeField" .}}
<label>用户名
<input name="user_id" placeholder="用户名" type="text" autocomplete="username">
</label>
<label>密码
<input name="password" placeholder="密码" type="password" autocomplete="current-password">
</label>
{{template "csrfInput" .}}
<button type="submit">登录</button>
</form>
<p class="center"><a href="/registerpage.php">注册</a> | <a href="/lostpassword.php">忘记密码</a></p>
</div>{{end}}

{{define "registerForm"}}<div class="ui container auth-card">
<form class="ui form" action="/register.php" method="post">
{{template "vcodeField" .}}
<div class="field"><label>用户名</label><input name="user_id" placeholder="用户名" type="text" autocomplete="username" required></div>
<div class="field"><label>昵称</label><input name="nick" placeholder="昵称" type="text"></div>
<div class="two fields"><div class="field"><label>密码</label><input name="password" type="password" autocomplete="new-password" required></div>
<div class="field"><label>确认密码</label><input name="rptpassword" type="password" autocomplete="new-password" required></div></div>
<div class="field"><label>学校</label><input name="school" placeholder="学校" type="text"></div>
<div class="field"><label>邮箱</label><input name="email" placeholder="邮箱" type="email" autocomplete="email" required></div>
{{template "csrfInput" .}}
<button class="ui fluid large submit button" type="submit">注册</button>
</form>
</div>{{end}}

{{define "vcodeField"}}{{if .ShowVCode}}<label>验证码
<input name="vcode" placeholder="验证码" type="text" autocomplete="off">
</label>{{end}}{{end}}

{{define "csrfInput"}}{{if .Token}}<input type="hidden" name="csrf_token" value="{{csrfToken .Token}}">{{end}}{{end}}

{{define "errorPageBody"}}<div class="ui negative message">
<div class="ui header">错误</div>
<p>{{.Message}}</p>
<a class="ui button" href="/index.php">返回首页</a>
</div>{{end}}

{{define "errorPage"}}<div class="ui negative message">
<div class="ui header">错误</div>
<p>{{.Message}}</p>
<a class="ui button" href="/index.php">返回首页</a>
</div>{{end}}

{{define "statusPage"}}<div class="ui container status-page">
<form class="ui form" method="GET">
<div class="status-filters">
<div class="field"><label>比赛</label><input name="cid" value="{{.Filter.CIDText}}" placeholder="比赛 ID" style="width:90px"></div>
<div class="field"><label>题目</label><input name="problem_id" value="{{.Filter.ProblemIDText}}" placeholder="题目 ID" style="width:90px"></div>
<div class="field"><label>用户</label><input name="user_id" value="{{.Filter.UserID}}" placeholder="用户名" style="width:120px"></div>
{{/* range 内部 . 会被重绑定成 map 的元素，因此先把筛选值取到局部变量。
     直接在 range 里写 .Filter.Language 会渲染失败（页面 500）。 */}}
{{$selLang := .Filter.SelectedLanguage}}
{{$selResult := .Filter.SelectedResult}}
<div class="field"><label>语言</label>
<select name="language">
<option value="-1"{{if eq (int $selLang) -1}} selected{{end}}>全部</option>
{{range $i, $name := languageNames}}
<option value="{{$i}}"{{if eq $i (int $selLang)}} selected{{end}}>{{$name}}</option>
{{end}}
</select>
</div>
<div class="field"><label>结果</label>
<select name="jresult">
<option value="-1"{{if eq (int $selResult) -1}} selected{{end}}>全部</option>
{{range $opt := resultLabels}}
<option value="{{$opt.Value}}"{{if eq $opt.Value (int $selResult)}} selected{{end}}>{{$opt.Label}}</option>
{{end}}
</select>
</div>
<div class="field"><label>学校</label><input name="school" value="{{.Filter.School}}" placeholder="学校" style="width:120px"></div>
<div class="field"><label>群组</label><input name="group_name" value="{{.Filter.GroupName}}" placeholder="群组" style="width:120px"></div>
<div class="field"><div class="ui checkbox"><input type="checkbox" name="showsim" value="1"{{if eq .Filter.ShowSim 1}} checked{{end}}><label>相似度</label></div></div>
<div class="field"><button class="ui button" type="submit">筛选</button></div>
</div></form>

<div class="status-table-wrap">
<table id="result-tab" class="ui celled table">
{{/* 列宽按"最长可能内容"比例分配（表宽自适应内容区，最小宽度见 hustoj.css
     的 --status-table-min-width）。用 colgroup 让比例显式、可预期，
     不靠浏览器按内容各自伸缩。 */}}
<colgroup>
<col style="width:9%">
<col style="width:13%">
<col style="width:6%">
<col style="width:9%">
<col style="width:9%">
<col style="width:8%">
<col style="width:12%">
<col style="width:11%">
<col style="width:23%">
</colgroup>
<thead>
<tr><th>ID</th><th>用户</th><th>题目</th><th>结果</th><th>内存</th><th>时间</th><th>语言</th><th>代码长度</th><th>提交时间</th></tr>
</thead><tbody>
{{range .Rows}}<tr{{if not .Masked}} data-sid="{{.SolutionID}}" data-result="{{.Result}}"{{end}}>
<td>{{.SolutionID}}</td>
<td><a href="/userinfo.php?user={{.UserID}}">{{.UserID}}</a></td>
<td>{{if gt .ProblemID 0}}<a href="/problem.php?id={{.ProblemID}}">{{.ProblemID}}</a>{{else}}{{.ProblemID}}{{end}}</td>
<td>{{if .Masked}}<span class="{{.ResultClass}}">{{.ResultLabel}}</span>{{else}}<a href="/showsource.php?id={{.SolutionID}}" class="{{.ResultClass}}" title="查看代码与判题信息{{with .ScoreText}}（{{.}}）{{end}}">{{.ResultLabel}}</a>{{end}}</td>
<td class="center">{{.MemoryText}}</td>
<td class="center">{{.TimeText}}</td>
<td class="center">{{if .CanViewSource}}<a href="/showsource.php?id={{.SolutionID}}" title="查看代码">{{languageName .Language}}</a>{{else}}{{languageName .Language}}{{end}}</td>
<td class="center">{{.CodeLength}} bytes</td>
<td>{{.InDate.Format "2006-01-02 15:04:05"}}</td>
</tr>
{{end}}</tbody></table>
</div>
{{template "statusPagination" .}}

{{/* 自动刷新：对应 template/syzoj/auto_refresh.js 的轮询分支。PHP 用
   status-ajax.php 拉回 "result,memory KB,time ms,judger,pass_rate,user_id"，
   这里更新结果/内存/时间三格，判题完成后把非 AC 的得分写进悬停提示。
   封榜行不带 data-sid，不参与轮询，得分也不会被补出来。 */}}
<script>
(function () {
  var LABELS = {{judgeLabelsJSON}};
  var CLASSES = {{judgeClassesJSON}};
  var tbl = document.getElementById('result-tab');
  if (!tbl || !tbl.tBodies.length) return;
  function findRow(id) {
    var rows = tbl.tBodies[0].rows;
    for (var i = 0; i < rows.length; i++) {
      if (rows[i].getAttribute('data-sid') === id) return rows[i];
    }
    return null;
  }
  // 得分放在结果链接的 title 上：悬停可见，不占列宽。
  function updateScore(cell, res, raw) {
    var link = cell.querySelector('a');
    if (!link) return;
    var pct = parseFloat(raw);
    var title = '查看判题信息';
    if (res > 4 && !isNaN(pct) && pct <= 99) title += '（得分 ' + pct + '）';
    link.title = title;
  }
  function refresh(id, delay) {
    var x = new XMLHttpRequest();
    x.onreadystatechange = function () {
      if (x.readyState !== 4 || x.status !== 200) return;
      var row = findRow(id);
      if (!row) return;
      var ra = x.responseText.split(',');
      var res = parseInt(ra[0], 10);
      row.cells[4].textContent = ra[1];
      row.cells[5].textContent = ra[2];
      var span = row.cells[3].firstChild;
      span.textContent = LABELS[res] || res;
      span.className = CLASSES[res] || '';
      updateScore(row.cells[3], res, ra[4]);
      if (res < 4 || res > 14) setTimeout(function () { refresh(id, delay * 1.5); }, delay);
    };
    x.open('GET', 'status-ajax.php?solution_id=' + id, true);
    x.send();
  }
  var rows = tbl.tBodies[0].rows;
  for (var i = 0; i < rows.length; i++) {
    var sid = rows[i].getAttribute('data-sid');
    if (!sid) continue;
    var res = parseInt(rows[i].getAttribute('data-result'), 10);
    if (res < 4 || res > 14) refresh(sid, 1000);
  }
})();
</script>
</div>{{end}}

{{/* statusPagination 是 keyset 分页，对应 template/syzoj/status.php:124-143。

   PHP 只有三个链接：Top（回到最新）、Prev（更早，top=prevtop 或
   top+50）、Next（更新，top=bottom 并记下 prevtop）。刻意不显示总条数
   —— PHP 也不统计，编一个总数出来反而与 PHP 页面不一致。 */}}
{{define "statusPagination"}}
<div class="ui pagination menu">
<a class="item" href="{{.TopHref}}">Top</a>
<a class="item" href="{{.PrevHref}}">Prev</a>
{{with .NextHref}}<a class="item" href="{{.}}">Next</a>{{end}}
</div>
{{end}}
{{/* contestRank 与 contestRankOI 是同一个视图模型的两种表头。

   对应 template/syzoj/contestrank.php 与 contestrank-oi.php。两者的行、列、
   首杀角标、错题底色完全一致，差别只有：OI 多一列总分，且已通过格子里显示
   单题得分（pass_rate×100）而不是错题数。抽成 contestRankProblems 共用，
   免得两处各写一遍改起来漏。 */}}
{{define "contestRankProblems"}}
<table class="ui very basic center striped aligned table" style="margin:30px">
<thead>
<tr class="active">
<td class="text-center">排名</td>
<td class="text-center">用户</td>
<td class="text-center">昵称</td>
<td class="text-center">解题数</td>
<td class="text-center">罚时</td>
{{if .OI}}<td class="text-center">总分</td>{{end}}
{{range $i, $l := .Letters}}{{if $.Review}}<th><a href="problem.php?id={{index $.ProblemIDs $i}}">{{$l}}</a></th>{{else}}<th><a href="problem.php?cid={{$.CID}}&pid={{$i}}">{{$l}}</a></th>{{end}}{{end}}
<th></th>
</tr>
</thead>
<tbody>
{{$view := .}}
{{range .Rows}}
<tr>
<td>
{{if .Rank}}
{{if eq .Rank 1}}<div class="ui yellow ribbon label">{{else if eq .Rank 2}}<div class="ui ribbon label">{{else if eq .Rank 3}}<div class="ui brown ribbon label" style="background-color: #C47222 !important;">{{else}}<div>{{end}}{{.Rank}}</div>
{{else}}*{{end}}
</td>
{{if and $view.ViewUser (eq $view.ViewUser .UserID)}}
<td bgcolor="#ffff77">
{{else}}
<td>
{{end}}
<a name="{{.UserID}}" href="userinfo.php?user={{.UserID}}">{{.UserID}}</a>
</td>
<td><a href="userinfo.php?user={{.UserID}}">{{.Nick}}</a></td>
<td><span class="score"><a href="status.php?user_id={{.UserID}}&cid={{$view.CID}}">{{.Solved}}</a></span></td>
<td>{{.Time}}</td>
{{if $view.OI}}<td class="ui small header"><a href="status.php?user_id={{.UserID}}&cid={{$view.CID}}">{{.Total}}</a></td>{{end}}
{{range .Cells}}
<td{{if .BackStyle}} style="background: {{.BackStyle}};{{if .First}} position:relative;{{end}}"{{end}}>
{{if .Solved}}
{{if .First}}<div style="position:absolute;width:30%;margin-top:5%;margin-right:5%;height:30%;right:0px;top:0px;"><font color="red">※1st</font></div>{{end}}
{{if $view.OI}}<span class="score score_10">{{.Point}}</span>
{{else if .WA}}<span class="score score_10">+{{.WA}}</span>
{{else}}<span class="score score_10">+</span>{{end}}
<div class="submit_time">{{.ACS}}</div>
{{else if .WA}}<span class="score score_0">{{if $view.OI}}{{.Point}}{{else}}-{{.WA}}{{end}}</span>{{end}}
</td>
{{end}}
</tr>
{{end}}
</tbody>
</table>
{{end}}

{{define "contestRank"}}
<div style="margin-bottom:40px;" class="padding">
<h2 class="rank-title">Contest{{.CID}} - {{.Title}}
<a class="ui small blue button" href="contestrank.xls.php?cid={{.CID}}">下载</a>
<a class="ui small green button" href="contestrank4.php?cid={{.CID}}">复盘</a>
<button class="ui small yellow button" onclick='$("tr[class!=active]").toggle();'>显示/隐藏</button>
</h2>
{{if .Locked}}<div class="ui info message">The board has been locked.</div>{{end}}
</div>
<div id="rankdiv" class="padding">
{{if .Rows}}
{{template "contestRankProblems" .}}
{{else}}
<div style="background-color:#fff;height:18px;margin-top:-18px;"></div>
<div class="ui placeholder segment" style="margin-top:0px;">
<div class="ui icon header"><i class="ui file icon" style="margin-bottom:20px;"></i>暂无选手提交</div>
</div>
{{end}}
{{if .Absent}}
<div style="background-color:#fff;height:18px;margin-top:-18px;"></div>
<div class="ui segment" style="margin:50px;">
缺席
<div class="ui list">
{{range .Absent}}<a href="userinfo.php?user={{.UserID}}">{{.UserID}}</a>&nbsp;{{end}}
</div>
</div>
{{end}}
</div>
{{end}}

{{define "contestRankOI"}}
<div style="margin-bottom:40px;" class="padding">
<h2 class="rank-title">Contest{{.CID}} -- {{.Title}}
<a class="ui small blue button" href="contestrank-oi.php?spa&amp;down&amp;cid={{.CID}}">下载</a>
<a class="ui small green button" href="contestrank5.php?cid={{.CID}}">复盘</a>
<button class="ui small yellow button" onclick='$("tr[class!=active]").toggle();'>显示/隐藏</button>
</h2>
{{if .Locked}}<div class="ui info message">The board has been locked.</div>{{end}}
</div>
<div id="rankdiv" class="padding">
{{if .Rows}}
{{template "contestRankProblems" .}}
{{else}}
<div style="background-color:#fff;height:18px;margin-top:-18px;"></div>
<div class="ui placeholder segment" style="margin-top:0px;">
<div class="ui icon header"><i class="ui file icon" style="margin-bottom:20px;"></i>暂无选手提交</div>
</div>
{{end}}
{{if .Absent}}
<div style="background-color:#fff;height:18px;margin-top:-18px;"></div>
<div class="ui segment" style="margin:50px;">
缺席
<div class="ui list">
{{range .Absent}}<a href="userinfo.php?user={{.UserID}}">{{.UserID}}</a>&nbsp;{{end}}
</div>
</div>
{{end}}
</div>
{{end}}

{{/* 补题榜 contestrank4.php / contestrank5.php。

   和正式榜共用 contestRankProblems 表格，差别有三处，都是照抄部署版模板：
   1. 标题后面缀"补题榜"，右上角只有"返回"按钮指回正式榜（正式榜才有下载/复盘）。
      template/syzoj/contestrank4.php:41-43。
   2. 题头链到 problem.php?id=<problem_id> 而不是 problem.php?cid=&pid=<题号>。
   3. 没有"缺席"区块 —— 部署版补题榜不查缺席名单。 */}}
{{define "contestRankReviewACM"}}
<div style="margin-bottom:40px;" class="padding">
<h2 class="rank-title">Contest{{.CID}} - {{.Title}} - 补题榜
<button class="ui small yellow button" onclick='$("tr[class!=active]").toggle();'>显示/隐藏</button>
<a class="ui small blue button" href="{{.BackURL}}">返回</a>
</h2>
{{if .Locked}}<div class="ui info message">The board has been locked.</div>{{end}}
</div>
<div id="rankdiv" class="padding">
{{if .Rows}}
{{template "contestRankProblems" .}}
{{else}}
<div style="background-color:#fff;height:18px;margin-top:-18px;"></div>
<div class="ui placeholder segment" style="margin-top:0px;">
<div class="ui icon header"><i class="ui file icon" style="margin-bottom:20px;"></i>暂无选手提交</div>
</div>
{{end}}
</div>
{{end}}

{{define "contestRankReviewOI"}}
<div style="margin-bottom:40px;" class="padding">
<h2 class="rank-title">Contest{{.CID}} -- {{.Title}} -- 补题榜
<button class="ui small yellow button" onclick='$("tr[class!=active]").toggle();'>显示/隐藏</button>
<a class="ui small blue button" href="{{.BackURL}}">返回</a>
</h2>
{{if .Locked}}<div class="ui info message">The board has been locked.</div>{{end}}
</div>
<div id="rankdiv" class="padding">
{{if .Rows}}
{{template "contestRankProblems" .}}
{{else}}
<div style="background-color:#fff;height:18px;margin-top:-18px;"></div>
<div class="ui placeholder segment" style="margin-top:0px;">
<div class="ui icon header"><i class="ui file icon" style="margin-bottom:20px;"></i>暂无选手提交</div>
</div>
{{end}}
</div>
{{end}}

{{define "userInfoError"}}<div class="ui negative icon message">
<i class="remove icon"></i>
<div class="content">
<div class="header">{{.Message}}</div>
<p><a href="javascript:history.go(-1)">返回</a></p>
</div>
</div>{{end}}

{{define "userInfo"}}
<div class="ui grid">
<div class="row">
<div class="five wide column">
<div class="ui card" id="user_card">
<img src="{{.AvatarURL}}" alt="">
<div class="content">
<div class="header">{{.Nick}}</div>
<div class="meta">{{.School}}{{.GroupName}}</div>
</div>
<table class="table">
<tbody>
<tr><th>等级</th><td>{{.Level}}</td><td>距离 {{.NextLevel}} 还需AC {{.LevelNeed}}题</td></tr>
<tr><th>通过</th><td>{{.AC}}</td><td>排名 {{.Rank}}</td></tr>
<tr><th>做题获得{{msgCoin}}</th><td>{{.CoinEarned}}</td></tr>
<tr><th>老师奖励{{msgCoin}}</th><td>{{.CoinBonus}}</td></tr>
<tr><th>已消耗{{msgCoin}}</th><td>{{.CoinSpent}}</td></tr>
<tr><th>当前{{msgCoin}}余额</th><td>{{.CoinTotal}}</td></tr>
</tbody>
</table>
{{if .Email}}<div><a href="mailto:{{.Email}}">{{.Email}}</a></div>{{end}}
</div>
</div>
</div>

<div class="eleven wide column">

<div class="row"><div class="column">
<h4 class="ui top attached block header">通过的题目</h4>
<div class="ui bottom attached segment">
{{if .ACProblems}}
<div class="value">{{len .ACProblems}} AC</div>
{{range .ACProblems}}<a class="ui {{if .ViewerPassed}}green{{else}}red{{end}} basic label" href="problem.php?id={{.ProblemID}}">{{.ProblemID}}</a>
{{end}}
{{else}}<div class="ui placeholder segment">暂无通过的题目</div>{{end}}
</div>
</div></div>

<div class="row"><div class="column">
<h4 class="ui top attached block header">未通过的题目</h4>
<div class="ui bottom attached segment">
{{if .BadProblems}}{{range .BadProblems}}<a class="ui basic label" href="problem.php?id={{.ProblemID}}">{{.ProblemID}}</a>
{{end}}{{else}}<div class="ui placeholder segment">暂无未通过的题目</div>{{end}}
</div>
</div></div>

<div class="row"><div class="column">
<h4 class="ui top attached block header">提交统计</h4>
<div class="ui bottom attached segment">
{{if .SubChart}}
<table class="ui very basic table">
<thead><tr><th>日期</th><th>提交数</th></tr></thead>
<tbody>
{{range .SubChart}}<tr><td>{{.Date}}</td><td>{{.Count}}</td></tr>
{{end}}</tbody>
</table>
<div>单日最多 {{.MaxSubCount}} 次</div>
{{else}}<div class="ui placeholder segment">暂无提交</div>{{end}}
</div>
</div></div>

<div class="row"><div class="column">
<h4 class="ui top attached block header">结果分布</h4>
<div class="ui bottom attached segment">
{{if .ResultStats}}
<table class="ui very basic table">
<thead><tr><th>结果</th><th>次数</th></tr></thead>
<tbody>
{{range .ResultStats}}<tr><td>{{.Label}}</td><td>{{.Count}}</td></tr>
{{end}}</tbody>
</table>
{{else}}<div class="ui placeholder segment">暂无已判定的提交</div>{{end}}
</div>
</div></div>

<div class="row"><div class="column">
<h4 class="ui top attached block header">{{msgContestWord}}</h4>
<div class="ui bottom attached segment">
<table class="ui striped table">
<thead><tr><th>{{msgNum}}</th><th>{{msgTitle}}</th><th>{{msgStartTime}}</th><th>{{msgSubmit}}</th><th>{{msgAC}}</th></tr></thead>
<tbody>
{{range $i, $c := .Contests}}<tr><td>{{add $i 1}}</td><td><a href="contestrank.php?cid={{$c.ContestID}}#{{$.User}}" target="_blank">{{$c.Title}}</a></td><td>{{$c.StartTime}}</td><td>{{$c.Tried}}</td><td>{{$c.AC}}</td></tr>
{{end}}
<tr><th></th><th></th><th></th><th>{{.TotalTry}}</th><th>{{.TotalContAC}}</th></tr>
</tbody>
</table>
</div>
</div></div>

{{if .IsAdmin}}
<div class="row"><div class="column">
<h4 class="ui top attached block header">近期登录日志</h4>
<div class="ui bottom attached segment">
<table class="ui striped table">
<thead><tr><th>UserID</th><th>Password</th><th>IP</th><th>Time</th></tr></thead>
<tbody>
{{range .LoginLog}}<tr><td>{{.UserID}}</td><td>{{.Password.String}}</td><td>{{.IP.String}}</td><td>{{if .Time.Valid}}{{.Time.Time.Format "2006-01-02 15:04:05"}}{{end}}</td></tr>
{{end}}
</tbody>
</table>
</div>
</div></div>
{{end}}

<div class="row"><div class="column">
<a href="/status.php?user_id={{.User}}">提交列表</a>
</div></div>

</div>
</div>
</div>
{{end}}

{{/* problemStatus 是单题提交状态页，对应 template/syzoj/problemstatus.php。

   PHP 用 Chart.js 画结果分布饼图，并另有一段 legend 显示提交/人数/AC 人数。
   Go 侧与 userinfo 一样退化成两张表（统计 + 结果分布），信息量一致而不依赖 JS。 */}}
{{define "problemStatus"}}
<div class="ui container">
<div class="ui grid">
<div class="row">
<div class="four wide column">
<h4 class="ui top attached block header">统计</h4>
<div class="ui bottom attached segment">
<table class="ui very basic table">
<tbody>
<tr><th>{{(index .Stats 0).Label}}</th><td>{{(index .Stats 0).Value}}</td></tr>
<tr><th>{{(index .Stats 1).Label}}</th><td>{{(index .Stats 1).Value}}</td></tr>
<tr><th>{{(index .Stats 2).Label}}</th><td>{{(index .Stats 2).Value}}</td></tr>
</tbody></table>
{{if .Pie}}
<table class="ui very basic table">
<thead><tr><th>结果</th><th>次数</th></tr></thead>
<tbody>
{{range .Pie}}<tr><td>{{.Label}}</td><td>{{.Count}}</td></tr>
{{end}}</tbody></table>
{{end}}
</div>
</div>
<div class="twelve wide column">
<h4 class="ui top attached block header">排名</h4>
<div class="ui bottom attached segment">
<table class="ui very basic table">
<thead><tr><th>排名</th><th>提交编号</th><th>用户</th><th>内存</th><th>时间</th><th>语言</th><th>代码长度</th><th>提交时间</th></tr></thead>
<tbody>
{{range .Solutions}}<tr>
<td>{{.Rank}}</td>
<td>{{.SolutionTag}}</td>
<td><a href="userinfo.php?user={{.UserID}}">{{.UserID}}</a></td>
<td>{{.Memory}}</td>
<td>{{.Time}}</td>
<td>{{if .LangHref}}<a href="{{.LangHref}}" target="_blank">{{.Language}}</a>{{else}}{{.Language}}{{end}}</td>
<td>{{.CodeLength}}</td>
<td>{{.InDate}}</td>
</tr>
{{end}}</tbody></table>
<div class="ui pagination menu">
<a class="item" href="problemstatus.php?id={{.ProblemID}}">主页</a>
{{if .PrevHref}}<a class="item" href="{{.PrevHref}}">上一页</a>{{end}}
{{if .NextHref}}<a class="item" href="{{.NextHref}}">下一页</a>{{end}}
</div>
</div>
</div>
</div>
{{if .Recommends}}
<div class="row"><div class="column">
<h4 class="ui top attached block header">相关题目</h4>
<div class="ui bottom attached segment">
{{range .Recommends}}<a class="ui basic label" href="problem.php?id={{.}}">{{.}}</a>
{{end}}
</div>
</div></div>
{{end}}
</div>
</div>
{{end}}

{{/* highlightAssets 引入本地 highlight.js（v11.12.0）与 GitHub 明/暗主题，
   并只对当前 bundle 收录的语言调用高亮（未收录的保持纯文本，避免控制台告警）。

   资源随二进制内嵌在 /assets/ 下，不依赖 CDN；delphi 是 common 构建之外的
   额外语法，单独引入。 */}}
{{define "highlightAssets"}}
<link rel="stylesheet" href="{{asset "/assets/highlight-github.min.css"}}">
<link rel="stylesheet" href="{{asset "/assets/highlight-github-dark.min.css"}}" media="(prefers-color-scheme: dark)">
<script src="{{asset "/assets/highlight.min.js"}}" defer></script>
<script src="{{asset "/assets/highlight-delphi.min.js"}}" defer></script>
<script defer>
document.addEventListener("DOMContentLoaded", function () {
  if (!window.hljs) { return; }
  document.querySelectorAll('pre code[class*="language-"]').forEach(function (el) {
    var m = /language-([\w-]+)/.exec(el.className);
    if (m && hljs.getLanguage(m[1])) { hljs.highlightElement(el); }
  });
});
</script>
{{end}}

{{/* showsourcePage 是源代码查看页，对应 template/syzoj/showsource.php。

   PHP 用 SyntaxHighlighter 高亮 + ob 抓取的注释头；Go 用本地 highlight.js，
   内容由 handler 拼好并交给 html/template 转义（源码/昵称里的尖括号不再裸奔）。 */}}
{{define "showsourcePage"}}
<div class="padding">
{{if .OK}}
{{template "highlightAssets"}}
<table class="ui celled table" style="margin-bottom:1em">
<tbody>
<tr>
<th style="width:6em">题目</th><td><a href="/problem.php?id={{.Meta.Problem}}">{{.Meta.Problem}}</a></td>
<th style="width:6em">用户</th><td>{{.Meta.User}}{{if .Meta.Nick}} [{{.Meta.Nick}}]{{end}}</td>
</tr>
<tr>
<th>语言</th><td>{{.Meta.Language}}</td>
<th>结果</th><td><span class="{{judgeResultClass .Meta.Result}}">{{judgeResultLabel .Meta.Result}}</span></td>
</tr>
{{if .Meta.ShowTime}}
<tr>
<th>耗时</th><td>{{.Meta.Time}} ms</td>
<th>内存</th><td>{{.Meta.Memory}} kb</td>
</tr>
{{end}}
</tbody>
</table>
<pre><code class="language-{{.Lang}}">{{.Source}}</code></pre>
{{if or .Reinfo .Ceinfo}}
<h3 class="ui header">判题信息</h3>
<p>提交 ID: {{.SID}} | 得分: {{.Mark}}</p>
{{if .Ceinfo}}
<h4 class="ui header">编译错误</h4>
<div class="ui segment"><pre>{{.Ceinfo}}</pre></div>
{{end}}
{{if .Reinfo}}
<h4 class="ui header">各用例结果</h4>
<div class="ui segment"><pre>{{.Reinfo}}</pre></div>
{{end}}
{{end}}
{{else}}I am sorry, You could not view this code!{{end}}
</div>
{{end}}

{{/* ranklist 是全局排名页，对应 template/syzoj/ranklist.php。

   顶部同一行：左侧 scope 导航（日/周/月/上月/年），右侧 prefix、group_name
   两个查找框（.ranklist-toolbar，见 hustoj.css）。表格固定 7 列、整表居中、
   列宽按最长内容比例分配（colgroup + table-layout:fixed，总宽见 hustoj.css
   的 --ranklist-table-width）。分页条按 page_size 分段拼接 start 偏移。
   starred>0 的账号在用户名前后各加一颗星，与 PHP :110-118 一致。 */}}
{{define "ranklist"}}
<div class="padding">
<div class="ranklist-toolbar">
<div class="ranklist-scopes">
<a href="ranklist.php?scope=d" class="ui mini button">{{msgRanklistDay}}</a>
<a href="ranklist.php?scope=w" class="ui mini button primary">{{msgRanklistWeek}}</a>
<a href="ranklist.php?scope=m" class="ui mini button red">{{msgRanklistMonth}}</a>
<a href="ranklist.php?scope=m&pre=1" class="ui mini button green">{{msgRanklistLastMonth}}</a>
<a href="ranklist.php?scope=y" class="ui mini button violet">{{msgRanklistYear}}</a>
</div>
<form action="ranklist.php" class="ranklist-search" method="get" role="form">
<div class="field"><i class="search icon"></i><input name="prefix" placeholder="{{msgUser}}" type="text" value="{{.Prefix}}"><button class="ui mini button" type="submit">{{msgSearch}}</button></div>
<div class="field"><i class="search icon"></i><input name="group_name" placeholder="{{msgGroupName}}" type="text" value="{{.GroupName}}"><button class="ui mini button" type="submit">{{msgSearch}}</button></div>
</form>
</div>
<div class="ranklist-table-wrap">
<table id="ranklist-tab" class="ui very basic center aligned table">
<colgroup>
<col style="width: 7%">
<col style="width: 18%">
<col style="width: 25%">
<col style="width: 22%">
<col style="width: 8%">
<col style="width: 8%">
<col style="width: 12%">
</colgroup>
<thead>
<tr>
<th>{{msgNumber}}</th>
<th>{{msgUser}}</th>
<th>{{msgNick}}</th>
<th>{{msgGroupName}}</th>
<th>{{msgSolved}}</th>
<th>{{msgSubmit}}</th>
<th>{{msgRatio}}</th>
</tr>
</thead>
<tbody>
{{range .Rows}}<tr>
<td>{{.Rank}}</td>
<td>{{if .Starred}}⭐{{end}}<a href="userinfo.php?user={{.UserID}}">{{.UserID}}</a>{{if .Starred}}<span title="用同名账户给hustoj项目加星，可以点亮此星">⭐</span>{{end}}</td>
<td><div class="center">{{.Nick}}</div></td>
<td><div class="center">{{.GroupName}}</div></td>
<td><div class="center"><a href="status.php?user_id={{.UserID}}&jresult=4">{{.Solved}}</a></div></td>
<td><div class="center"><a href="status.php?user_id={{.UserID}}">{{.Submit}}</a></div></td>
<td>{{.Ratio}}</td>
</tr>
{{end}}
</tbody>
</table>
</div>
<br>
<div style="margin-bottom: 30px;">
<div style="text-align: center;">
<div class="ui pagination" style="box-shadow: none;">
{{range .Pages}}<a class="ui button" href="./ranklist.php?start={{.Start}}{{if $.Scope}}&scope={{$.Scope}}{{end}}">{{.Label}}</a>{{end}}
</div>
</div>
</div>
</div>
{{end}}

{{/* indexPage 是首页，对应 template/syzoj/index.php。

    新闻列表、本月之星、最近题目/比赛、echarts 图表。 */}}
{{define "indexPage"}}
<div class="padding">
<div class="ui three column grid">
<div class="eleven wide column">
<h4 class="ui top attached block header"><i class="ui info icon"></i>新闻</h4>
<div class="ui bottom attached segment">
<table class="ui very basic table">
<thead><tr><th>标题</th><th>时间</th></tr></thead>
<tbody>
{{range .NewsList}}<tr>
<td><a href="viewnews.php?id={{.NewsID}}">{{.Title}}</a></td>
<td>{{.Time}}</td>
</tr>
{{end}}
</tbody>
</table>
</div>
{{if gt .Pages 1}}
<div style="display:inline;">
<nav class="center">
<ul class="pagination pagination-sm">
<li class="page-item"><a href="index.php?page=1">&lt;&lt;</a></li>
<li class="page-item"><a href="index.php?page={{if eq .Page 1}}{{.Page}}{{else}}{{sub .Page 1}}{{end}}">&lt;</a></li>
{{range $i := until .Pages}}{{if and (ge $i $.SPage) (le $i $.EPage)}}
<li class="{{if eq $i $.Page}}active {{end}}page-item"><a title="go to page" href="index.php?page={{$i}}">{{$i}}</a></li>
{{end}}{{end}}
<li class="page-item"><a href="index.php?page={{if eq .Page .Pages}}{{.Page}}{{else}}{{add .Page 1}}{{end}}">&gt;</a></li>
<li class="page-item"><a href="index.php?page={{.Pages}}">&gt;&gt;</a></li>
</ul>
</nav>
</div>
{{end}}

{{if .MonthStars}}
<h4 class="ui top attached block header"><i class="ui star icon"></i>本月之星</h4>
<div class="ui bottom attached segment">
<table class="ui very basic center aligned table" style="table-layout: fixed;">
<tbody>
{{range .MonthStars}}<tr>
<td><a target="_blank" href="userinfo.php?user={{.UserID}}">⭐{{.UserID}}⭐</a></td>
<td>{{.Nick}}</td>
<td>{{.AC}}</td>
</tr>
{{end}}
</tbody>
</table>
</div>
{{end}}

<h4 class="ui top attached block header"><i class="ui star icon"></i>{{.IndexNewsTitle}}</h4>
<div class="ui bottom attached segment">
<table class="ui very basic left aligned table" style="table-layout: fixed;">
<tbody>
{{range .IndexNewsList}}<tr><td>{{.Content}}</td></tr>
{{end}}
<tr><td>
<center> Recent submission : {{.Speed}} .
<div id="submission" style="width:80%;height:300px"></div>
</center>
</td></tr>
</tbody>
</table>
</div>
</div>
<div class="right floated five wide column">
<h4 class="ui top attached block header"><i class="ui rss icon"></i> 最近题目 </h4>
<div class="ui bottom attached segment">
<table class="ui very basic center aligned table">
<thead><tr><th width="60%">标题</th><th width="40%">时间</th></tr></thead>
<tbody>
{{range .RecentProblems}}<tr>
<td><a href="problem.php?id={{.ProblemID}}">{{.Title}}</a></td>
<td>{{.MaxInDate}}</td>
</tr>
{{end}}
</tbody>
</table>
</div>
<h4 class="ui top attached block header"><i class="ui search icon"></i>搜索</h4>
<div class="ui bottom attached segment">
<form action="problem.php" method="get">
<div class="ui search" style="width: 100%;">
<div class="ui left icon input" style="width: 100%;">
<input class="prompt" style="width: 100%;" type="text" placeholder="题目编号 …" name="id">
<i class="search icon"></i>
</div>
</div>
</form>
</div>
<h4 class="ui top attached block header"><i class="ui calendar icon"></i>最近比赛</h4>
<div class="ui bottom attached center aligned segment">
<table class="ui very basic center aligned table">
<thead><tr><th>比赛名称</th><th>开始时间</th></tr></thead>
<tbody>
{{range .RecentContests}}<tr>
<td><a href="contest.php?cid={{.ContestID}}">{{.Title}}</a></td>
<td>{{.StartTime}}</td>
</tr>
{{end}}
</tbody>
</table>
</div>
</div>
</div>
</div>
<script>
(function() {
  var all = {{.ChartDataAll}};
  var ac = {{.ChartDataAC}};
  var el = document.getElementById('submission');
  if (!el) return;
  if (typeof echarts === 'undefined' || !all.length) {
    el.classList.add('chart-empty');
    el.style.height = 'auto';
    el.style.minHeight = '6rem';
    el.innerHTML = '<div class="chart-empty">暂无提交趋势数据</div>';
    return;
  }
  var chart = echarts.init(el);
  var option = {
    tooltip: { trigger: 'axis', formatter: '{b0}({a0}): {c0}<br />{b1}({a1}): {c1}' },
    legend: { data: ['提交', 'AC'] },
    xAxis: { data: ac.map(function(d){return d.md;}), inverse: true },
    yAxis: [{ type: 'value', name: '提交' }],
    series: [
      { name: '提交', type: 'bar', data: all.map(function(d){return d.c;}) },
      { name: 'AC', type: 'bar', data: ac.map(function(d){return d.c;}) }
    ]
  };
  chart.setOption(option);
})();
</script>
{{end}}

{{/* portalPage 是门户页，对应 template/syzoj/portal.php。

    待办题目、我的比赛。 */}}
{{define "portalPage"}}
<div class="ui container">
<div class="ui icon message">
<div class="content">
<div class="header" style="margin-bottom: 10px;">待办题目</div>
<table class="ui very basic center aligned table">
<thead>
<tr class="toprow">
<td></td>
<td class="hidden-xs center">编号</td>
<td class="center">标题</td>
<td class="hidden-xs center">来源</td>
<td style="cursor:hand" class="center">通过</td>
<td style="cursor:hand" class="center">提交</td>
</tr>
</thead>
<tbody>
{{range $i, $p := .ProblemSet}}<tr class="{{if even $i}}evenrow{{else}}oddrow{{end}}">
<td>{{$p.Marker}}</td>
<td class="hidden-xs center">{{$p.ProblemID}}</td>
<td class="center"><a href="problem.php?id={{$p.ProblemID}}">{{$p.Title}}</a></td>
<td class="hidden-xs center">{{$p.Source}}</td>
<td class="center"><a href="status.php?problem_id={{$p.ProblemID}}&jresult=4">{{if $p.Accepted.Valid}}{{$p.Accepted.Int32}}{{else}}0{{end}}</a></td>
<td class="center"><a href="status.php?problem_id={{$p.ProblemID}}">{{if $p.Submit.Valid}}{{$p.Submit.Int32}}{{else}}0{{end}}</a></td>
</tr>
{{end}}
</tbody>
</table><br>
<table class="ui very basic center aligned table">
<thead>
<tr class="toprow" align="center">
<td>比赛编号</td>
<td>比赛名称</td>
<td>比赛状态</td>
<td>公开/私有</td>
<td>创建者</td>
</tr>
</thead>
<tbody align="center">
{{range $i, $c := .ContestSet}}<tr class="{{if even $i}}evenrow{{else}}oddrow{{end}}">
<td>{{$c.ContestID}}</td>
<td><a href="contest.php?cid={{$c.ContestID}}">{{$c.Title}}</a></td>
<td class="text-left">开始: {{$c.StartTime}} 结束: {{$c.EndTime}}</td>
<td>{{if eq $c.Private 0}}公开{{else}}私有{{end}}</td>
<td>{{$c.UserID}}</td>
</tr>
{{end}}
</tbody>
</table>
<p><a href="javascript:history.go(-1)">返回</a></p>
</div>
</div>
</div>
{{end}}

{{/* viewNewsPage 是新闻详情页，对应 template/syzoj/viewnews.php。 */}}
{{define "viewNewsPage"}}
<div class="padding">
<p class="news-meta">
<b><i class="edit icon"></i><a class="black-link" href="userinfo.php?user={{.NewsWriter}}"> {{.NewsWriter}}</a></b>
<b><i class="calendar icon"></i> {{.NewsDate}}</b>
</p>
<div class="ui existing segment">
<div id="content" class="font-content md-body">{{.NewsContent}}</div>
</div>
</div>
{{if .MathJax}}
<script>
MathJax = { tex: { inlineMath: [['$', '$'], ['\\(', '\\)']] } };
</script>
<script id="MathJax-script" async src="template/bs3/tex-chtml.js"></script>
{{end}}
{{end}}

{{/* mailPage 是邮件页，对应 template/syzoj/mail.php。 */}}
{{define "mailPage"}}
<div class="ui container">
<h2 class="ui header">邮件</h2>
{{if .ViewMail}}
<div class="ui segment">
<h3>{{.ViewMail.Title}}</h3>
<p>发件人: {{.ViewMail.FromUser}} | 收件人: {{.ViewMail.ToUser}} | 时间: {{.ViewMail.InDate}}</p>
<div>{{.ViewMail.Content}}</div>
</div>
{{end}}
<form method="post" class="ui form">
<div class="field"><label>收件人</label><input name="to_user" value="{{.ToUser}}"></div>
<div class="field"><label>标题</label><input name="title" value="{{.Title2}}"></div>
<div class="field"><label>内容</label><textarea name="content"></textarea></div>
<button class="ui button" type="submit">发送</button>
</form>
<table class="ui very basic table">
<thead><tr><th>邮件</th><th>时间</th></tr></thead>
<tbody>
{{range .MailList}}<tr>
<td><a href="mail.php?vid={{.MailID}}">{{.FromUser}}:{{.Title}}</a>{{if .NewMail}}<span class="red">New</span>{{end}}</td>
<td>{{.InDate}}</td>
</tr>
{{end}}
</tbody>
</table>
</div>
{{end}}

{{/* modifyPage 是用户信息修改页，对应 template/syzoj/modify.php。 */}}
{{define "modifyPage"}}
<div class="ui container">
<h2 class="ui header">修改用户信息</h2>
{{if .Error}}<div class="ui negative message">{{.Error}}</div>{{end}}
<form method="post" class="ui form">
<div class="field"><label>旧密码</label><input type="password" name="opassword"></div>
<div class="field"><label>新密码</label><input type="password" name="npassword"></div>
<div class="field"><label>确认新密码</label><input type="password" name="rptpassword"></div>
<div class="field"><label>昵称</label><input name="nick" value="{{.Nick}}"></div>
<div class="field"><label>学校</label><input name="school" value="{{.School}}"></div>
<div class="field"><label>邮箱</label><input name="email" value="{{.Email}}"></div>
<button class="ui button" type="submit">保存</button>
</form>
</div>
{{end}}

{{/* lostPasswordPage 是密码重置页，对应 template/syzoj/lostpassword.php。 */}}
{{define "lostPasswordPage"}}
<div class="ui container">
<h2 class="ui header">密码重置</h2>
{{if .Error}}<div class="ui negative message">{{.Error}}</div>{{end}}
<form method="post" class="ui form">
<div class="field"><label>用户名</label><input name="user_id"></div>
<div class="field"><label>邮箱</label><input name="email"></div>
<button class="ui button" type="submit">重置密码</button>
</form>
</div>
{{end}}

{{/* lostPassword2Page 是密码重置确认页，对应 template/syzoj/lostpassword2.php。 */}}
{{define "lostPassword2Page"}}
<div class="ui container">
<h2 class="ui header">密码重置</h2>
<form method="post" class="ui form">
<div class="field"><label>用户名</label><input name="user_id" value="{{.UserID}}"></div>
<div class="field"><label>重置密钥</label><input name="lost_key"></div>
<button class="ui button" type="submit">确认重置</button>
</form>
</div>
{{end}}

{{/* userSetIPPage 是设置登录 IP 页，对应 template/syzoj/user_set_ip.php。 */}}
{{define "userSetIPPage"}}
<div class="ui container">
<h2 class="ui header">设置登录 IP</h2>
<p>比赛: {{.ContestName}} (CID: {{.CID}})</p>
<div class="md-body">{{markdown .Description}}</div>
<p>开始时间: {{.StartTime}} | 结束时间: {{.EndTime}}</p>
<p>状态:
{{if .IsOver}}已结束{{else if .IsNotYet}}未开始{{else}}进行中{{end}}
| 类型: {{if .Private}}私有{{else}}公开{{end}}
</p>
{{if .Changed}}
<div class="ui positive message">已将用户 {{.UserID}} 的登录 IP 设置为 {{.IP}}</div>
{{end}}
<form method="post" class="ui form">
<input type="hidden" name="do" value="do">
<div class="field"><label>用户 ID</label><input name="user_id" value="{{.UserID}}" required></div>
<div class="field"><label>新 IP</label><input name="ip" value="{{.IP}}" placeholder="?.?.?.?" required></div>
<button class="ui button" type="submit">保存</button>
<button class="ui button" type="reset">重置</button>
</form>
</div>
{{end}}

{{/* reinfoPage 是运行错误信息页，对应 template/syzoj/reinfo.php。 */}}
{{define "reinfoPage"}}
<div class="ui container">
<h2 class="ui header">运行错误信息</h2>
<p>提交 ID: {{.SID}} | 得分: {{.Mark}}</p>
{{if .ErrorMsg}}
<div class="ui segment">
<pre>{{.ErrorMsg}}</pre>
</div>
{{else}}
<div class="ui message">无错误信息</div>
{{end}}
</div>
{{end}}

{{/* adminDashboard 是后台落地内容：分区列出可用功能。
     早期这里用 frameset + menu.php(target=main) 做左右分栏，但 frameset 嵌进
     普通 <main> 会被浏览器忽略、页面空白；现在改由布局统一渲染 adminNav，
     落地页只负责列功能，index.php 与旧的 menu.php 共用同一份内容。 */}}
{{define "adminDashboard"}}
<p>请选择要使用的功能（也可使用上方后台导航）。</p>

<div class="admin-sections">
<div class="sidebar-section">
<h3>新闻管理</h3>
<ul>
{{if .IsAdmin}}
<li><a href="/admin/news_list.php">新闻列表</a></li>
<li><a href="/admin/news_add_page.php">添加新闻</a></li>
{{end}}
</ul>
</div>

<div class="sidebar-section">
<h3>用户管理</h3>
<ul>
{{if or .IsAdmin .IsPasswordSetter}}
<li><a href="/admin/user_list.php">用户列表</a></li>
{{end}}
{{if or .IsAdmin .IsUserAdder}}
<li><a href="/admin/user_add.php">添加用户</a></li>
<li><a href="/admin/user_import.php">导入用户</a></li>
{{end}}
{{if or .IsAdmin .IsPasswordSetter}}
<li><a href="/admin/changepass.php">修改密码</a></li>
{{end}}
{{if .IsAdmin}}
<li><a href="/admin/privilege_list.php">权限列表</a></li>
<li><a href="/admin/privilege_add.php">添加权限</a></li>
{{end}}
</ul>
</div>

<div class="sidebar-section">
<h3>题目管理</h3>
<ul>
{{if or .IsAdmin .IsProblemEditor .IsContestCreator}}
<li><a href="/admin/problem_list.php">题目列表</a></li>
{{end}}
{{if or .IsAdmin .IsProblemEditor}}
<li><a href="/admin/problem_add_page.php">添加题目</a></li>
<li><a href="/admin/problem_import.php">导入题目</a></li>
<li><a href="/admin/problem_export.php">导出题目</a></li>
<li><a href="/admin/problem_copy.php">复制题目</a></li>
{{end}}
</ul>
</div>

<div class="sidebar-section">
<h3>比赛管理</h3>
<ul>
{{if or .IsAdmin .IsContestCreator}}
<li><a href="/admin/contest_list.php">比赛列表</a></li>
<li><a href="/admin/contest_add.php">添加比赛</a></li>
{{end}}
</ul>
</div>

{{if .IsAdmin}}
<div class="sidebar-section">
<h3>系统管理</h3>
<ul>
<li><a href="/admin/rejudge.php">重新判题</a></li>
<li><a href="/admin/solution_statistics.php">解题统计</a></li>
<li><a href="/admin/ranklist_export.php">导出榜单</a></li>
<li><a href="/online.php">在线用户</a></li>
<li><a href="/admin/help.php">后台帮助</a></li>
</ul>
</div>
{{end}}
</div>
{{end}}

{{/* adminIndexPage 是后台首页，对应 admin/index.php。 */}}
{{define "adminIndexPage"}}{{template "adminDashboard" .}}{{end}}

{{/* adminMenuPage 对应旧的 admin/menu.php：内容与首页一致（不再用 frameset）。 */}}
{{define "adminMenuPage"}}{{template "adminDashboard" .}}{{end}}

{{/* adminHeaderPage 是后台头部，对应 admin/admin-header.php。 */}}
{{define "adminHeaderPage"}}
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>{{.Title}}</title>
</head>
<body>
<p>后台管理头部</p>
</body>
</html>
{{end}}

{{/* adminNewsListPage 对应 admin/news_list.php。 */}}
{{define "adminNewsListPage"}}
<hr>
<center><h3>新闻-列表</h3></center>
<div class="padding">
<center>
<form action="/admin/news_list.php" method="get" class="form-search form-inline">
<input type="text" name="keyword" class="form-control search-query" value="{{.Keyword}}" placeholder="标题, 内容">
<button type="submit" class="form-control">搜索</button>
</form>
</center>
<center>
<table width="100%" border="1" style="text-align:center;">
<tr style="height:22px;">
<td>ID</td><td>TITLE</td><td>UPDATE</td><td>NOW</td><td>COPY</td><td>IMPORTANCE</td><td>MENU</td>
</tr>
{{range .Rows}}
<tr style="height:22px;" news_id="{{.NewsID}}">
<td>{{.NewsID}}</td>
<td><a href="/admin/news_edit.php?id={{.NewsID}}">{{.Title}}</a></td>
<td>{{.Time}}</td>
<td><a href="/admin/news_df_change.php?id={{.NewsID}}">{{if .On}}<span class="green">On</span>{{else}}<span class="red">Off</span>{{end}}</a></td>
<td><a href="/admin/news_add_page.php?cid={{.NewsID}}">Copy</a></td>
<td fd="importance">{{.Importance}}</td>
<td fd="menu">{{if .MenuYes}}YES{{else}}NO{{end}}</td>
</tr>
{{end}}
</table>
</center>
{{if .ShowPagination}}
<div style="display:inline;">
<nav class="center">
<ul class="pagination pagination-sm">
<li class="page-item"><a href="/admin/news_list.php?page=1">&lt;&lt;</a></li>
<li class="page-item"><a href="/admin/news_list.php?page={{.PrevPage}}">&lt;</a></li>
{{range .Pages}}<li class="{{if .Active}}active {{end}}page-item"><a title="go to page" href="/admin/news_list.php?page={{.Num}}">{{.Num}}</a></li>{{end}}
<li class="page-item"><a href="/admin/news_list.php?page={{.NextPage}}">&gt;</a></li>
<li class="page-item"><a href="/admin/news_list.php?page={{.TotalPages}}">&gt;&gt;</a></li>
</ul>
</nav>
</div>
{{end}}
</div>
{{end}}

{{/* adminNewsAddPage 对应 admin/news_add_page.php。 */}}
{{define "adminNewsAddPage"}}
<hr>
<center><h3>新闻-添加</h3></center>
<div class="padding">
<form method="post" action="/admin/news_add.php">
{{template "csrfInput" .}}
<p align="left">
<label class="col control-label">标题</label>
<input class="input input-large" style="width:100%;" size="71" value="{{.Title}}" type="text" name="title" id="title">
<input type="submit" class="btn btn-success" value="保存" name="submit">
</p>
<p align="left">
<label class="col control-label">首页菜单显示
<input style="display:inline-block;" type="checkbox" name="showInMenu">
</label>
</p>
<p align="left">
<textarea class="md-source" name="content" rows="41" style="width:100%;">{{.Content}}</textarea>
</p>
<p><center><input type="submit" value="保存" name="submit"></center></p>
</form>
</div>
{{template "markdownEditorScript"}}
{{end}}

{{/* adminNewsEditPage 对应 admin/news_edit.php。 */}}
{{define "adminNewsEditPage"}}
<hr>
<center><h3>新闻-编辑</h3></center>
<div class="padding">
<form method="post" action="/admin/news_edit.php">
{{template "csrfInput" .}}
<input type="hidden" name="news_id" value="{{.NewsID}}">
<p align="left">
<label class="col control-label">标题</label>
<input type="text" name="title" size="71" value="{{.Title}}">
</p>
<p align="left">
<label class="col control-label">首页菜单显示
<input style="display:inline-block;" type="checkbox" name="showInMenu" {{if .ShowInMenu}}checked{{end}}>
</label>
</p>
<p align="left">
<textarea class="md-source" name="content" rows="41" style="width:100%;">{{.Content}}</textarea>
</p>
<p><center><input type="submit" value="保存" name="submit"></center></p>
</form>
</div>
{{template "markdownEditorScript"}}
{{end}}

{{/* adminPrivilegeListPage 对应 admin/privilege_list.php。 */}}
{{define "adminPrivilegeListPage"}}
<hr><center><h3>用户-权限-列表</h3></center>
<div class="padding" data-csrf="{{csrfToken .Token}}">
<center>
<form action="/admin/privilege_list.php" method="get" class="form-search form-inline">
<input type="text" name="keyword" class="form-control search-query" value="{{.Keyword}}" placeholder="用户ID, 权限">
<button type="submit" class="form-control">搜索</button>
</form>
</center>
<center>
<table width="100%" border="1" style="text-align:center;">
<tr><td>ID</td><td>PRIVILEGE</td><td>REMOVE</td></tr>
{{range .Rows}}
<tr>
<td>{{.UserID}}</td>
<td>{{.RightText}}</td>
<td>{{if .CanDelete}}<button type="button" class="privilege-delete btn btn-link" data-uid="{{.UserID}}" data-rightstr="{{.RightText}}">Delete</button>{{else}}Can't suicide{{end}}</td>
</tr>
{{end}}
</table>
</center>
{{if .ShowPagination}}
<div style="display:inline;"><nav class="center"><ul class="pagination pagination-sm">
<li class="page-item"><a href="/admin/privilege_list.php?page=1">&lt;&lt;</a></li>
<li class="page-item"><a href="/admin/privilege_list.php?page={{.PrevPage}}">&lt;</a></li>
{{range .Pages}}<li class="{{if .Active}}active {{end}}page-item"><a title="go to page" href="/admin/privilege_list.php?page={{.Num}}">{{.Num}}</a></li>{{end}}
<li class="page-item"><a href="/admin/privilege_list.php?page={{.NextPage}}">&gt;</a></li>
<li class="page-item"><a href="/admin/privilege_list.php?page={{.TotalPages}}">&gt;&gt;</a></li>
</ul></nav></div>
{{end}}
</div>
<script>
document.querySelectorAll('.privilege-delete').forEach(function (btn) {
  btn.addEventListener('click', function () {
    if (!window.confirm('Delete this privilege?')) return;
    var token = document.querySelector('.padding[data-csrf]').getAttribute('data-csrf');
    var body = new URLSearchParams({ uid: btn.dataset.uid, rightstr: btn.dataset.rightstr, csrf_token: token });
    fetch('/admin/privilege_delete.php', { method: 'POST', body: body }).then(function (resp) {
      if (resp.ok) { window.location.reload(); }
      else { resp.text().then(function (t) { window.alert(t || 'Delete failed'); }); }
    }).catch(function () { window.alert('Delete failed'); });
  });
});
</script>
{{end}}

{{/* adminPrivilegeAddPage 对应 admin/privilege_add.php。 */}}
{{define "adminPrivilegeAddPage"}}
<hr><center><h3>用户-权限-添加</h3></center>
<div class="padding">
{{if .Message}}<center><h4 class="text-danger">{{.Message}}</h4></center>{{end}}
<form method="post" action="/admin/privilege_add.php" class="form-horizontal">
{{template "csrfInput" .}}
<div class="form-group">
<label>用户ID</label>
<input name="user_id" class="form-control" value="{{.UserID}}" type="text" required>
</div>
<div class="form-group">
<label>权限类型</label>
<select name="rightstr">
{{range .Options}}<option value="{{.}}">{{.}}</option>{{end}}
</select>
<input type="text" class="form-control" name="valuestr" value="true">
</div>
<input type="hidden" name="do" value="do">
<center><button type="submit" class="btn btn-default">保存</button></center>
</form>
<br>
<form method="post" action="/admin/privilege_add.php" class="form-horizontal">
{{template "csrfInput" .}}
<div class="form-group"><label>用户ID</label><input name="user_id" class="form-control" value="{{.UserID}}" type="text" required></div>
<div class="form-group"><label>比赛ID</label><input name="rightstr" class="form-control" placeholder="比赛ID*"></div>
<input type="hidden" name="do" value="do">
<center><button type="submit" name="contest" value="do" class="btn btn-default">保存</button></center>
</form>
<br>
<form method="post" action="/admin/privilege_add.php" class="form-horizontal">
{{template "csrfInput" .}}
<div class="form-group"><label>用户ID</label><input name="user_id" class="form-control" value="{{.UserID}}" type="text" required></div>
<div class="form-group"><label>题目ID</label><input name="rightstr" class="form-control" placeholder="题目ID*"></div>
<input type="hidden" name="do" value="do">
<center><button type="submit" name="psv" value="do" class="btn btn-default">保存</button></center>
</form>
</div>
{{end}}

{{/* adminChangePassPage 对应 admin/changepass.php。 */}}
{{define "adminChangePassPage"}}
<hr><center><h3>用户-设置密码</h3></center>
<div class="padding">
{{if .Message}}<center><h4 class="text-danger">{{.Message}}</h4></center>{{end}}
<form action="/admin/changepass.php" method="post" class="form-horizontal">
{{template "csrfInput" .}}
<div class="form-group"><label>用户ID</label><input name="user_id" class="form-control" value="{{.UserID}}" type="text" required></div>
<div class="form-group"><label>密码</label><input name="passwd" class="form-control" type="password" autocomplete="off" required></div>
<input type="hidden" name="do" value="do">
<center><button type="submit" class="btn btn-default">保存</button></center>
</form>
</div>
{{end}}

{{/* adminUserSetIPPage 对应 admin/user_set_ip.php。 */}}
{{define "adminUserSetIPPage"}}
<hr><center><h3>设置登录 IP</h3></center>
<div class="container">
{{if .Message}}<center><h4 class="text-danger">{{.Message}}</h4></center>{{end}}
<form action="/admin/user_set_ip.php" method="post" class="form-horizontal">
{{template "csrfInput" .}}
<div class="form-group"><label>用户ID</label><input name="user_id" class="form-control" value="{{.UserID}}" type="text" required></div>
<div class="form-group"><label>New IP</label><input name="ip" class="form-control" type="text" autocomplete="off" required></div>
<input type="hidden" name="do" value="do">
<center><button type="submit" class="btn btn-default">保存</button></center>
</form>
</div>
{{end}}

{{/* adminUserListPage 对应 admin/user_list.php。 */}}
{{define "adminUserListPage"}}
<hr><center><h3>用户-列表</h3></center>
<div style="overflow:auto" data-csrf="{{csrfToken .Token}}">
<center>
<form action="/admin/user_list.php" method="get" class="form-search form-inline">
<input type="text" name="keyword" value="{{.Keyword}}" class="form-control search-query" placeholder="用户ID, 昵称, 学校">
<button type="submit" class="form-control">搜索</button>
<a href="/admin/user_list.php?trash" title="查看已删除用户">已删除</a>
</form>
</center>
<center>
<table width="100%" border="1" style="text-align:center;">
<thead>
<tr>
<th>用户ID</th><th>昵称</th><th>IP</th><th>EMAIL</th><th>学校</th><th>分组</th><th>家长电话</th>
<th>最后登录</th><th>注册时间</th><th>到期日</th><th>获得金币</th><th>奖励金币</th><th>消耗金币</th><th>金币余额</th>
<th>状态</th><th>管理</th><th>重置密码</th><th>添加权限</th>
</tr>
</thead>
<tbody>
{{range .Rows}}
<tr>
<td><a href="/userinfo.php?user={{.UserID}}">{{.UserID}}</a></td>
<td><span fd="nick" user_id="{{.UserID}}">{{if .Nick}}{{.Nick}}{{else}}{{raw "&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;"}}{{end}}</span></td>
<td><a href="{{.IPHref}}">{{.IP}}</a></td>
<td>{{.Email}}</td>
<td><span fd="school" user_id="{{.UserID}}">{{if .School}}{{.School}}{{else}}{{raw "&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;"}}{{end}}</span></td>
<td><span fd="group_name" user_id="{{.UserID}}">{{if .GroupName}}{{.GroupName}}{{else}}{{raw "&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;"}}{{end}}</span></td>
<td><span fd="parent_phone" user_id="{{.UserID}}">{{if .ParentPhone}}{{.ParentPhone}}{{else}}{{raw "&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;"}}{{end}}</span></td>
<td>{{.AcceTime}}</td>
<td>{{.RegTime}}</td>
<td><span fd="expiry_date" user_id="{{.UserID}}" class="{{.ExpiryClass}}">{{.ExpiryDate}}</span></td>
<td>{{.CoinEarned}}</td>
<td><span fd="coin_bonus" user_id="{{.UserID}}">{{.CoinBonus}}</span></td>
<td><span fd="coin_spent" user_id="{{.UserID}}">{{.CoinSpent}}</span></td>
<td>{{.CoinBalance}}</td>
<td>{{if .Normal}}<span class="green">Normal</span>{{else}}<span class="red">Deleted</span>{{end}}</td>
<td>{{if .CanManage}}<a href="/admin/user_df_change.php?cid={{.UserID}}">{{if .Normal}}<span class="label label-danger">Click to Delete</span>{{else}}<span class="label label-success">Click to Recover</span>{{end}}</a>{{else}}&nbsp;{{end}}</td>
<td><a class="label label-warning" href="/admin/changepass.php?uid={{.UserID}}">Reset</a></td>
<td><a class="label label-success" href="/admin/privilege_add.php?uid={{.UserID}}">Add</a></td>
</tr>
{{end}}
</tbody>
</table>
</center>
{{if .ShowPagination}}
<div style="display:inline;"><nav class="center"><ul class="pagination pagination-sm">
<li class="page-item"><a href="/admin/user_list.php?page=1{{.TrashSuffix}}">&lt;&lt;</a></li>
<li class="page-item"><a href="/admin/user_list.php?page={{.PrevPage}}{{.TrashSuffix}}">&lt;</a></li>
{{range .Pages}}<li class="{{if .Active}}active {{end}}page-item"><a title="go to page" href="/admin/user_list.php?page={{.Num}}{{$.TrashSuffix}}">{{.Num}}</a></li>{{end}}
<li class="page-item"><a href="/admin/user_list.php?page={{.NextPage}}{{.TrashSuffix}}">&gt;</a></li>
<li class="page-item"><a href="/admin/user_list.php?page={{.TotalPages}}{{.TrashSuffix}}">&gt;&gt;</a></li>
</ul></nav></div>
{{end}}
</div>
<script>
(function () {
  var root = document.querySelector('[data-csrf]');
  var token = root ? root.getAttribute('data-csrf') : '';
  var NBSP = '&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;';
  document.querySelectorAll('span[fd]').forEach(function (span) {
    span.addEventListener('dblclick', function () {
      var userID = span.getAttribute('user_id');
      var fd = span.getAttribute('fd');
      var old = span.textContent.trim();
      var input = document.createElement('input');
      input.type = fd === 'expiry_date' ? 'date' : (fd === 'coin_bonus' || fd === 'coin_spent' ? 'number' : 'text');
      input.value = old;
      span.textContent = '';
      span.appendChild(input);
      input.focus();
      input.select();
      input.addEventListener('change', function () {
        var val = input.value;
        var body = new URLSearchParams({ m: 'user_update_' + fd, user_id: userID, csrf_token: token });
        body.set(fd, val);
        fetch('/admin/ajax.php', { method: 'POST', body: body }).then(function (resp) {
          if (!resp.ok) { window.alert('更新失败'); return; }
          span.textContent = val || '';
        });
      });
    });
  });
})();
</script>
{{end}}

{{/* adminUserAddPage 对应 admin/user_add.php。 */}}
{{define "adminUserAddPage"}}
<hr><center><h3>用户-添加</h3></center>
<div class="container">
{{range .Messages}}<div>{{.}}</div>{{end}}
<form action="/admin/user_add.php" method="post" class="form-horizontal">
{{template "csrfInput" .}}
<div class="form-group">
<label class="col-sm">用户ID 密码</label>
</div>
<div>
( 每行一个 "userid password"，以换行分隔 )
<table width="100%">
<tr><td height="*">
<textarea name="ulist" rows="10" style="width:100%;" placeholder="userid1 password1
userid2 password2
userid3 password3">{{.UList}}</textarea>
</td></tr>
</table>
</div>
<div class="form-group">
<center><button name="do" value="do" class="btn btn-default">保存</button></center>
</div>
</form>
</div>
{{end}}

{{/* adminUserImportPage 对应 admin/user_import.php。 */}}
{{define "adminUserImportPage"}}
<br><br>
<h1>导入用户 csv 文件</h1>
{{range .Messages}}<div>{{.}}</div>{{end}}
<form class="form-inline" action="/admin/user_import.php" method="post" enctype="multipart/form-data">
{{template "csrfInput" .}}
<div class="form-group">
<input class="form-control" type="file" name="fps">
</div>
<br><br>
<center><div class="form-group">
<button class="btn btn-default btn-sm" type="submit">Upload to HUSTOJ</button>
</div></center>
</form>
<h2><a href="/admin/users.csv">下载模板</a></h2>
<h3>请用下载的模板填写，保存为UTF-8编码。</h3>
{{end}}

{{/* adminProblemListPage 对应 admin/problem_list.php。 */}}
{{define "adminProblemListPage"}}
<hr><center><h3>题目-列表</h3></center>
<div class="container" data-csrf="{{csrfToken .Token}}">
<center>
<form action="/admin/problem_list.php" method="get" class="form-search form-inline">
<input type="text" name="keyword" value="{{.Keyword}}" class="form-control search-query" placeholder="题号, 标题, 描述, 来源">
<button type="submit" class="form-control">搜索</button>
</form>
</center>
<center>
<table width="100%" border="1" style="text-align:center;">
<thead>
<tr>
<th>题号</th><th>标题</th><th>AC</th><th>入库时间</th><th>来源</th>
{{if .CanEdit}}<th>状态</th><th>金币</th><th>编辑</th><th>数据</th>{{end}}
</tr>
</thead>
<tbody>
{{range .Rows}}
<tr>
<td>{{.ProblemID}}</td>
<td><a href="/problem.php?id={{.ProblemID}}">{{.Title}}</a>{{if .HasRemote}}&nbsp;<a href="{{.RemoteHref}}" target="_blank">{{.RemoteLabel}}</a>{{end}}</td>
<td>{{.Accepted}}</td>
<td>{{.InDate}}</td>
<td>{{range .Tags}}<a title="{{.Title}}" class="label label-{{.Theme}}" style="display:inline-block;" href="{{.Href}}">{{.Label}}</a>&nbsp;{{end}}</td>
{{if $.CanEdit}}
<td><a href="/admin/problem_df_change.php?id={{.ProblemID}}">{{if eq .Defunct "N"}}<span class="green">可用</span>{{else}}<span class="red">保留</span>{{end}}</a></td>
<td><span fd="coin" problem_id="{{.ProblemID}}">{{.Coin}}</span></td>
<td><a href="/admin/problem_edit.php?id={{.ProblemID}}">编辑</a></td>
<td><a href="/admin/test_data.php?pid={{.ProblemID}}">数据</a></td>
{{end}}
</tr>
{{end}}
</tbody>
</table>
</center>
{{if .ShowPagination}}
<div style="display:inline;"><nav class="center"><ul class="pagination pagination-sm">
<li class="page-item"><a href="/admin/problem_list.php?page=1">&lt;&lt;</a></li>
<li class="page-item"><a href="/admin/problem_list.php?page={{.PrevPage}}">&lt;</a></li>
{{range .Pages}}<li class="{{if .Active}}active {{end}}page-item"><a title="go to page" href="/admin/problem_list.php?page={{.Num}}">{{.Num}}</a></li>{{end}}
<li class="page-item"><a href="/admin/problem_list.php?page={{.NextPage}}">&gt;</a></li>
<li class="page-item"><a href="/admin/problem_list.php?page={{.TotalPages}}">&gt;&gt;</a></li>
</ul></nav></div>
{{end}}
</div>
<script>
(function () {
  var root = document.querySelector('[data-csrf]');
  var token = root ? root.getAttribute('data-csrf') : '';
  document.querySelectorAll('span[fd=coin]').forEach(function (span) {
    span.addEventListener('dblclick', function () {
      var pid = span.getAttribute('problem_id');
      var old = span.textContent.trim();
      var input = document.createElement('input');
      input.type = 'number';
      input.value = old;
      span.textContent = '';
      span.appendChild(input);
      input.focus();
      input.select();
      input.addEventListener('change', function () {
        var val = input.value;
        var body = new URLSearchParams({ m: 'problem_update_coin', problem_id: pid, csrf_token: token });
        body.set('coin', val);
        fetch('/admin/ajax.php', { method: 'POST', body: body }).then(function (resp) {
          if (resp.ok) { span.textContent = val; } else { window.alert('更新失败'); }
        });
      });
    });
  });
})();
</script>
{{end}}

{{/* adminProblemChangeIDPage 对应 admin/problem_changeid.php。 */}}
{{define "adminProblemChangeIDPage"}}
<hr><center><h3>修改题号</h3></center>
{{if .Message}}<center><h4 class="text-danger">{{.Message}}</h4></center>{{end}}
{{if .ShowForm}}
<div class="padding">
<b>Change ProblemID</b>
<ol>
<li>Problem
<form action="/admin/problem_changeid.php" method="post">
{{template "csrfInput" .}}
Move <input type="text" name="from"> -&gt; <input type="text" name="to">
<input type="hidden" name="do" value="do">
<input type="submit" value="submit">
</form>
</li>
</ol>
</div>
{{else}}
<center><h4 class="text-danger">你需要在 php.ini 的 open_basedir 里加入 OJ_DATA，或执行 chmod 775 -R $OJ_DATA</h4></center>
{{end}}
{{end}}

{{/* markdownEditorScript 是后台 Markdown 编辑器，替代已停更多年的 KindEditor。
     逐字段增强 textarea.md-source：左侧写 Markdown 源码，右侧实时预览；
     预览走 /admin/markdown_preview.php，用与题面完全相同的 goldmark 渲染，
     因此所见即所得；上传走 /assets/editor/upload.php，在光标处插入链接。
     数学公式交给浏览器端 KaTeX auto-render（与题面同一套资源）。 */}}
{{define "markdownEditorScript"}}
<link rel="stylesheet" href="{{asset "/assets/katex.min.css"}}">
<style>
.md-editor{display:flex;flex-wrap:wrap;gap:.5rem 1rem;align-items:flex-start;margin-bottom:.5rem}
.md-editor textarea{flex:1 1 46%;min-width:280px;font-family:Consolas,Menlo,monospace;font-size:15px}
.md-editor .md-pane{flex:1 1 46%;min-width:280px;min-height:6rem;max-height:32rem;overflow:auto;
  padding:.5rem .75rem;border:1px solid #ddd;border-radius:4px;background:#fff}
.md-editor .md-bar{flex:1 1 100%;display:flex;gap:.5rem;align-items:center}
.md-editor .md-bar .muted{color:#888;font-size:.85em}
@media(max-width:768px){.md-editor .md-pane{display:none}}
</style>
<script src="{{asset "/assets/katex.min.js"}}" defer></script>
<script src="{{asset "/assets/auto-render.min.js"}}" defer></script>
<script>
(function(){
  var PREVIEW_URL="/admin/markdown_preview.php";
  var UPLOAD_URL="/assets/editor/upload.php";
  var MEDIA=/\.(mp3|wav|wma|wmv|mid|avi|mpg|asf|rm|rmvb|mp4|swf|flv|pdf|zip|rar|gz|bz2|doc|docx|xls|xlsx|ppt|txt)$/i;
  function debounce(fn,ms){var t;return function(){var a=arguments,c=this;clearTimeout(t);
    t=setTimeout(function(){fn.apply(c,a)},ms);};}
  function renderPane(ta,pane){
    var xhr=new XMLHttpRequest();
    xhr.open("POST",PREVIEW_URL,true);
    xhr.setRequestHeader("Content-Type","application/x-www-form-urlencoded");
    xhr.onreadystatechange=function(){
      if(xhr.readyState!==4||xhr.status!==200)return;
      pane.innerHTML=xhr.responseText;
      if(window.renderMathInElement){try{renderMathInElement(pane,{{katexOptions}});}catch(e){}}
    };
    xhr.send("text="+encodeURIComponent(ta.value));
  }
  function insertAtCursor(ta,text){
    var s=ta.selectionStart,e=ta.selectionEnd,v=ta.value;
    ta.value=v.slice(0,s)+text+v.slice(e);
    var p=s+text.length;
    ta.selectionStart=ta.selectionEnd=p;
    ta.focus();
  }
  function enhance(ta){
    var wrap=document.createElement("div");wrap.className="md-editor";
    ta.parentNode.insertBefore(wrap,ta);
    var bar=document.createElement("div");bar.className="md-bar";
    var up=document.createElement("button");
    up.type="button";up.className="btn btn-default btn-sm";up.textContent="上传图片/文件";
    var hint=document.createElement("span");hint.className="muted";
    hint.textContent="支持 Markdown 与 $LaTeX$ 公式";
    var file=document.createElement("input");file.type="file";file.style.display="none";
    file.accept="image/*,.pdf,.mp3,.mp4,.zip,.rar,.gz,.bz2,.doc,.docx,.xls,.xlsx,.ppt,.txt";
    var pane=document.createElement("div");pane.className="md-pane md-body";
    bar.appendChild(up);bar.appendChild(hint);bar.appendChild(file);
    wrap.appendChild(bar);wrap.appendChild(ta);wrap.appendChild(pane);
    var doRender=debounce(function(){renderPane(ta,pane);},250);
    ta.addEventListener("input",doRender);
    if(ta.value.trim()!=="")renderPane(ta,pane);
    up.addEventListener("click",function(){file.click();});
    file.addEventListener("change",function(){
      if(!file.files.length)return;
      var f=file.files[0],fd=new FormData();fd.append("file",f);
      var xhr=new XMLHttpRequest();
      xhr.open("POST",UPLOAD_URL,true);
      xhr.onreadystatechange=function(){
        if(xhr.readyState!==4)return;
        var resp={};
        try{resp=JSON.parse(xhr.responseText);}catch(e){}
        if(xhr.status===200&&resp.url){
          var alt=(f.name||"file").replace(/[\[\]]/g,"");
          insertAtCursor(ta,(MEDIA.test(resp.url)?"[":"![")+alt+"]("+resp.url+")");
          doRender();
        }else{
          alert(resp.message||"上传失败");
        }
        file.value="";
      };
      xhr.send(fd);
    });
  }
  function boot(){
    var list=document.querySelectorAll("textarea.md-source");
    for(var i=0;i<list.length;i++){enhance(list[i]);}
  }
  if(document.readyState==="loading"){document.addEventListener("DOMContentLoaded",boot);}else{boot();}
})();
</script>
{{end}}

{{/* adminProblemAddPage 对应 admin/problem_add_page.php。 */}}
{{define "adminProblemAddPage"}}
<center><h3>题目-添加</h3></center>
<hr>
<div id="main" class="padding">
<form method="post" action="/admin/problem_add.php">
{{template "csrfInput" .}}
<input type="hidden" name="problem_id" value="New Problem">
<h3>标题</h3>
<input class="input input-large" style="width:100%;" type="text" name="title" id="title">
<p align="left">
时限 <input class="input input-mini" type="number" min="0.001" max="300" step="0.001" name="time_limit" size="20" value="1"> sec
内存 <input class="input input-mini" type="number" min="1" max="2048" step="1" name="memory_limit" size="20" value="128"> MiB<br><br>
{{if .ShowCoin}}金币 <input class="input input-mini" type="number" min="0" max="9999" step="1" name="coin" size="20" value="1"><br><br>{{end}}
</p>
<h4>描述（Markdown，&lt;64kB）</h4>
<textarea class="md-source" name="description" rows="33" cols="80" style="width:100%;font-family:monospace"></textarea><br>
<h4>输入(&lt;64kB)</h4>
<textarea class="md-source" rows="13" name="input" cols="80"></textarea><br>
<h4>输出(&lt;64kB)</h4>
<textarea class="md-source" rows="13" name="output" cols="80"></textarea><br>
<h4>样例输入(&lt;64kB)</h4>
<textarea style="width:100%;" rows="13" name="sample_input"></textarea><br><br>
<h4>样例输出(&lt;64kB)</h4>
<textarea style="width:100%;" rows="13" name="sample_output"></textarea><br><br>
<h4>测试输入</h4>
<textarea style="width:100%;" rows="13" name="test_input"></textarea><br><br>
<h4>测试输出</h4>
<textarea style="width:100%;" rows="13" name="test_output"></textarea><br><br>
<h4>提示(&lt;64kB)</h4>
<textarea class="md-source" rows="13" name="hint" cols="80"></textarea><br>
<h4>评测方式</h4>
<input type="radio" name="spj" value="0" checked>普通<br>
<input type="radio" name="spj" value="1">特殊评测(SPJ)<br>
<input type="radio" name="spj" value="2">判断题(Raw Text)<br>
<input type="radio" name="spj" value="3">交互题<br>
<input type="radio" name="spj" value="4">通讯题（多轮交互）<br>
<h4>来源</h4>
<textarea name="source" style="width:100%;" rows="1">{{.Source}}</textarea><br><br>
<h4>比赛</h4>
<select name="contest_id">
<option value="">none</option>
{{range .Contests}}<option value="{{.ContestID}}">{{.ContestID}} {{.Title}}</option>{{end}}
</select>
<div align="center"><input type="submit" value="保存" name="submit"></div>
</form>
</div>
{{template "markdownEditorScript"}}
{{end}}

{{/* adminProblemAddResultPage 是新增题目的结果页。 */}}
{{define "adminProblemAddResultPage"}}
<center><h3>题目已添加</h3></center>
<p>New Problem: <a href="/problem.php?id={{.PID}}">{{.PID}} {{.Title}}</a></p>
{{end}}

{{/* adminProblemEditPage 对应 admin/problem_edit.php。 */}}
{{define "adminProblemEditPage"}}
<center><h3>编辑题目</h3></center>
<p><a href="/admin/test_data.php?pid={{.ProblemID}}">测试数据管理（题目 {{.ProblemID}}）→</a></p>
<hr>
<div id="main" class="container">
<form method="post" action="/admin/problem_edit.php">
{{template "csrfInput" .}}
<input type="hidden" name="problem_id" value="{{.ProblemID}}">
<p align="left"><center><h3>{{.ProblemID}}: <input class="input input-xxlarge" style="width:90%" type="text" name="title" value="{{.Title}}"></h3></center></p>
<p align="left">
时限 <input class="input input-mini" type="number" min="0.001" max="300" step="0.001" name="time_limit" size="20" value="{{.TimeLimit}}"> sec
内存 <input class="input input-mini" type="number" min="1" max="1024" step="1" name="memory_limit" size="20" value="{{.MemoryLimit}}"> MiB
{{if .ShowCoin}}金币 <input class="input input-mini" type="number" min="0" max="9999" step="1" name="coin" size="20" value="{{.Coin}}">{{end}}
</p>
<h4>描述（Markdown）</h4>
<textarea class="md-source" name="description" rows="33" cols="80" style="width:100%;font-family:monospace">{{.Description}}</textarea><br>
<h4>输入</h4>
<textarea class="md-source" rows="13" name="input" cols="80">{{.Input}}</textarea><br>
<h4>输出</h4>
<textarea class="md-source" rows="13" name="output" cols="80">{{.Output}}</textarea><br>
<h4>样例输入</h4>
<textarea style="width:100%;" rows="13" name="sample_input">{{.SampleInput}}</textarea><br><br>
<h4>样例输出</h4>
<textarea style="width:100%;" rows="13" name="sample_output">{{.SampleOut}}</textarea><br><br>
<h4>提示</h4>
<textarea class="md-source" rows="13" name="hint" cols="80">{{.Hint}}</textarea><br>
<h4>评测方式</h4>
<input type="radio" name="spj" value="0" {{if eq .Spj "0"}}checked{{end}}>普通<br>
<input type="radio" name="spj" value="1" {{if eq .Spj "1"}}checked{{end}}>特殊评测(SPJ)<br>
<input type="radio" name="spj" value="2" {{if eq .Spj "2"}}checked{{end}}>判断题(Raw Text)<br>
<input type="radio" name="spj" value="3" {{if eq .Spj "3"}}checked{{end}}>交互题<br>
<input type="radio" name="spj" value="4" {{if eq .Spj "4"}}checked{{end}}>通讯题（多轮交互）<br>
<h4>来源</h4>
<textarea name="source" style="width:100%;" rows="1">{{.Source}}</textarea><br>
<h4>远程 OJ</h4>
<input name="remote_oj" value="{{.RemoteOj}}">
<input name="remote_id" value="{{.RemoteID}}"><br>
<div align="center"><input class="btn btn-success" type="submit" value="保存" name="submit"></div>
</form>
</div>
{{template "markdownEditorScript"}}
{{end}}

{{/* adminTestDataPage 是后台测试数据管理页（对应 PHP phpfm.php 里与题目数据相关的部分）。 */}}
{{define "adminTestDataPage"}}
<center><h3>测试数据 - 题目 {{.PID}}</h3></center>
<div class="padding">
<p>目录：<code>{{.Path}}</code></p>
<div class="ui message">
<strong>文件约定：</strong>
<ul style="margin:.4em 0 0">
<li>用例 <code>0.in</code>／<code>0.out</code>、<code>1.in</code>／<code>1.out</code>…… 数字编号成对；判题按文件名顺序扫描所有 <code>.in</code>，各自找同名 <code>.out</code>（没有 <code>.ans</code> 回退：缺 <code>.out</code> 的用例会异常，孤立的 <code>.out</code> 不会被使用）</li>
<li>子任务与分值：文件名可用 <code>1_1.in</code>／<code>1_2.in</code> 形式（下划线前是子任务编号，同编号为一个子任务），<code>[10]</code> 标记该测试点分值（如 <code>sub[10].in</code>）；多用例时按 OI 模式计分，同一子任务的用例全部通过才拿到该子任务的分值</li>
<li>文本评测题：整题只有一对 <code>data.in</code>／<code>data.out</code></li>
<li>交互题：每个 <code>.in</code> 是一个交互会话；目录里没有 <code>.in</code> 时系统会注入合成用例</li>
<li>通讯题：<code>X.in</code> 与 <code>X.ans</code> 成对（<code>.ans</code> 可为空文件），目录里必须有预编译的 <code>validator</code>（或 <code>output_validator</code>）；每轮校验器以 <code>本轮输入 答案文件 工作目录</code> 为参数、选手本轮输出接 stdin，退出码 42（或 0）通过并写 <code>nextpass.in</code> 进入下一轮，最多 100 轮</li>
<li>特殊评测程序：<code>spj</code>（参数：<code>spj 输入文件 标准输出 选手输出</code>）、<code>tpj</code>（参数：<code>tpj 输入文件 选手输出 标准输出</code>，即 testlib 的 inf／ouf／ans 顺序）、<code>upj</code>（参数同 spj，退出码即 0–100 的分值，100 为 AC）；题目标记为特殊评测时目录内必须有其一，同时存在时按 upj → tpj → spj 的优先级取用。判题程序退出码 0 判 AC，非 0 判 WA</li>
<li>交互评测（题目标记为交互评测）：交互器是目录里预编译的 <code>spj</code> 二进制，判题时拷入沙箱并附加用例文件作参数（如 <code>/code/spj /code/data.in</code>）；玩家程序与交互器经两个 FIFO 双向通信（各自的 stdout 接对方的 stdin），双方同时受时限与内存限制，交互器退出码 0 判 AC、非 0 判 WA，任一侧超限或崩溃优先作为最终结果</li>
<li>通讯题（题目标记为通讯题，即 ICPC/Kattis 的 custom multi-pass）：每个 <code>X.in</code> 配一个 <code>X.ans</code>（答案常为空文件，缺省按空文件处理）；目录里必须有预编译的 <code>validator</code>（或 <code>output_validator</code>）。每轮先以本轮输入跑选手程序，再以 <code>validator 本轮输入 答案文件 工作目录</code> 运行校验器、选手本轮输出接 stdin；校验器退出码 42（或 0）表示本轮通过并写出 <code>nextpass.in</code> 进入下一轮，未写 <code>nextpass.in</code> 时本组数据通过，其余退出码判 WA（校验器崩溃/超时判 RE），最多 100 轮。校验器可用 <code>state.txt</code> 跨轮保存状态，<code>judgemessage.txt</code> 会显示在评测详情里</li>
<li>编译方法：<code>spj</code>／<code>tpj</code>／<code>upj</code>、交互器与通讯题校验器都由出题人预先编译成二进制放入数据目录（判题时不编译），如 <code>g++ -O2 -static spj.cc -o spj</code>；<code>tpj</code> 源码需 <code>#include "testlib.h"</code>，编译时用 <code>-I</code> 指向 testlib.h 所在目录（通常放数据根目录共享）。目录里的 <code>.cc</code>／<code>.cpp</code> 源码仅作存档</li>
<li>强制文件输入/输出：<code>input.name</code>／<code>output.name</code>，内容即程序 freopen 使用的文件名（如 <code>sort.in</code>）；<code>solution.name</code> 则强制用户按指定文件名上传答案</li>
<li>代码辅助：<code>template.&lt;语言后缀&gt;</code> 是答案模版，<code>prepend.&lt;后缀&gt;</code>／<code>append.&lt;后缀&gt;</code> 会拼到用户源码前后；<code>Main.c</code>／<code>Main.cc</code>（标程）与 <code>Gen.py</code>（数据生成器）仅作存档，判题不读取</li>
<li>文件名只能是纯文件名（不能含 <code>/</code>、<code>\</code> 等路径字符）；上传单个文件限 64MB，<code>.zip</code> 解压限 2000 个条目、总量 512MB</li>
</ul>
</div>
<p><a href="/admin/problem_list.php">返回题目列表</a> · <a href="/admin/problem_edit.php?id={{.PID}}">编辑此题</a></p>

{{if .Msg}}<div class="ui message">{{.Msg}}</div>{{end}}
{{if .Err}}<div class="ui negative message">{{.Err}}</div>{{end}}

<div class="ui message">
<strong>完整性检查：</strong>共 {{.Report.Total}} 个文件，<strong>{{.Report.Pairs}}</strong> 组用例配对齐全{{if .Report.Numbering}}，编号 {{.Report.Numbering}}{{end}}。
{{if .Report.CommMode}}<br>本题是通讯题：用例按 <code>X.in</code>／<code>X.ans</code> 配对，判题结果由 <code>validator</code> 决定，<code>.out</code> 不参与判题。{{end}}
{{if .Report.InWithoutOut}}<br><span class="red">缺 {{if .Report.CommMode}}.ans{{else}}.out{{end}}（判题会异常）：{{range .Report.InWithoutOut}}{{.}} {{end}}</span>{{end}}
{{if .Report.OutWithoutIn}}<br><span class="orange">孤立 {{if .Report.CommMode}}.ans{{else}}.out{{end}}（判题用不到）：{{range .Report.OutWithoutIn}}{{.}} {{end}}</span>{{end}}
{{if .Report.OddNames}}<br>非数字编号的 .in：{{range .Report.OddNames}}{{.}} {{end}}{{end}}
{{if .Report.SPJPrograms}}<br>SPJ 程序：{{range .Report.SPJPrograms}}{{.}} {{end}}{{end}}
{{if .Report.SPJWarn}}<br><span class="red">该题标记为特殊评测，但目录内没有 spj／tpj／upj 文件。</span>{{end}}
{{if .Report.CommValidator}}<br>通讯题校验器：{{.Report.CommValidator}}{{end}}
{{if .Report.CommWarn}}<br><span class="red">该题标记为通讯题，但目录内没有 validator／output_validator 文件。</span>{{end}}
</div>

<form method="post" action="/admin/test_data.php?pid={{.PID}}" enctype="multipart/form-data">
{{template "csrfInput" .}}
<input type="hidden" name="action" value="upload">
<p>上传：<input type="file" name="files" multiple>
<button type="submit" class="ui button">上传</button>
<span style="font-size:.9em">（可多选；<code>.zip</code> 自动解压，只允许写本题目录，覆盖同名文件）</span></p>
</form>

<form method="post" action="/admin/test_data.php?pid={{.PID}}">
{{template "csrfInput" .}}
<input type="hidden" name="action" value="create">
<p>新建空文件：<input name="name" placeholder="如 4.in"> <button type="submit" class="ui button">新建</button></p>
</form>

<form method="post" action="/admin/test_data.php?pid={{.PID}}">
{{template "csrfInput" .}}
<input type="hidden" name="action" value="delete">
<table class="ui celled table">
<thead><tr><th style="width:2em"></th><th>文件名</th><th>大小</th><th>修改时间</th><th>操作</th></tr></thead>
<tbody>
{{range .Files}}
<tr>
<td><input type="checkbox" name="names" value="{{.Name}}"></td>
<td>{{if .IsText}}<a href="{{.ViewURL}}">{{.Name}}</a>{{else}}{{.Name}}{{end}}</td>
<td>{{.SizeText}}</td>
<td>{{.ModTime}}</td>
<td><a href="{{.DownURL}}">下载</a>{{if .IsText}} · <a href="{{.EditURL}}">编辑</a>{{end}}</td>
</tr>
{{else}}
<tr><td colspan="5">（暂无数据文件）</td></tr>
{{end}}
</tbody>
</table>
<p><button type="submit" class="ui button">删除选中</button></p>
</form>

<form method="post" action="/admin/test_data.php?pid={{.PID}}">
{{template "csrfInput" .}}
<input type="hidden" name="action" value="rename">
<p>重命名：<input name="old" placeholder="原文件名"> → <input name="new" placeholder="新文件名">
<button type="submit" class="ui button">重命名</button></p>
</form>

<p><a class="ui button" href="/admin/test_data.php?pid={{.PID}}&amp;action=zip">打包下载全部（zip）</a></p>
</div>
{{end}}

{{/* adminTestDataViewPage 是数据文件的文本查看页（超出上限只显示前段）。 */}}
{{define "adminTestDataViewPage"}}
<div class="padding">
<p><a href="/admin/test_data.php?pid={{.PID}}">← 返回题目 {{.PID}} 的测试数据</a> · <a href="/admin/test_data.php?pid={{.PID}}&amp;action=edit&amp;name={{.Name|urlquery}}">编辑本文件</a></p>
<h3>{{.Name}}</h3>
{{if .Truncated}}<div class="ui warning message">文件较大，仅显示前 256KB。</div>{{end}}
<pre style="white-space:pre-wrap">{{.Content}}</pre>
</div>
{{end}}

{{/* adminTestDataEditPage 是数据文件的在线编辑页（仅文本类，上限 1MiB）。 */}}
{{define "adminTestDataEditPage"}}
<div class="padding">
<p><a href="/admin/test_data.php?pid={{.PID}}">← 返回题目 {{.PID}} 的测试数据</a> · <a href="/admin/test_data.php?pid={{.PID}}&amp;action=view&amp;name={{.Name|urlquery}}">只读查看</a></p>
<h3>编辑 {{.Name}}</h3>
<div class="ui message">
保存会覆盖原文件。行尾统一为 LF（<code>\n</code>）；请保留末尾换行等对判题有意义的字节。仅文本类文件可在线编辑，超过 1MiB 请下载后修改再上传。
</div>
<form method="post" action="/admin/test_data.php?pid={{.PID}}">
{{template "csrfInput" .}}
<input type="hidden" name="action" value="save">
<input type="hidden" name="name" value="{{.Name}}">
<p><textarea name="content" rows="24" style="width:100%;font-family:monospace;white-space:pre">{{.Content}}</textarea></p>
<p><button type="submit" class="ui button">保存</button> <a href="/admin/test_data.php?pid={{.PID}}">取消</a></p>
</form>
</div>
{{end}}

{{/* adminProblemExportPage 对应 admin/problem_export.php。 */}}
{{define "adminProblemExportPage"}}
<center><h3>题目-导出</h3></center>
<hr>
<div class="container">
<br><br>
- Export Problem XML<br><br>
<form class="form-inline" action="/admin/problem_export_xml.php" method="post">
{{template "csrfInput" .}}
<div class="form-group"><label>1) 连续题号：</label>
<input class="form-control" name="start" type="text" placeholder="1001"> ~
<input class="form-control" name="end" type="text" placeholder="1009"></div>
<br><br>
<div class="form-group"><label>2) 任选题号：</label>
<input class="form-control" name="in" type="text" placeholder="1001,1003,1005, ... "></div>
<br><br>
<div class="form-group"><label>3) 远程判题导出填远程别名，正常导出留空：</label>
<input class="form-control" name="remote_name" type="text" placeholder="my"></div>
<br><br>
<center><div class="form-group">
<input type="checkbox" name="zip">ZIP
<input type="hidden" name="do" value="do">
<button class="btn btn-default btn-sm" type="submit">Download to XML File</button>
</div></center>
</form>
<br><br>
- 连续题号与任选题号二选一：任选题号为空时用连续题号，反之亦然。<br>
</div>
{{end}}

{{/* adminProblemCopyPage 对应 admin/problem_copy.php。 */}}
{{define "adminProblemCopyPage"}}
<center><h3>复制题目</h3></center>
<ol>
{{range .Sources}}
<li>
<form method="post" action="/admin/{{.Action}}">
<input name="url" type="text" size="100" class="input input-xxlarge" value="{{.Placeholder}}">
<input type="submit">
</form>
</li>
{{end}}
</ol>
{{end}}

{{/* adminRejudgePage 对应 admin/rejudge.php。 */}}
{{define "adminRejudgePage"}}
<div class="container">
<b>Rejudge</b>
<ol>
<li>题目
<form action="/admin/rejudge.php" method="post">
{{template "csrfInput" .}}
<input type="text" name="rjpid" placeholder="1001">
<input type="hidden" name="do" value="do">
<input type="submit" value="submit">
</form>
<li>提交
<form action="/admin/rejudge.php" method="post">
{{template "csrfInput" .}}
<input type="text" name="rjsid" placeholder="1002">
<input type="hidden" name="do" value="do">
<input type="submit" value="submit">
</form>
<li>手动
<form action="/admin/rejudge.php" method="post">
{{template "csrfInput" .}}
<select name="result">
{{range .FromResults}}<option value="{{.}}"{{if eq . 3}} selected{{end}}>{{judgeResultLabel .}}</option>{{end}}
</select>
<input type="hidden" name="do" value="do"> -&gt;
<select name="to">
{{range .ToResults}}<option value="{{.}}"{{if eq . 6}} selected{{end}}>{{judgeResultLabel .}}</option>{{end}}
</select>
<input type="submit" value="submit">
</form>
<li>比赛
<form action="/admin/rejudge.php" method="post">
{{template "csrfInput" .}}
<input type="text" name="rjcid" placeholder="1003" value="1003">
<input type="hidden" name="do" value="do">
<input type="submit" value="submit">
</form>
<form action="/admin/rejudge.php" method="post">
{{template "csrfInput" .}}
<input type="text" name="rjcid" placeholder="1004">
<select name="pid">
{{range $i, $l := .Letters}}<option value="{{$i}}">{{$l}}</option>{{end}}
</select>
<input type="hidden" name="do" value="do">
<input type="submit" value="submit">
</form>
</ol>
</div>
{{end}}

{{/* adminSolutionStatsPage 对应 admin/solution_statistics.php。 */}}
{{define "adminSolutionStatsPage"}}
<hr><center><h3>Report</h3></center>
<div class="container">
{{if .Problems}}
<center>
<table width="100%" border="1" style="text-align:center;">
<tr><td>USER_ID</td><td>NICK</td>{{range .Problems}}<td class="pid" value="{{.}}">P{{.}}</td>{{end}}</tr>
{{range .Rows}}
<tr><td>{{.UserID}}</td><td>{{.Nick}}</td>
{{range .Cells}}<td>{{if eq .Kind "AC"}}<span class="label label-success label-sm">AC</span>{{else if eq .Kind "WA"}}<span class="label label-danger label-sm">WA</span>{{else}}&nbsp;{{end}}</td>{{end}}
</tr>
{{end}}
</table>
<a href="javascript:history.go(-1);">Back</a>
</center>
{{else}}
<center>
<form action="/admin/solution_statistics.php" method="post" class="form-search form-inline">
{{template "csrfInput" .}}
<input type="text" name="pids" class="form-control" placeholder="1000,1001" size="128">
<textarea name="ulist" rows="10" style="width:100%;" placeholder="user1
user2
user3"></textarea>
<button type="submit" class="form-control">搜索</button>
</form>
</center>
{{end}}
<form action="/admin/solution_statistics.php" method="get" class="form-search form-inline">
<select name="cid">
{{range .Contests}}<option value="{{.ContestID}}"{{if eq .ContestID $.CID}} selected{{end}}>{{.Title}}</option>{{end}}
</select>
<button type="submit" class="form-control">搜索</button>
</form>
</div>
{{end}}

{{/* adminContestListPage 对应 admin/contest_list.php。 */}}
{{define "adminContestListPage"}}
<hr><center><h3>比赛-列表</h3></center>
<div class="padding">
<center>
<form action="/admin/contest_list.php" method="get" class="form-search form-inline">
<input type="text" name="keyword" class="form-control search-query" value="{{.Keyword}}" placeholder="比赛名, 说明">
<button type="submit" class="form-control">搜索</button>
</form>
</center>
<center>
<table width="100%" border="1" style="text-align:center;">
<tr>
<td>比赛ID</td><td>比赛名</td><td>开始</td><td>结束</td><td>公开</td><td>状态</td>
<td>编辑</td><td>复制</td><td>导出</td><td>日志</td><td>查重</td>
</tr>
{{range .Rows}}
<tr>
<td>{{.ContestID}}</td>
<td><a href="/contest.php?cid={{.ContestID}}">{{.Title}}</a></td>
<td>{{.StartTime}}</td>
<td>{{.EndTime}}</td>
<td><a href="/admin/contest_pr_change.php?cid={{.ContestID}}">{{if .Private}}<span class="red">私有</span>{{else}}<span class="green">公开</span>{{end}}</a></td>
<td><a href="/admin/contest_df_change.php?cid={{.ContestID}}">{{if eq .Defunct "N"}}<span class="green">可用</span>{{else}}<span class="red">保留</span>{{end}}</a></td>
<td><a href="/admin/contest_edit.php?cid={{.ContestID}}">编辑</a></td>
<td><a href="/admin/contest_add.php?cid={{.ContestID}}">复制</a></td>
<td><a href="/admin/problem_export_xml.php?cid={{.ContestID}}">导出</a></td>
<td><a href="/export_contest_code.php?cid={{.ContestID}}">日志</a></td>
<td><a href="/admin/suspect_list.php?cid={{.ContestID}}">查重</a></td>
</tr>
{{end}}
</table>
</center>
{{if .ShowPagination}}
<div style="display:inline;"><nav class="center"><ul class="pagination pagination-sm">
<li class="page-item"><a href="/admin/contest_list.php?page=1">&lt;&lt;</a></li>
<li class="page-item"><a href="/admin/contest_list.php?page={{.PrevPage}}">&lt;</a></li>
{{range .Pages}}<li class="{{if .Active}}active {{end}}page-item"><a title="go to page" href="/admin/contest_list.php?page={{.Num}}">{{.Num}}</a></li>{{end}}
<li class="page-item"><a href="/admin/contest_list.php?page={{.NextPage}}">&gt;</a></li>
<li class="page-item"><a href="/admin/contest_list.php?page={{.TotalPages}}">&gt;&gt;</a></li>
</ul></nav></div>
{{end}}
</div>
{{end}}

{{/* adminContestFormPage 对应 admin/contest_add.php 与 contest_edit.php。 */}}
{{define "adminContestFormPage"}}
<hr><center><h3>{{if .Editing}}编辑{{else}}添加{{end}}比赛</h3></center>
<div class="padding">
<form method="post">
{{template "csrfInput" .}}
{{if .Editing}}<input type="hidden" name="cid" value="{{.CID}}">{{end}}
<p align="left"><h3>比赛-标题</h3>
<input class="input input-xxlarge" style="width:100%;" type="text" name="title" value="{{.Title}}"></p>
<p align="left">
比赛-开始：<input type="date" name="startdate" value="{{.StartDate}}">
时 <input type="text" name="shour" size="2" value="{{.SHour}}">
分 <input type="text" name="sminute" size="2" value="{{.SMinute}}">
</p>
<p align="left">
比赛-结束：<input type="date" name="enddate" value="{{.EndDate}}">
时 <input type="text" name="ehour" size="2" value="{{.EHour}}">
分 <input type="text" name="eminute" size="2" value="{{.EMinute}}">
</p>
<p align="left"><h4>比赛-题号（英文逗号分隔）</h4>
<input class="input input-xxlarge" type="text" style="width:100%" name="cproblem" value="{{.PList}}"></p>
<p align="left">子网 <input class="input input-xxlarge" type="text" style="width:100%" name="subnet" value="{{.Subnet}}" placeholder="0.0.0.0/0"></p>
<p align="left"><h4>比赛-说明</h4>
<textarea class="md-source" rows="13" name="description" cols="80">{{.Description}}</textarea></p>
<table width="100%"><tr>
<td rowspan="2"><p align="left">比赛-语言（Ctrl 多选）<br>
<select name="lang[]" multiple="multiple" style="height:220px">
{{range .Langs}}<option value="{{.ID}}"{{if .Selected}} selected{{end}}>{{.Name}}</option>{{end}}
</select></p></td>
<td rowspan="2"><p align="left">禁止选项<br>
{{range .Locks}}<input type="checkbox" name="contest_type[]" value="{{.Index}}"{{if .Checked}} checked{{end}}>{{.Name}}<br>{{end}}
</p></td>
<td height="10px"><p align="left">公开：
<select name="private" style="width:150px;">
<option value="0"{{if not .Private}} selected{{end}}>公开</option>
<option value="1"{{if .Private}} selected{{end}}>私有</option>
</select>
密码：<input type="text" name="password" style="width:150px;" value="{{.Password}}"></p></td>
</tr><tr>
<td height="*"><p align="left">比赛-私有用户（每行一个）<br>
<textarea name="ulist" rows="10" style="width:100%;">{{.UList}}</textarea></p></td>
</tr></table>
<div align="center"><input type="submit" value="保存" name="submit"></div>
</form>
</div>
{{template "markdownEditorScript"}}
{{end}}

{{/* adminHelpPage 对应 admin/help.php。 */}}
{{define "adminHelpPage"}}
<table class="table">
<tbody>
<tr><td colspan="2">
延迟：{{.Delay}}s/judge　CPU：{{.Load}}tasks/1min　空闲内存：{{.FreeMem}}　空闲磁盘：{{.FreeDisk}}
　题目：{{.ProblemNum}}　用户：{{.UserNum}}　提交：{{.SubmitNum}}
</td></tr>
<tr><td><a class="btn btn-block btn-sm" href="/status.php">查看 OJ</a></td></tr>
<tr><td><a class="btn btn-success btn-sm" href="/admin/problem_list.php">题目-列表</a></td>
<td><a class="btn btn-success btn-sm" href="/admin/problem_add_page.php">题目-添加</a></td></tr>
<tr><td><a class="btn btn-success btn-sm" href="/admin/problem_export.php">题目-导出</a></td>
<td><a class="btn btn-success btn-sm" href="/admin/problem_copy.php">题目-复制</a></td></tr>
<tr><td><a class="btn btn-warning btn-sm" href="/admin/contest_list.php">比赛-列表</a></td>
<td><a class="btn btn-warning btn-sm" href="/admin/contest_add.php">比赛-添加</a></td></tr>
<tr><td><a class="btn btn-warning btn-sm" href="/admin/user_set_ip.php">比赛-设置登录IP</a></td></tr>
{{if .IsAdmin}}
<tr><td><a class="btn btn-info btn-sm" href="/admin/news_list.php">新闻-列表</a></td>
<td><a class="btn btn-info btn-sm" href="/admin/news_add_page.php">新闻-添加</a></td></tr>
<tr><td><a class="btn btn-primary btn-sm" href="/admin/user_list.php">用户-列表</a></td>
<td><a class="btn btn-primary btn-sm" href="/admin/user_add.php">用户-添加</a></td></tr>
<tr><td><a class="btn btn-primary btn-sm" href="/admin/user_import.php">用户-导入</a></td>
<td><a class="btn btn-primary btn-sm" href="/admin/privilege_list.php">权限-列表</a></td></tr>
<tr><td><a class="btn btn-primary btn-sm" href="/admin/privilege_add.php">权限-添加</a></td>
<td><a class="btn btn-primary btn-sm" href="/admin/changepass.php">用户-设置密码</a></td></tr>
<tr><td><a class="btn btn-danger btn-sm" href="/admin/rejudge.php">系统-重新判题</a></td>
<td><a class="btn btn-danger btn-sm" href="/admin/solution_statistics.php">系统-解题统计</a></td></tr>
<tr><td><a class="btn btn-danger btn-sm" href="/online.php">系统-在线用户</a></td></tr>
{{end}}
</tbody>
</table>
{{if .IsAdmin}}
<a href="/admin/problem_copy.php">复制题目</a> ·
<a href="/admin/problem_changeid.php">重排题号</a>
{{end}}
{{end}}

{{/* adminSuspectPage 对应 admin/suspect_list.php。 */}}
{{define "adminSuspectPage"}}
<hr><center><h3>IP 验证</h3></center>
<div class="container">
<center>同一 IP 被多个用户使用
<table width="90%" border="1" style="text-align:center;">
<tr><td>IP address</td><td colspan="2">Used ID</td><td>Time</td><td>IP address count</td></tr>
{{range .ByIP}}
<tr><td>{{.IP}}</td><td>{{.UserID}}</td>
<td><a href="/userinfo.php?user={{.UserID}}">档案</a> / <a href="/status.php?cid={{$.CID}}&user_id={{.UserID}}">提交</a></td>
<td>{{.Time}}</td><td>{{.Count}}</td></tr>
{{end}}
</table>
</center>
<br><br>
<center>同一用户使用多个 IP
<table width="90%" border="1" style="text-align:center;">
<tr><td colspan="2">User ID</td><td>Used IP address</td><td>Time</td><td>IP address count</td></tr>
{{range .ByUser}}
<tr><td>{{.UserID}}</td>
<td><a href="/userinfo.php?user={{.UserID}}">档案</a> / <a href="/status.php?cid={{$.CID}}&user_id={{.UserID}}">提交</a></td>
<td>{{.IP}}</td><td>{{.Time}}</td><td>{{.Count}}</td></tr>
{{end}}
</table>
</center>
</div>
{{end}}

{{/* adminWatchPage 对应 admin/watch.php。 */}}
{{define "adminWatchPage"}}
<h1>
<span style="color:rgb(175,216,248)">CPU :<span id="cpu">{{.CPU}}</span>%</span><br>
<span style="color:rgb(237,194,64)">FREE:<span id="mem">{{.FreeMem}}</span>MB</span><br>
<span style="color:rgb(77,167,77)">SWAP:<span id="swap">{{.Swap}}</span>MB</span><br>
<span style="color:rgb(203,75,75)">TCP :<span id="tcp">{{.TCP}}</span></span><br>
<span style="color:rgb(150,150,150)">DISK:<span id="disk">{{.Disk}}</span>MB (Used)</span><br>
<span style="color:rgb(150,150,150)">TOTAL:<span id="nas">{{.Total}}</span>GB</span><br>
</h1>
<script src="/assets/jquery.min.js"></script>
<script src="/assets/jquery.flot.js"></script>
<div id="panel" style="width:98%;height:180px" onclick="update()">loading data ...</div>
<script type="text/javascript">
function update() {
  $.getJSON("/admin/watch.php?json", function(result) {
    var cpu = result[0] || [], mem = result[1] || [], swap = result[2] || [], tcp = result[3] || [];
    if (!cpu.length) { $("#cpu,#mem,#swap,#tcp").text("N/A"); return; }
    $.plot($("#panel"), [
      { label: "MEM%", data: mem, lines: { show: true } },
      { label: "CPU%", data: cpu, bars: { show: true } },
      { label: "TCP", data: tcp, lines: { show: true } },
      { label: "SWAP%", data: swap, lines: { show: true } }
    ], { xaxis: { mode: "time" }, legend: { position: "nw" } });
    $("#cpu").text(cpu[cpu.length - 1][1].toFixed(2) + "%");
    $("#mem").text(mem[mem.length - 1][1].toFixed(0));
    $("#swap").text(swap[swap.length - 1][1].toFixed(0));
    $("#tcp").text(tcp[tcp.length - 1][1]);
  });
}
$(document).ready(function() { update(); window.setInterval(update, 2000); });
</script>
{{end}}

{{/* onlinePage 对应 online.php 的登录日志表（admin 取 50 条，其它取 20 条）。 */}}
{{define "onlinePage"}}
<div class="padding">
<table class="ui very basic center aligned table">
<thead>
<tr><th>用户</th><th>Password</th><th>IP</th><th>Time</th></tr>
</thead>
<tbody>
{{range .Users}}
<tr>
<td><a href="/userinfo.php?user={{.UserID}}">{{.UserID}}</a></td>
<td>{{.Password}}</td>
<td>{{.IP}}</td>
<td>{{.Time}}</td>
</tr>
{{end}}
</tbody>
</table>
</div>
{{end}}

{{/* recentContestPage 对应 recent-contest.php（原页靠外部 JSON 渲染，这里改为服务端列出库内的最近比赛）。 */}}
{{define "recentContestPage"}}
<div class="padding">
<table class="ui very basic center aligned table">
<thead>
<tr><th>比赛ID</th><th>比赛名</th><th>开始</th><th>结束</th><th>公开</th></tr>
</thead>
<tbody>
{{range .Contests}}
<tr>
<td>{{.ContestID}}</td>
<td><a href="/contest.php?cid={{.ContestID}}">{{.Title}}</a></td>
<td>{{.StartTime}}</td>
<td>{{.EndTime}}</td>
<td>{{if .Private}}<span class="red">私有</span>{{else}}<span class="green">公开</span>{{end}}</td>
</tr>
{{end}}
</tbody>
</table>
</div>
{{end}}

{{/* categoryPage 对应 category.php：来源标签云，点击跳 problemset.php?search=<标签>。 */}}
{{define "categoryPage"}}
<div class="padding">
<div class="ui existing segment" style="word-wrap:break-word;">
{{range .Categories}}<a class="ui label {{.Theme}}" style="display:inline-block;margin:5px" href="{{.Href}}">{{.Label}}</a>{{else}}<h3>No Category Now!</h3>{{end}}
</div>
</div>
{{end}}

{{/* compareSourcePage 对应 comparesource.php（原页用 mergely 前端对比，这里改为双栏 <pre>）。 */}}
{{define "compareSourcePage"}}
<div class="padding">
{{template "highlightAssets"}}
<table style="width:100%"><tr>
<td style="width:50%">{{.LeftInfo.UserID}} / P{{.LeftInfo.ProblemID}}</td>
<td style="width:50%">{{.RightInfo.UserID}} / P{{.RightInfo.ProblemID}}</td>
</tr></table>
<table style="width:100%"><tr>
<td style="width:50%;vertical-align:top"><pre><code class="language-{{.LeftLang}}">{{.LeftSource}}</code></pre></td>
<td style="width:50%;vertical-align:top"><pre><code class="language-{{.RightLang}}">{{.RightSource}}</code></pre></td>
</tr></table>
</div>
{{end}}

{{/* contestRankPage / contestRank3Page：contestrank2.php、contestrank3.php 的服务端榜单。 */}}
{{define "contestRankPage"}}
<div class="padding">
<center><h3>{{.ContestName}}</h3>
<p>{{.StartTime}} ~ {{.EndTime}}（封榜 {{.LockTime}}）</p></center>
<table class="ui very basic center aligned table">
<thead>
<tr><th>#</th><th>用户</th><th>解决</th><th>罚时</th><th>题目</th></tr>
</thead>
<tbody>
{{range .Rankings}}
<tr>
<td>{{.Rank}}</td>
<td><a href="/userinfo.php?user={{.UserID}}">{{.UserID}}</a></td>
<td>{{.Solved}}</td>
<td>{{.Time}}</td>
<td>
{{range .ProblemResults}}{{if gt .ACTime 0}}<span class="label label-success" title="{{.ACTime}}">{{.Num}}</span>{{else if gt .Attempts 0}}<span class="label label-danger">{{.Num}}</span>{{else}}<span class="label">{{.Num}}</span>{{end}}&nbsp;{{end}}
</td>
</tr>
{{end}}
</tbody>
</table>
</div>
{{end}}

{{define "contestRank3Page"}}{{template "contestRankPage" .}}{{end}}

 {{/* problemBatchDeleteResultPage 对应 admin/problem_batch_delete.php 的结果页。 */}}
 {{define "problemBatchDeleteResultPage"}}
 <center><h3>批量删除题目</h3></center>
 <p>已删除 {{.Count}} 道题目{{if .Success}}（成功）{{else}}（失败）{{end}}。</p>
 <p><a href="/admin/problem_list.php">返回题目列表</a></p>
 {{end}}

 {{/* faqPage 是帮助/常见问答页，对应 template/syzoj/faqs.php 与 faqs.news.php。
      标题"帮助"由 layout 统一渲染，这里不再重复 h1。
      - 命中 faqs.<lang> 公告时只渲染公告正文（faqs.news.php 的形态）；
      - 否则渲染静态帮助，其中"评测"一节替换为 .LangInfo —— 由
        hustoj-go langinfo 预生成的 <ojhome>/etc/faq-langs.html，
        列出各语言的版本号与编译/运行命令。 */}}
 {{define "faqPage"}}
 <div class="padding">
 <div class="font-content">
 {{if .NewsContent}}
 {{.NewsContent}}
 {{else}}
 {{.LangInfo}}
 <p>
   <br> 您可以使用 <code>#pragma GCC optimize ("O0")</code> 手工关闭 O2 优化；
   <br> Java 如果您的代码中没有 <code>public class</code>，请将入口类命名为 <code>Main</code>，在评测时提供额外 2 秒的运行时间和 64MB 的运行内存；
   <br> 这里给出的编译器版本仅供参考，请以实际编译器版本为准。
 </p>
 <h2 class="ui header">Q: cin/cout为什么会超时（TLE）?</h2>
 <p>A: cin/cout因为默认同步stdin/stdout而变慢，并产生更多的系统调用而受到性能影响，可以在main函数开头加入下面代码加速：
 <div class="ui existing segment">
 <pre style="margin-top: 0; margin-bottom: 0; ">ios::sync_with_stdio(false);
 cin.tie(0);</pre>
 </div>
 * 另外，请使用'\n'而不是 endl ，因为endl默认会增加刷新操作，而导致输出缓冲失效，降低效率。
 </p>

 <h2 class="ui header">Q: gets函数没有了吗?</h2>
 <p>A: gets函数因为不能限制输入的长度，造成了历史上大量的缓冲区溢出漏洞，因此在最新版本中被彻底删除了，请使用fgets这个函数取代。 或者使用下面的宏定义来取代：
 <div class="ques-view">   #define gets(S) fgets(S,sizeof(S),stdin)  </div>
 </p>

 <h2 class="ui header">Q: 为什么我的代码在本地正常，提交以后被判错? </h2>
 <p>A: 不要使用rewind来清空输入缓冲，OJ的输入本质是文件，与键盘输入逻辑不一样。 如果你发现别人可以正确完成问题，那么有可能是你的代码没有考虑到所有的情况。
 并不是通过样例数据就代表一定是正确答案。如果你发现所有人都不能正确提交该题，那么有可能是测试数据有问题，请向管理员{{.Admin}}反馈。
 </p>

 <h2 class="ui header">个人资料<br></h2>
 <p>本站不提供头像存储服务，而是使用 QQ 头像显示。请使用QQ邮箱注册 ，系统自动取用您在QQ的头像。</p>

 <h2 class="ui header">返回结果说明<br></h2>
 <div class="ques-view">
 <p>试题的解答提交后由评分系统评出即时得分，每一次提交会判决结果会及时通知；系统可能的反馈信息包括：</p>
 <li>等待评测：评测系统还没有评测到这个提交，请稍候</li>
 <li>正在评测：评测系统正在评测，稍候会有结果</li>
 <li>编译错误：您提交的代码无法完成编译，点击“编译错误”可以看到编译器输出的错误信息</li>
 <li>答案正确：恭喜！您通过了这道题</li>
 <li>格式错误：您的程序输出的格式不符合要求（比如空格和换行与要求不一致）</li>
 <li>答案错误：您的程序未能对评测系统的数据返回正确的结果</li>
 <li>运行超时：您的程序未能在规定时间内运行结束</li>
 <li>内存超限：您的程序使用了超过限制的内存</li>
 <li>运行错误：您的程序在运行过程中崩溃了，发生了如段错误、浮点错误等</li>
 <li>输出超限：您的程序输出了过多内容，一般可能是无限循环输出导致的结果</li>
 </div>

 <h2>程序样例</h2>
 <p>以下样例程序可用于解决这道简单的题目：<strong>读入2个整数A和B，然后输出它们的和。</strong></p>
 <p><strong>gcc (.c)</strong></p>
 <div class="ui existing segment">
 <pre style="margin-top: 0; margin-bottom: 0; ">
 <code class="lang-c">#include &lt;stdio.h&gt;
 int main(){
     int a, b;
     while(scanf("%d %d",&amp;a, &amp;b) != EOF){
         printf("%d\n", a + b);
     }
     return 0;
 }</code></pre>
 </div>
 <p><strong>g++ (.cpp)</strong></p>
 <div class="ui existing segment">
 <pre style="margin-top: 0; margin-bottom: 0; ">
 <code class="lang-c++">#include &lt;iostream&gt;
 using namespace std;
 int main(){
     // io speed up
     const char endl = '\n';
     std::ios::sync_with_stdio(false);
     cin.tie(nullptr);

     int a, b;
     while (cin &gt;&gt; a &gt;&gt; b){
         cout &lt;&lt; a+b &lt;&lt; endl;
     }
     return 0;
 }</code></pre>
 </div>
 <p><strong>fpc (.pas)</strong></p>
 <div class="ui existing segment">
 <pre style="margin-top: 0; margin-bottom: 0; ">
 <code class="lang-pascal">var
 a, b: integer;
 begin
     while not eof(input) do begin
         readln(a, b);
         writeln(a + b);
     end;
 end.</code></pre>
 </div>
 <p><strong>javac (.java)</strong></p>
 <div class="ui existing segment">
 <pre style="margin-top: 0; margin-bottom: 0; ">
 <code class="lang-java">import java.util.Scanner;
 public class Main {
     public static void main(String[] args) {
         Scanner in = new Scanner(System.in);
         while (in.hasNextInt()) {
             int a = in.nextInt();
             int b = in.nextInt();
             System.out.println(a + b);
         }
     }
 }</code></pre>
 </div>
 <p><strong>python3 (.py)</strong></p>
 <div class="ui existing segment">
 <pre style="margin-top: 0; margin-bottom: 0; ">
 <code class="lang-c">import io
 import sys
 sys.stdout = io.TextIOWrapper(sys.stdout.buffer,encoding='utf8')
 for line in sys.stdin:
     a = line.split()
     print(int(a[0]) + int(a[1]))</code></pre>
 </div>
 <p><strong>仓颉 (.cj)</strong></p>
 <div class="ui existing segment">
 <pre style="margin-top: 0; margin-bottom: 0; ">
 <code class="lang-python">import std.core.*
 import std.console.*
 import std.collection.*
 import std.convert.*

 main(): Int64 {
     while (let Some(line) &lt;- Console.stdIn.readln()) {
         let nums = map(Int.parse)(line.split(" "))
         let sum = reduce({a: Int64, b: Int64 =&gt; a + b})(nums)
         if (let Some(r) &lt;- sum) {
             Console.stdOut.writeln(r)
         }
     }
     return 0
 }</code></pre>
 </div>
 </div>
 {{end}}
 </div>
 </div>
 {{end}}

 `

// navVM 是站点导航头的数据（登录态、比赛上下文）。
type navVM struct {
	OJName       string
	LoggedIn     bool
	UserID       string
	Nick         string
	IsAdmin      bool
	ShowRegister bool
	CID          int32
	// Path 是当前请求路径，供导航高亮当前栏目。
	Path string

	// Admin 非空时，布局在公共导航下方再渲染一条后台导航条。
	// 只在 /admin/ 下的页面、且当前身份有后台权限时设置。
	Admin *AdminMenuVM
}

// buildNav 依据当前请求解析登录态，用于渲染导航头。
func (s *Server) buildNav(w http.ResponseWriter, r *http.Request) navVM {
	id := s.resolveIdentity(w, r)
	nav := navVM{
		OJName:       s.cfg.OJName,
		LoggedIn:     id.LoggedIn,
		UserID:       id.UserID,
		Nick:         id.Nick,
		IsAdmin:      id.Admin || id.ContestCreator || id.ProblemEditor,
		ShowRegister: s.cfg.Register,
		CID:          int32(parseInt(r.URL.Query().Get("cid"))),
		Path:         r.URL.Path,
	}
	// 后台页面（/admin/...）补一条后台导航，免去逐个页面自己维护菜单。
	if strings.HasPrefix(r.URL.Path, "/admin/") && s.hasAdminPrivilege(id) {
		nav.Admin = &AdminMenuVM{
			Title:            "后台管理",
			OJName:           s.cfg.OJName,
			IsAdmin:          id.Admin,
			IsContestCreator: id.ContestCreator,
			IsProblemEditor:  id.ProblemEditor,
		}
	}
	return nav
}

// navActive 判断当前路径是否属于某个导航栏目。
//
// 只做路径归组，不关心查询串：contest.php?my 与 contest.php 都算"竞赛"，
// 题面/提交/统计等题目子页统一算"题目"。
func navActive(path, href string) bool {
	if path == "" {
		return false
	}
	switch href {
	case "/problemset.php":
		switch path {
		case "/problemset.php", "/problem.php", "/problemstatus.php", "/submitpage.php":
			return true
		}
	case "/contest.php":
		return path == "/contest.php" || path == "/contestset.php"
	case "/status.php":
		return path == "/status.php"
	case "/ranklist.php":
		return path == "/ranklist.php"
	case "/category.php":
		return path == "/category.php"
	case "/faqs.php":
		return path == "/faqs.php"
	}
	return path == href
}

// renderPage 渲染具名页面模板并套上外壳（含站点导航头），写出响应。
// r 用于渲染登录态导航；允许为 nil（此时按匿名渲染）。
func (s *Server) renderPage(w http.ResponseWriter, r *http.Request, code int, name, title string, data any) {
	var body bytes.Buffer
	if err := pageTemplates.ExecuteTemplate(&body, name, data); err != nil {
		// 模板执行失败只回一句"页面渲染失败"是不够的：没有日志就只能对着
		// 空白页猜。这里把模板名和原始错误记下来，排查时一眼能看到根因
		// （例如 "wrong type for value; expected int; got int32"）。
		s.log.Error("渲染页面失败", "template", name, "err", err)
		http.Error(w, "页面渲染失败", http.StatusInternalServerError)
		return
	}
	nav := navVM{OJName: s.cfg.OJName, ShowRegister: s.cfg.Register}
	if r != nil {
		nav = s.buildNav(w, r)
	}
	var out bytes.Buffer
	if err := pageTemplates.ExecuteTemplate(&out, "layout", pageData{
		Title:   title,
		Content: template.HTML(body.String()),
		Nav:     nav,
	}); err != nil {
		s.log.Error("渲染页面外壳失败", "template", "layout", "err", err)
		http.Error(w, "页面渲染失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(out.Bytes())
}

// renderRaw 渲染不套外壳的具名模板（如 alert 片段），直接写出响应。
func renderRaw(w http.ResponseWriter, code int, name string, data any) {
	var out bytes.Buffer
	if err := pageTemplates.ExecuteTemplate(&out, name, data); err != nil {
		http.Error(w, "页面渲染失败", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(out.Bytes())
}

// pageData 是传给 layout 的数据。
type pageData struct {
	Title   string
	Content template.HTML
	Nav     navVM
}

// contestProblemsView 是比赛题目页模板的数据。
type contestProblemsView struct {
	Contest  dbgen.GetContestByIDRow
	CID      int32
	Problems []ContestProblemItem
	Flags    contestProblemFlags
}
