// 比赛模式的错误页，对应 PHP 的 template/*/error.php。
//
// PHP 在比赛不可访问时一律 exit(0)，即渲染错误页但状态码仍是 200
//（problem.php:66-68、:86-89，contest-check.php:94-95、:172-173）。
// 保留 200 是有意的：比赛未开始、密码错误都是正常状态，
// 用 404 会让浏览器缓存与监控都误判成资源不存在。
//
// 错误页正文由 contest-check.php 的两段 $view_errors 拼出，
// 结构固定为：
//
//	<center>
//	  <h3>作业比赛编号 : {cid} - {标题}</h3>
//	  <p>{描述}</p>
//	  <span class=text-danger>{警告}</span>
//	  <br><br>
//	  名次 / OI名次 / 统计 三个按钮
//	  <br><br>
//	  密码输入表单（POST 到 contest.php?cid=）
//	</center>
//
// 注意标题那一行用的是 $view_title 而不是 $MSG_CONTEST_ID 的固定文案：
// 比赛不存在或已关闭时 $view_title 被改写成"比赛已经关闭!"
// （contest-check.php:48-50），否则是比赛自己的标题。

package web

import "github.com/sempr/hustoj-go/pkg/dbgen"

// 比赛错误页里出现的固定文案，取自 lang/cn.php。
//
// 直接写死中文而不是做成配置：这个站点 $OJ_LANG 固定为 cn，
// 且 HustOJ 的 $MSG_* 分散在 500+ 处模板里，没有单一来源可注入。
// 若日后要支持多语言，应整体改成从 lang 包读，而不是逐页加开关。
const (
	msgContestID          = "作业比赛编号"            // $MSG_CONTEST_ID
	msgPrivateWarning     = "比赛尚未开始或私有，不能查看题目。" // $MSG_PRIVATE_WARNING
	msgStanding           = "名次"                // $MSG_STANDING
	msgStatistics         = "统计"                // $MSG_STATISTICS
	msgSubmit             = "提交"                // $MSG_SUBMIT
	msgPassword           = "密码"                // $MSG_PASSWORD
	msgContestWord        = "竞赛&作业"             // $MSG_CONTEST，密码框 placeholder 的一半
	msgContestClosedTitle = "比赛已经关闭!"           // contest-check.php:48
	msgSubnet             = "子网"                // $MSG_SUBNET

	// 列表页与题目页的表头/文案，同样取自 lang/cn.php。
	msgContestName    = "作业比赛名称" // $MSG_CONTEST_NAME
	msgTime           = "时间"     // $MSG_TIME
	msgContestOpen    = "开放"     // $MSG_CONTEST_OPEN
	msgContestCreator = "创建人"    // $MSG_CONTEST_CREATOR
	msgPublic         = "公开"     // $MSG_Public
	msgPrivate        = "私有"     // $MSG_Private
	msgProblemState   = "状态"
	msgProblemID      = "题目编号"   // $MSG_PROBLEM_ID
	msgTitle          = "标题"     // $MSG_TITLE
	msgAC             = "正确"     // $MSG_AC
	msgSubmitNum      = "提交量"    // $MSG_SUBMIT_NUM
	msgAllContests    = "所有作业比赛" // $MSG_VIEW_ALL_CONTESTS
	msgMyContests     = "我的作业比赛" // $MSG_VIEW_MY_CONTESTS
	msgBack           = "返回上一页"  // $MSG_BACK，error.php:27
	msgCoin           = "积分"     // $MSG_COIN，lang/cn.php:550
	msgNum            = "序号"     // $MSG_NUM，lang/cn.php:561
	msgStartTime      = "开始时间"   // $MSG_START_TIME，lang/cn.php:219
)

// contestErrorView 是错误页模板的数据。
type contestErrorView struct {
	// Deny 供模板里的 contestErrorIsLimited / contestErrorShowContestLinks
	// 判断分支；不直接塞布尔进结构是为了让模板不必知道常量取值。
	Deny ContestDeny
	CID  int32
	// Title 是标题里 "作业比赛编号 : {cid} - " 后面那段。
	// 比赛不存在时为 msgContestClosedTitle。
	Title string
	// Description 对应 $view_description：比赛描述，
	// 或 IP 不在网段内时的 "Not in 子网 {subnet}"。
	Description string
	// Warning 对应 <span class=text-danger> 里的那句。
	Warning string
	// ShowPasswordForm 为真时渲染密码输入框。
	//
	// contest-check.php:157-168 两种路径都会带密码框：
	// 未开始（:89-96）与 !contest_ok（:172-173）都输出同一段表单。
	// 这里保留，因为私有比赛只能靠密码进入。
	ShowPasswordForm bool
}

