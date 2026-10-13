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

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// templateFuncs 汇总模板可用的辅助函数。
//
// 所有函数必须是纯函数、无副作用，且不直接输出 HTML（由 template 自动转义）。
var templateFuncs = template.FuncMap{
	// raw 声明一段内容为可信 HTML，按原文输出。见包注释的适用范围说明。
	"raw": func(s string) template.HTML { return template.HTML(s) },
	// nullInt 把可空计数渲染成字符串；NULL 显示为空，与 PHP 把 NULL
	// 拼成空串的行为一致。
	"nullInt": nullIntStr,
	// trimFloat 去掉时限末尾的 .0，与 PHP 的 float 打印习惯对齐。
	"trimFloat": trimFloat,
	// maskStat 在 NOIP 锁榜时把统计数字替换成问号。
	"maskStat": maskStat,
	// contestErrorTitle 让错误原因与页面标题共用同一份文案。
	"contestErrorTitle":     contestErrorTitle,
	"contestTypeName":       contestTypeName,
	"csrfToken":             csrfToken,
	"judgeResultLabel":      judgeResultLabel,
	"judgeResultClass":      judgeResultClass,
	"languageName":          languageName,
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
	"add":                          func(a, b int) int { return a + b },
	"sub":                          func(a, b int) int { return a - b },
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
	"languageNames": func() map[int32]string { return languageNames },
	// coinStars 复刻 contest.php:95 的 min(coin, 5)：题面最多 5 颗星。
	"coinStars":    coinStars,
	"resultLabels": func() map[int32]string { return resultLabels },
	// judgeLabelsJSON / judgeClassesJSON 供状态页把结果标签与配色嵌进
	// 自动刷新的 JS（对应 PHP 模板里的 judge_result / judge_color）。
	"judgeLabelsJSON": func() template.JS {
		b, _ := json.Marshal(resultLabels)
		return template.JS(b)
	},
	"judgeClassesJSON": func() template.JS {
		b, _ := json.Marshal(judgeResultClasses)
		return template.JS(b)
	},
}

// maxStatusLanguage 是 status.php 允许的语言编号上界，对应
// PHP 的 count($language_ext)（include/const.inc.php:13，26 项）。
// 超出该范围的 language 参数按 -1 处理，即"不按语言过滤"。
const maxStatusLanguage = 26

// languageNames 是 statusPage 的语言下拉框。
//
// 只列出本 OJ 真正配置了编译器的语言（/home/judge/etc/langs/*.lang.toml），
// 让用户选到的每个选项都能提交成功。PHP 那边的下拉框同样只列已安装语言。
var languageNames = map[int32]string{
	0: "GCC", 1: "G++", 2: "Java", 3: "Pascal", 5: "Python", 6: "PHP",
	7: "Perl", 8: "Ruby", 9: "Bash", 16: "Kotlin", 17: "Dart", 25: "Rust",
}

// resultLabels 是 statusPage 的结果下拉框。
//
// 数值语义必须与 judgeResultLabel 一致（result=4 是"通过"），否则下拉框
// 选"通过"会筛出错误答案。这里直接复用同一份标签，不再维护第二张表。
var resultLabels = map[int32]string{
	0: "等待", 1: "编译中", 2: "运行中", 3: "判题中", 4: "通过", 5: "格式错误",
	6: "错误答案", 7: "超时", 8: "内存超限", 9: "输出超限", 10: "运行错误",
	11: "编译错误", 12: "受限函数", 13: "非零退出", 14: "信号终止", 15: "内部错误",
}

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
<title>{{.Title}}</title>
</head>
<body>
<div class="ui container">
<h1>{{.Title}}</h1>
{{.Content}}
</div>
</body>
</html>
{{end}}`

const pageContentTmpls = `
{{define "problemset"}}{{template "problemsetTable" .}}{{template "pagination" .}}{{end}}

