// 模板引擎与页面模板定义。
//
// 所有 HTML 页面都通过 html/template 渲染，确保自动转义。
// 所有模板在启动时一次性 Parse 完毕，模板名拼写错误会在启动时报错。

package web

import (
	"bytes"
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
	// int 转换，供模板中将 int32 转为 int 以便比较。
	"int": func(i int32) int { return int(i) },
	// languageNames 和 resultLabels 供 statusPage 模板的 select 选项用。
	"languageNames": func() map[int32]string { return languageNames },
	// coinStars 复刻 contest.php:95 的 min(coin, 5)：题面最多 5 颗星。
	"coinStars":    coinStars,
	"resultLabels": func() map[int32]string { return resultLabels },
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

<table class="ui celled table"><thead>
<tr><th>ID</th><th>用户</th><th>题目</th><th>结果</th><th>内存</th><th>时间</th><th>语言</th><th>提交时间</th></tr>
</thead><tbody>
{{range .Rows}}<tr>
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

// contestListView 是比赛列表页模板的数据。
type contestListView struct {
	Contests []ContestListItem
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