// contestErrorTitle 返回错误页标题里那段文字，对应 PHP 的 $view_title。
//
// 关键：**比赛本身存在** 时用比赛真实标题，只有"不存在或已关闭"
// 才用"比赛已经关闭!"。传 hasContest 是为了区分这两种情况。
func contestErrorTitle(deny ContestDeny, hasContest bool) string {
	if hasContest {
		switch deny {
		case ContestNotStarted, ContestPrivate, ContestLimited, ContestSubnet:
			// 这几种情况下 PHP 的 $view_title 仍是数据库里的真实标题。
			return ""
		}
	}
	return msgContestClosedTitle
}

// contestErrorShowContestLinks 报告错误页是否给出"名次/OI名次/统计"入口。
//
// 对应 contest-check.php:147-152：这段按钮在 !$contest_ok 的分支里输出，
// 比赛不存在（:48-50）和已关闭（:47）同样会走到那里，所以也算有按钮。
// 唯一例外是个人限时赛超时 —— 它在 :129-135 就 exit 了，
// 只有标题和描述。把限时赛也塞进来会让选手看到"名次"入口并点进一个空榜。
func contestErrorShowContestLinks(deny ContestDeny) bool {
	switch deny {
	case ContestNotFound, ContestClosed, ContestPrivate, ContestSubnet, ContestNotStarted:
		return true
	default:
		return false
	}
}

// contestErrorIsLimited 报告是否是个人限时赛的拒绝（contest-check.php:129）。
//
// 该分支只有 <h3> 与 <p>，没有警告语、按钮和密码框。
func contestErrorIsLimited(deny ContestDeny) bool {
	return deny == ContestLimited
}

// contestSubnetDescription 复刻 contest-check.php:42-45 的描述文案。
//
// `Not in 子网 {subnet}`。比赛不存在时 $subnet 未定义，PHP 会拼出一个
// 尾部带空格的 "Not in 子网 "，这里照搬那个空格 —— 去掉它会让
// 与 PHP 的字节级比对失败。
func contestSubnetDescription(subnet string) string {
	return "Not in " + msgSubnet + " " + subnet
}

// newContestErrorView 由准入结果构造错误页数据。
//
// 参数与 contest-check.php 里拼 $view_errors 时用到的变量一一对应：
//   - contest 为 nil 表示比赛不存在（PHP 的 $rows_cnt == 0），
//     此时标题是"比赛已经关闭!"，描述是网段检查失败那句。
//   - denySubnet 为真表示 IP 不在比赛网段内，描述会被改写成
//     "Not in 子网 {subnet}"（contest-check.php:42-45）。
func newContestErrorView(cid int32, contest *dbgen.GetContestByIDRow, deny ContestDeny, denySubnet bool) contestErrorView {
	v := contestErrorView{
		Deny:             deny,
		CID:              cid,
		Warning:          msgPrivateWarning,
		ShowPasswordForm: !contestErrorIsLimited(deny),
	}
	if contest == nil {
		v.Title = msgContestClosedTitle
		v.Description = contestSubnetDescription("")
		return v
	}
	if contest.Title.String != "" {
		v.Title = contest.Title.String
	} else {
		// PHP 那边 $view_title 直接是库里的空串，不做回落；
		// 这里给个可读值，避免页面上出现 "作业比赛编号 : 5 - " 这种断句。
		v.Title = "比赛" + itoa32(cid)
	}
	if denySubnet {
		v.Description = contestSubnetDescription(subnetOf(contest))
	} else {
		v.Description = contest.Description.String
	}
	return v
}

// subnetOf 取比赛允许的网段。
//
// contest.subnet 是普通 varchar 而非 NULL 列，多数站点为空串；
// 空网段时 in_subnet_of_contest 判定失败，于是描述变成
// "Not in 子网 "（尾部带空格），与 PHP 的未定义变量行为一致。
func subnetOf(c *dbgen.GetContestByIDRow) string { return c.Subnet }

// abs32 取 int32 绝对值，对应 contest-check.php:3 的 abs(intval(...))。
//
// math.Abs 只处理 float64，转回来会在 math.MinInt32 上溢出成负数，
// 而 contest-check.php 里 intval("abc") 得 0、intval("-5") 得 -5 再取 5，
// 两边都落在正常范围，用整数运算足够且可预期。
func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}