{{define "problemsetTable"}}<table class="ui celled table">
<thead><tr>
<th class="one wide"></th>
<th class="one wide">编号</th>
<th class="left aligned">标题</th>
<th class="one wide">来源</th>
<th class="one wide">通过/提交</th>
<th class="one wide">通过率</th>
</tr></thead>
<tbody>
{{range .Rows}}<tr>
<td><div class="{{.MarkerClass}}">{{.Marker}}</div></td>
<td><b>{{.ProblemID}}</b></td>
<td><a href="{{.Href}}">{{.Title}}</a></td>
<td>{{range .Sources}}<a class="ui label {{.Theme}}" href="{{.Href}}" title="{{.Title}}">{{.Trunc}}</a> {{end}}</td>
<td>{{if .Submit.Valid}}{{.Accepted.Value}}/{{.Submit.Value}}{{else}}0/0{{end}}</td>
<td>{{if .HasRate}}{{printf "%.3f" .RatePercent}}%{{else}}0.000%{{end}}</td>
</tr>
{{end}}</tbody></table>
{{template "pagination" .}}
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

   Description 按 PHP 行为原样输出（富文本），其余字段一律走 html/template
   的默认转义。Defunct='Y' 的题目只给提示，不展示任何内容。 */}}
{{define "problemDetail"}}<div class="ui container">
{{if .Contest}}<div class="ui message">
本题目来自比赛 <a href="contest.php?cid={{.Contest.CID}}">{{.Contest.Title}}</a> 第 {{.Contest.PID}} 题
</div>{{end}}
<h1 class="ui header">{{.Title}}</h1>
<div class="ui label">命题人：<span id="creator"></span></div>
{{if eq .Defunct "Y"}}
<div class="ui warning message">该题目已被删除或尚未公开</div>
{{else}}
{{if .Description}}<div class="problem-description">{{raw .Description}}</div>{{end}}
<table class="ui very basic table">
<tr><td class="one wide">Time Limit</td><td>{{trimFloat .TimeLimit}} 秒</td></tr>
<tr><td class="one wide">Memory Limit</td><td>{{.MemoryLimit}} KB</td></tr>
{{if .SPJ}}<tr><td class="one wide">Special Judge</td><td>{{.SPJ}}</td></tr>{{end}}
<tr><td class="one wide">Submit</td><td>{{if .StatsHidden}}?{{else}}{{if .SubmitStat.Valid}}{{.SubmitStat.Int32}}{{else}}0{{end}}{{end}}</td></tr>
<tr><td class="one wide">Accepted</td><td>{{if .StatsHidden}}?{{else}}{{if .AcceptedStat.Valid}}{{.AcceptedStat.Int32}}{{else}}0{{end}}{{end}}</td></tr>
</table>
{{if .Input}}<h3 class="ui header">输入</h3><div>{{raw .Input}}</div>{{end}}
{{if .Output}}<h3 class="ui header">输出</h3><div>{{raw .Output}}</div>{{end}}
{{if .SampleInput.Valid}}<h3 class="ui header">样例输入</h3><pre>{{.SampleInput}}</pre>{{end}}
{{if .SampleOutput.Valid}}<h3 class="ui header">样例输出</h3><pre>{{.SampleOutput}}</pre>{{end}}
{{if .Hint}}{{if not .HintHidden}}<h3 class="ui header">提示</h3><div>{{raw .Hint}}</div>{{end}}{{end}}
{{if .Source.Valid}}<h3 class="ui header">来源</h3><div>{{.Source}}</div>{{end}}
{{template "problemActions" .}}
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
<p>{{.Description}}</p>
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

{{define "pagination"}}{{if or .HasPrev .HasNext}}
{{$tp := .TotalPage}}
<div class="ui pagination menu">
{{if .HasPrev}}<a class="item" href="{{.PrevHref}}">上一页</a>{{end}}
<span class="active item">第 {{.Page}} / {{$tp}} 页（共 {{.Total}} 条）</span>
{{if .HasNext}}<a class="item" href="{{.NextHref}}">下一页</a>{{end}}
</div>{{end}}{{end}}

{{define "alertAndBack"}}<script language="javascript">
alert({{.Message}});
history.go(-1);
</script>{{end}}

{{define "notImplemented"}}<div class="ui warning message">{{.Message}}</div>{{end}}

{{define "submitPage"}}<center>
<form id="frmSolution" action="submit.php{{if .Ajax}}?ajax{{end}}" method="post" enctype="multipart/form-data">
<input type="hidden" name="csrf_token" value="{{csrfToken .Token}}">
{{if .IsContest}}
Problem <span class="blue"><b>{{add .Num 65 | printf "%c"}}</b></span> of Contest <span class="blue"><b>{{.ContestID}}</b></span>
<input id="cid" type="hidden" value="{{.ContestID}}" name="cid">
<input id="pid" type="hidden" value="{{.Num}}" name="pid">
{{else}}
<span style="color:#0000ff">Problem <b>{{.ProblemID}}</b></span>
<input id="problem_id" type="hidden" value="{{.ProblemID}}" name="id">
{{end}}
<span id="language_span">Language:
<select id="language" name="language">
{{range .Languages}}
<option value="{{.ID}}">{{.Name}}</option>
{{end}}
</select>
</span>
<textarea cols="180" rows="20" id="source" name="source" style="width:80%; height:600px;"></textarea>
<input type="submit" value="Submit" class="ui button">
</form>
</center>
{{end}}


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
<p>{{.Description}}</p>
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

{{define "loginForm"}}<div class="ui middle aligned center aligned grid" style="height: 500px;">
<div class="row"><div class="column" style="max-width: 450px">
<h2 class="ui image header"><div class="content" style="margin-bottom: 10px;">登录</div></h2>
<form class="ui large form" action="/login.php" method="post">
<div class="ui segment">
{{template "vcodeField" .}}
<div class="field"><div class="ui left icon input"><i class="user icon"></i><input name="user_id" placeholder="用户名" type="text" autocomplete="username"></div></div>
<div class="field"><div class="ui left icon input"><i class="lock icon"></i><input name="password" placeholder="密码" type="password" autocomplete="current-password"></div></div>
{{template "csrfInput" .}}
<button class="ui fluid large submit button" type="submit">登录</button>
</div></form>
<div class="ui message"><a href="/registerpage.php">注册</a> | <a href="/lostpassword.php">忘记密码</a></div>
</div></div></div>{{end}}

{{define "registerForm"}}<div class="ui container" style="max-width: 450px; margin-top: 50px;">
<h2>注册</h2>
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

{{define "vcodeField"}}{{if .ShowVCode}}<div class="field"><div class="ui left icon input"><i class="shield icon"></i><input name="vcode" placeholder="验证码" type="text" autocomplete="off"></div></div>{{end}}{{end}}

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

{{define "statusPage"}}<div class="ui container">
<form class="ui form" method="GET"><div class="fields">
<div class="field"><label>比赛</label><input name="cid" value="{{.Filter.CID}}" placeholder="比赛 ID"></div>
<div class="field"><label>题目</label><input name="problem_id" value="{{.Filter.ProblemID}}" placeholder="题目 ID"></div>
<div class="field"><label>用户</label><input name="user_id" value="{{.Filter.UserID}}" placeholder="用户名"></div>
{{/* range 内部 . 会被重绑定成 map 的元素，因此先把筛选值取到局部变量。
     直接在 range 里写 .Filter.Language 会渲染失败（页面 500）。 */}}
{{$selLang := .Filter.SelectedLanguage}}
{{$selResult := .Filter.SelectedResult}}
<div class="field"><label>语言</label>
<select name="language">
<option value="">全部</option>
{{range $i, $name := languageNames}}
<option value="{{$i}}"{{if eq $i (int $selLang)}} selected{{end}}>{{$name}}</option>
{{end}}
</select>
</div>
<div class="field"><label>结果</label>
<select name="jresult">
<option value="">全部</option>
{{range $i, $label := resultLabels}}
<option value="{{$i}}"{{if eq $i (int $selResult)}} selected{{end}}>{{$label}}</option>
{{end}}
</select>
</div>
<div class="field"><label>学校</label><input name="school" value="{{.Filter.School}}" placeholder="学校"></div>
<div class="field"><label>群组</label><input name="group_name" value="{{.Filter.GroupName}}" placeholder="群组"></div>
<div class="field">
<div class="ui checkbox"><input type="checkbox" name="showsim" value="1"{{if eq .Filter.ShowSim 1}} checked{{end}}><label>相似度</label></div>
</div>
<div class="field"><button class="ui button" type="submit">筛选</button></div>
</div></form>

<table id="result-tab" class="ui celled table"><thead>
<tr><th>ID</th><th>用户</th><th>题目</th><th>结果</th><th>内存</th><th>时间</th><th>语言</th><th>提交时间</th></tr>
</thead><tbody>
{{range .Rows}}<tr{{if not .Masked}} data-sid="{{.SolutionID}}" data-result="{{.Result}}"{{end}}>
<td>{{.SolutionID}}</td>
<td>{{.UserID}}</td>
<td>{{.ProblemID}}</td>
<td><span class="{{.ResultClass}}">{{.ResultLabel}}</span></td>
<td class="center">{{.MemoryText}}</td>
<td class="center">{{.TimeText}}</td>
<td class="center">{{languageName .Language}}</td>
<td>{{.InDate.Format "2006-01-02 15:04:05"}}</td>
</tr>
{{end}}</tbody></table>
{{template "statusPagination" .}}

{{/* 自动刷新：对应 template/syzoj/auto_refresh.js 的轮询分支。PHP 用
   status-ajax.php 拉回 "result,memory KB,time ms,judger,pass_rate,user_id"，
   这里只更新结果/内存/时间三格。封榜行不带 data-sid，不参与轮询。 */}}
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
<h1 style="text-align:left;">Contest{{.CID}} - {{.Title}}
<a class="ui small blue button" href="contestrank.xls.php?cid={{.CID}}">下载</a>
<a class="ui small green button" href="contestrank4.php?cid={{.CID}}">复盘</a>
<button class="ui small yellow button" onclick='$("tr[class!=active]").toggle();'>显示/隐藏</button>
</h1>
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
<h1 style="text-align:left;">Contest{{.CID}} -- {{.Title}}
<a class="ui small blue button" href="contestrank-oi.php?spa&amp;down&amp;cid={{.CID}}">下载</a>
<a class="ui small green button" href="contestrank5.php?cid={{.CID}}">复盘</a>
<button class="ui small yellow button" onclick='$("tr[class!=active]").toggle();'>显示/隐藏</button>
</h1>
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
<h1 style="text-align:left;">Contest{{.CID}} - {{.Title}} - 补题榜
<button class="ui small yellow button" onclick='$("tr[class!=active]").toggle();'>显示/隐藏</button>
<a class="ui small blue button" href="{{.BackURL}}">返回</a>
</h1>
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
<h1 style="text-align:left;">Contest{{.CID}} -- {{.Title}} -- 补题榜
<button class="ui small yellow button" onclick='$("tr[class!=active]").toggle();'>显示/隐藏</button>
<a class="ui small blue button" href="{{.BackURL}}">返回</a>
</h1>
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

{{/* showsourcePage 是源代码查看页，对应 template/syzoj/showsource.php。

   PHP 用 SyntaxHighlighter 高亮 + ob 抓取的注释头；Go 退化成单个 <pre>，
   内容由 handler 拼好并交给 html/template 转义（源码/昵称里的尖括号不再裸奔）。 */}}
{{define "showsourcePage"}}
<div class="padding">
{{if .OK}}<pre class="brush:{{.Brush}};">{{.Source}}</pre>
{{else}}I am sorry, You could not view this code!{{end}}
</div>
{{end}}

{{/* ranklist 是全局排名页，对应 template/syzoj/ranklist.php。

   顶部是 scope 导航（日/周/月/上月/年）与 prefix、group_name 两个搜索框；
   表格固定 7 列；分页条按 page_size 分段拼接 start 偏移。
   starred>0 的账号在用户名前后各加一颗星，与 PHP :110-118 一致。 */}}
{{define "ranklist"}}
<div class="padding">
<a href="ranklist.php?scope=d" class="ui mini button">{{msgRanklistDay}}</a>
<a href="ranklist.php?scope=w" class="ui mini button primary">{{msgRanklistWeek}}</a>
<a href="ranklist.php?scope=m" class="ui mini button red">{{msgRanklistMonth}}</a>
<a href="ranklist.php?scope=m&pre=1" class="ui mini button green">{{msgRanklistLastMonth}}</a>
<a href="ranklist.php?scope=y" class="ui mini button violet">{{msgRanklistYear}}</a>
<form action="ranklist.php" class="ui mini form" method="get" role="form" style="margin-bottom: 25px; text-align: right;">
<div class="ui action left icon input inline" style="width: 180px; margin-right: 77px;">
<i class="search icon"></i><input name="prefix" placeholder="{{msgUser}}" type="text" value="{{.Prefix}}">
<button class="ui mini button" type="submit">{{msgSearch}}</button>
</div>
<div class="ui action left icon input inline" style="width: 180px; margin-right: 77px;">
<i class="search icon"></i><input name="group_name" placeholder="{{msgGroupName}}" type="text" value="{{.GroupName}}">
<button class="ui mini button" type="submit">{{msgSearch}}</button>
</div>
</form>
<table class="ui very basic center aligned table" style="table-layout: fixed;">
<thead>
<tr>
<th style="width: 60px;">{{msgNumber}}</th>
<th style="width: 180px;">{{msgUser}}</th>
<th>{{msgNick}}</th>
<th>{{msgGroupName}}</th>
<th style="width: 100px;">{{msgSolved}}</th>
<th style="width: 100px;">{{msgSubmit}}</th>
<th style="width: 100px;">{{msgRatio}}</th>
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
  if (typeof echarts === 'undefined') return;
  var el = document.getElementById('submission');
  if (!el) return;
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
<h1>{{.NewsTitle}}</h1>
<p style="margin-bottom: 5px;">
<b style="margin-right: 30px;"><i class="edit icon"></i><a class="black-link" href="userinfo.php?user={{.NewsWriter}}"> {{.NewsWriter}}</a></b>
<b style="margin-right: 30px;"><i class="calendar icon"></i> {{.NewsDate}}</b>
</p>
<div class="ui existing segment" style="overflow-y:overlay;">
<div id="content" class="font-content">{{.NewsContent}}</div>
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

{{/* adminIndexPage 是后台首页，对应 admin/index.php。 */}}
{{define "adminIndexPage"}}
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>{{.Title}}</title>
</head>
<frameset cols="16%,*">
<frame name="menu" src="/admin/menu.php">
<frame name="main" src="/admin/help.php">
</frameset>
</html>
{{end}}

{{/* adminMenuPage 是后台菜单，对应 admin/menu.php。 */}}
{{define "adminMenuPage"}}
<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>{{.Title}}</title>
</head>
<body>
<div id="sidebar">
<a class="btn btn-sm" href="/admin/help.php" target="main">{{.Title}}</a>
<a class="btn btn-sm" href="/status.php" target="_top">查看OJ</a>

<div class="sidebar-section">
<h3>新闻管理</h3>
<ul>
{{if .IsAdmin}}
<li><a href="/admin/news_list.php" target="main">新闻列表</a></li>
<li><a href="/admin/news_add_page.php" target="main">添加新闻</a></li>
{{end}}
</ul>
</div>

<div class="sidebar-section">
<h3>用户管理</h3>
<ul>
{{if or .IsAdmin .IsPasswordSetter}}
<li><a href="/admin/user_list.php" target="main">用户列表</a></li>
{{end}}
{{if or .IsAdmin .IsUserAdder}}
<li><a href="/admin/user_add.php" target="main">添加用户</a></li>
<li><a href="/admin/user_import.php" target="main">导入用户</a></li>
{{end}}
{{if or .IsAdmin .IsPasswordSetter}}
<li><a href="/admin/changepass.php" target="main">修改密码</a></li>
{{end}}
{{if .IsAdmin}}
<li><a href="/admin/privilege_list.php" target="main">权限列表</a></li>
<li><a href="/admin/privilege_add.php" target="main">添加权限</a></li>
{{end}}
</ul>
</div>

<div class="sidebar-section">
<h3>题目管理</h3>
<ul>
{{if or .IsAdmin .IsProblemEditor .IsContestCreator}}
<li><a href="/admin/problem_list.php" target="main">题目列表</a></li>
{{end}}
{{if or .IsAdmin .IsProblemEditor}}
<li><a href="/admin/problem_add_page.php" target="main">添加题目</a></li>
<li><a href="/admin/problem_import.php" target="main">导入题目</a></li>
<li><a href="/admin/problem_export.php" target="main">导出题目</a></li>
{{end}}
</ul>
</div>

<div class="sidebar-section">
<h3>比赛管理</h3>
<ul>
{{if or .IsAdmin .IsContestCreator}}
<li><a href="/admin/contest_list.php" target="main">比赛列表</a></li>
<li><a href="/admin/contest_add.php" target="main">添加比赛</a></li>
<li><a href="/admin/team_generate.php" target="main">队伍生成</a></li>
{{end}}
</ul>
</div>

{{if .IsAdmin}}
<div class="sidebar-section">
<h3>系统管理</h3>
<ul>
<li><a href="/admin/rejudge.php" target="main">重新判题</a></li>
<li><a href="/admin/backup.php" target="main">备份数据库</a></li>
<li><a href="/online.php" target="main">在线用户</a></li>
</ul>
</div>
{{end}}
</div>
</body>
</html>
{{end}}

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

`

// renderPage 渲染具名页面模板并套上外壳，写出响应。
func renderPage(w http.ResponseWriter, code int, name, title string, data any) {
	var body bytes.Buffer
	if err := pageTemplates.ExecuteTemplate(&body, name, data); err != nil {
		http.Error(w, "页面渲染失败", http.StatusInternalServerError)
		return
	}
	var out bytes.Buffer
	if err := pageTemplates.ExecuteTemplate(&out, "layout", pageData{
		Title:   title,
		Content: template.HTML(body.String()),
	}); err != nil {
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
}

// contestProblemsView 是比赛题目页模板的数据。
type contestProblemsView struct {
	Contest  dbgen.GetContestByIDRow
	CID      int32
	Problems []ContestProblemItem
	Flags    contestProblemFlags
}

// notImplementedView 是尚未迁移到 Go 的页面统一占位响应。
type notImplementedView struct {
	Message string
}
