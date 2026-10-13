// 状态页（/status.php）完整实现。
//
// 对应 PHP status.php 的完整逻辑：
//   - 练习模式（无 cid）：全站提交记录，支持用户/题目/语言/结果/学校/群组/Top/ShowSim 筛选
//   - 比赛模式（cid > 0）：该比赛的提交记录，支持 num/用户/语言/结果 筛选，rank lock / NOIP 锁榜
//   - problemstatus.php?id=N：单题统计，由 problemstatus 路由处理（暂不实现）
//
// 关键逻辑与 PHP 保持一致：
//   - 权限可见性：管理员/源码浏览员看全站，普通用户只看练习+自己，游客只看练习非 guest
//   - rank lock：比赛进行中且时间过 lock_time 时，隐藏 result/内存/时间
//   - NOIP 锁榜：比赛标题含 noip 关键字或 contest_type&16，且未结束，管理员/管理员/代码审查员/比赛创建者豁免
//   - rank lock/NOIP 时 result=0/1/2/3 显示为 "判题中"，内存/时间隐藏

package web

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/sempr/hustoj-go/pkg/dbgen"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const statusPageSize = 50

// noipContestTypeBit 是 contest_type 里标记 NOIP 模式的位（status.php 的 $noip 判定）。
const noipContestTypeBit = 16

// StatusRow 单条记录（对应 GetStatusListRow / GetStatusListContestRow）。
type StatusRow struct {
	SolutionID   uint32
	ProblemID    int32
	Num          int32
	UserID       string
	Language     int32
	Result       int16
	InDate       time.Time
	Time         int32
	Memory       int32
	CodeLength   int32
	Nick         string
	GroupName    string
	Starred      int32
	ContestID    sql.NullInt32
	ContestTitle sql.NullString
	ContestType  sql.NullInt16
	ProblemTitle sql.NullString

	// Masked 表示该行落在封榜区间内、且不是访问者自己的提交。
	// 封榜期内这些行的运行结果与资源占用必须隐藏，否则比赛后半场
	// 就能通过不断提交探测对手的解题情况（status.php:402）。
	Masked bool

	// CanViewSource 表示访问者有权查看该提交的源码，语言列将渲染成
	// showsource.php?id= 链接（对齐 status.php:444-452：本人或 source_browser）。
	CanViewSource bool
}

// ResultLabel 返回该行的结果文案，封榜行统一显示为 PHP 的 "----"。
func (r StatusRow) ResultLabel() string {
	if r.Masked {
		return "----"
	}
	return judgeResultLabel(int32(r.Result))
}

// ResultClass 返回该行的 CSS 类。封榜行不给状态色，避免通过颜色
// 侧信道泄露结果。
func (r StatusRow) ResultClass() string {
	if r.Masked {
		return ""
	}
	return judgeResultClass(int32(r.Result))
}

// MemoryText 返回内存占用文案，封榜行隐藏具体数值。
func (r StatusRow) MemoryText() string {
	if r.Masked {
		return ""
	}
	return strconv.Itoa(int(r.Memory)) + " KiB"
}

// TimeText 返回耗时文案，封榜行隐藏具体数值。
func (r StatusRow) TimeText() string {
	if r.Masked {
		return ""
	}
	return strconv.Itoa(int(r.Time)) + " ms"
}

// StatusPage 页面数据。
//
// 分页是 keyset 式的：Top 是当前页第一行（solution_id 最大）的 ID，
// Bottom 是最后一行（最小）的 ID，PrevTop 是"上一页"链接的游标。
// 这三个值直接对应 PHP 的 $top / $bottom / $_GET['prevtop']
// （status.php:250-289、template/syzoj/status.php:128-141）。
// 刻意不统计总数 —— PHP 也不统计，多一次 COUNT 只会拖慢这个高频页面。
type StatusPage struct {
	Rows    []StatusRow
	Top     int64
	Bottom  int64
	PrevTop int64
	Filter  StatusFilter
	CID     int32
}

// StatusFilter 筛选参数（含 Page）。
//
// Language 与 Result 的"未设置"用 -1 表示，而不是 0。
//
// PHP 用 isset($_GET[...]) 区分"没传"和"传了 0"（status.php:156、:170）：
// 两者含义完全不同 —— language=0 是"只看 C"，而没传 language 是"看全部"。
// 这里若用 0 当默认值，没带参数的状态页会被静默筛成"只看 C 的提交"，
// 而 result=0 更是把整页限制成"等待中"，页面几乎永远是空的。
type StatusFilter struct {
	CID       int32
	ProblemID int32
	UserID    string
	Language  int32
	Result    int32
	ShowSim   int32
	Top       int64
	PrevTop   int64
	School    string
	GroupName string

	// HasLanguage / HasResult 记录 Language、Result 是否真的被请求指定。
	//
	// 这两个字段不能用 0 当"未设置"的哨兵：language=0（C）和
	// jresult=0（等待中）都是合法且常用的筛选值。PHP 用 isset() 区分
	// 缺省与显式 0（status.php:158/171），直接拿 0 判断会把它误当成
	// 筛选条件。显式记一个布尔量之后，StatusFilter 的零值就是"不过滤"，
	// 直接构造这个结构体的代码（包括测试）不会再意外加上条件。
	HasLanguage bool
	HasResult   bool

	// HasTop 记录 top 参数是否出现。
	//
	// PHP 的判断是 `isset($top) && $top != -1`（status.php:101-104），
	// 不是 `$top > 0`。差别只在 `?top=0`：PHP 会拼上
	// `solution_id<=0` 从而返回空页，Go 这边若跳过条件反而会吐出满页数据。
	// 照抄 PHP 的判定，两端对畸形 URL 的反应才一致。
	HasTop bool
}

// parseStatusFilter 从请求解析筛选参数。
func parseStatusFilter(r *http.Request) StatusFilter {
	q := r.URL.Query()

	// 语言：PHP 把越界值归一成 -1（status.php:158）。
	language := int32(-1)
	hasLanguage := false
	if _, ok := q["language"]; ok {
		if v := int32(parseInt(q.Get("language"))); v >= 0 && v <= maxStatusLanguage {
			language = v
			hasLanguage = true
		}
	}

	_, hasTop := q["top"]

	// 结果：-1 是"不按结果过滤"的保留值（status.php:171）。
	result := int32(-1)
	hasResult := false
	if _, ok := q["jresult"]; ok {
		if v := int32(parseInt(q.Get("jresult"))); v != -1 {
			result = v
			hasResult = true
		}
	}

	return StatusFilter{
		CID:         int32(parseInt(q.Get("cid"))),
		ProblemID:   int32(parseInt(q.Get("problem_id"))),
		UserID:      strings.TrimSpace(q.Get("user_id")),
		Language:    language,
		Result:      result,
		ShowSim:     int32(parseInt(q.Get("showsim"))),
		Top:         int64(parseInt(q.Get("top"))),
		HasTop:      hasTop,
		PrevTop:     int64(parseInt(q.Get("prevtop"))),
		School:      strings.TrimSpace(q.Get("school")),
		GroupName:   strings.TrimSpace(q.Get("group_name")),
		HasLanguage: hasLanguage,
		HasResult:   hasResult,
	}
}

// rankHiddenUsers 是 $OJ_RANK_HIDDEN（include/db_info.inc.php:87）。
//
// 这些用户的提交不进入状态页。它是内嵌在 SQL 里的字符串字面量而非
// 占位符，PHP 侧同样如此；这里用参数化写法，语义相同且不会因配置
// 含引号而拼出坏 SQL。
var rankHiddenUsers = []string{"admin", "zhblue"}

// buildWhere 构建 WHERE 子句（仅练习模式，含基础筛选）。
// 比赛模式直接用 SQL 查询，不走此函数。
//
// 可见性复刻 status.php:81-94 的三分支：
//  1. 管理员 / 代码审查员 / 普通用户主动查自己 —— 放开范围；
//  2. 其余登录用户 —— 只看练习（contest_id 为 0 或 NULL）的提交；
//  3. 游客 —— 再排除 $OJ_RANK_HIDDEN 里的账号。
//
// 分支 2 里普通用户默认看不到自己练习以外的提交，是 PHP 的既有行为
// （status.php:90 的注释即说明这一点），不是遗漏。
func (s *Server) buildWhere(f StatusFilter, id Identity) (string, []interface{}) {
	var sb strings.Builder
	var args []interface{}

	sb.WriteString("WHERE s.problem_id > 0 ")

	// status.php:85 —— 管理员、代码审查员、或用户正在查询自己。
	queryingSelf := id.LoggedIn && f.UserID != "" && f.UserID == id.UserID
	switch {
	case id.SourceBrowser:
		// 代码审查员：全站可见，不加任何额外限制。
	case id.Admin:
		sb.WriteString("AND (s.contest_id = 0 OR s.contest_id IS NULL) ")
	case queryingSelf:
		sb.WriteString("AND (s.contest_id = 0 OR s.contest_id IS NULL) ")
	case id.LoggedIn && id.UserID != "guest":
		sb.WriteString("AND (s.contest_id = 0 OR s.contest_id IS NULL) ")
	default:
		// 游客：默认排除 rank-hidden 账号（admin/zhblue）。
		//
		// 但**显式指定 user_id 时不排除**：否则本人（尤其 admin 这类
		// rank-hidden 账号）提交后跳转的 status.php?user_id=admin 会显示为空，
		// 看起来像"看不到自己的提交记录"。
		sb.WriteString("AND (s.contest_id = 0 OR s.contest_id IS NULL) ")
		if f.UserID == "" {
			sb.WriteString("AND s.user_id NOT IN (?, ?) ")
			args = append(args, rankHiddenUsers[0], rankHiddenUsers[1])
		}
	}

	sb.WriteString(statusFilterWhere(f, &args))

	return sb.String(), args
}

// statusFilterWhere 生成与模式无关的筛选条件（keyset 游标、题目、用户、
// 语言、结果、学校、群组）。
//
// 练习与比赛两种模式共用这一段。此前比赛模式走 sqlc 生成的
// GetStatusListContest，只按 contest_id 取，language/jresult/user/top
// 全部被忽略，而且分页退回 OFFSET —— 也就是说 /status.php?cid=N 是个
// 只会翻到第一页、筛不了语言的死页面。
//
// keyset 游标也放在这里而不是由调用方拼接，就是为了免得某个模式漏掉
// 它、又悄悄退回 OFFSET 分页。
func statusFilterWhere(f StatusFilter, args *[]interface{}) string {
	var sb strings.Builder
	if f.HasTop && f.Top != -1 {
		sb.WriteString("AND s.solution_id <= ? ")
		*args = append(*args, f.Top)
	}
	if f.ProblemID > 0 {
		sb.WriteString("AND s.problem_id = ? ")
		*args = append(*args, f.ProblemID)
	}
	if f.UserID != "" {
		sb.WriteString("AND s.user_id = ? ")
		*args = append(*args, f.UserID)
	}
	if f.HasLanguage {
		sb.WriteString("AND s.language = ? ")
		*args = append(*args, f.Language)
	}
	// 封榜期 PHP 会整个丢掉 jresult 条件（status.php:170 的 && !$lock），
	// 否则用户靠"只看 AC"就能反推对手是否已通过。这里由调用方在封榜时
	// 传 result=-1，见 handleContestStatus。
	if f.HasResult {
		sb.WriteString("AND s.result = ? ")
		*args = append(*args, f.Result)
	}
	if f.School != "" {
		sb.WriteString("AND u.school = ? ")
		*args = append(*args, f.School)
	}
	if f.GroupName != "" {
		sb.WriteString("AND u.group_name = ? ")
		*args = append(*args, f.GroupName)
	}
	return sb.String()
}

// contestLockWindow 计算封榜时刻，并报告当前是否处于封榜期。
//
// 复刻 status.php:69-78：
//
//	lock_time = end_time - (end_time - start_time) * $OJ_RANK_LOCK_PERCENT
//	lock      = (time() > lock_time && time() < end_time)
//
// 返回的 lockTime 只有在 lockActive 为真时才有意义。
// percent <= 0 时 PHP 算出的 lock_time 就等于 end_time，而 now < end_time
// 与 now > end_time 互斥，因此永远不封榜 —— 这里直接返回 false，
// 省掉一次无意义的时间比较。
func contestLockWindow(contest *dbgen.GetContestForRankRow, now time.Time, percent float64) (time.Time, bool) {
	if percent <= 0 {
		return time.Time{}, false
	}
	start, end := contest.StartTime.Time, contest.EndTime.Time
	if start.IsZero() || end.IsZero() {
		return time.Time{}, false
	}
	lockTime := end.Add(-time.Duration(float64(end.Sub(start)) * percent))
	if now.After(lockTime) && now.Before(end) {
		return lockTime, true
	}
	return time.Time{}, false
}

// applyRankLockMasking 标记封榜期内需要隐藏的行。
//
// status.php:402 的条件是 `!$lock || $lock_time > $row['in_date']
// || $row['user_id'] == $_SESSION[user_id]`，取反即"需要隐藏"：
//
//   - 提交时间早于封榜时刻（封榜前交的题，此时结果已定）；
//   - 且不是访问者本人的提交。
//
// 封榜后提交的行不隐藏，因为它们此刻还没有结果可言。
func applyRankLockMasking(rows []StatusRow, lockTime time.Time, viewer string) {
	for i := range rows {
		if rows[i].InDate.After(lockTime) {
			continue
		}
		if viewer != "" && rows[i].UserID == viewer {
			continue
		}
		rows[i].Masked = true
	}
}

// markViewableSources 标记访问者可查看源码的行（语言列渲染为链接）。
//
// 对齐 status.php:444-452：本人或 source_browser 可见；封榜隐藏的行不给链接。
func markViewableSources(rows []StatusRow, id Identity) {
	for i := range rows {
		if rows[i].Masked {
			continue
		}
		if id.SourceBrowser || (id.LoggedIn && strings.EqualFold(rows[i].UserID, id.UserID)) {
			rows[i].CanViewSource = true
		}
	}
}

// statusNOIPExempt 报告该身份是否豁免 NOIP 锁榜。
//
// 对应 status.php:52-56 的四个 session 标记：管理员、限时赛持有人
// （"m<cid>"）、代码审查员、比赛创建者。
func statusNOIPExempt(id Identity, cid int32) bool {
	if id.Admin || id.SourceBrowser || id.ContestCreator {
		return true
	}
	// 限时赛持有人：PHP 查 $_SESSION["m<cid>"]。
	return id.ContestTimeLimitUsed[cid]
}

// isNOIPContest 判断比赛是否启用 NOIP 模式。
//
// 对应 status.php 的 $noip 判定：标题含关键字（$OJ_NOIP_KEYWORD），
// 或 contest_type 的 NOIP 位（16）。比赛结束后不再生效。
func isNOIPContest(contest *dbgen.GetContestForRankRow, keyword string, now time.Time) bool {
	if contest.EndTime.Valid && !now.Before(contest.EndTime.Time) {
		return false
	}
	if keyword != "" && strings.Contains(strings.ToLower(contest.Title.String), strings.ToLower(keyword)) {
		return true
	}
	return (contest.ContestType.Int16 & noipContestTypeBit) != 0
}

// listStatus 练习模式查询。
// listStatus 按 solution_id 倒序取一页提交记录。
//
// 分页用 keyset（top = 最大的 solution_id）而不是 OFFSET，对应
// status.php:101-104 的 `solution_id<=?` 加固定 LIMIT 50。
// OFFSET 分页在 solution 持续插入时会漏行也不重行，状态页是"最新提交"
// 列表，这种漂移会直接表现为重复或跳过某次提交。
func listStatus(ctx context.Context, db *sql.DB, where string, args []interface{}, limit int) ([]StatusRow, error) {
	query := fmt.Sprintf(`
		SELECT s.solution_id, s.problem_id, s.user_id, s.language, s.result, s.in_date,
		       s.time, s.memory, s.code_length, u.nick, u.group_name, u.starred,
		       c.contest_id, c.title as contest_title, c.contest_type,
		       p.title as problem_title
		FROM (
			SELECT * FROM solution s %s ORDER BY solution_id DESC LIMIT ?
		) s
		INNER JOIN users u ON s.user_id = u.user_id AND u.defunct = 'N'
		LEFT JOIN contest c ON s.contest_id = c.contest_id
		LEFT JOIN problem p ON s.problem_id = p.problem_id
	`, where)
	args = append(append([]interface{}{}, args...), limit)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []StatusRow
	for rows.Next() {
		var r StatusRow
		if err := rows.Scan(
			&r.SolutionID, &r.ProblemID, &r.UserID, &r.Language, &r.Result, &r.InDate,
			&r.Time, &r.Memory, &r.CodeLength, &r.Nick, &r.GroupName, &r.Starred,
			&r.ContestID, &r.ContestTitle, &r.ContestType, &r.ProblemTitle,
		); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// handleStatus 处理 GET /status.php。
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	identity := s.resolveIdentity(w, r)
	filter := parseStatusFilter(r)
	now := time.Now()

	var rows []StatusRow
	var err error

	if filter.CID > 0 {
		// 比赛模式
		contest, err := s.queries().GetContestForRank(r.Context(), filter.CID)
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "比赛不存在", http.StatusNotFound)
				return
			}
			s.log.Error("查询比赛失败", "err", err)
			http.Error(w, "比赛信息暂时不可用", http.StatusInternalServerError)
			return
		}

		// 获取完整比赛信息用于准入判定
		fullContest, err := s.queries().GetContestByID(r.Context(), filter.CID)
		if err != nil {
			if err == sql.ErrNoRows {
				http.Error(w, "比赛不存在", http.StatusNotFound)
				return
			}
			s.log.Error("查询比赛失败", "err", err)
			http.Error(w, "比赛信息暂时不可用", http.StatusInternalServerError)
			return
		}
		// 检查比赛准入
		access, err := EvaluateContestAccess(r.Context(), s.queries(), fullContest, identity, time.Now(), remoteIP(r), hasPHPSession(r))
		if err != nil {
			s.log.Error("比赛准入判定失败", "err", err)
			http.Error(w, "比赛信息暂时不可用", http.StatusInternalServerError)
			return
		}
		if !access.OK() {
			s.renderPage(w, r, http.StatusOK, "contestError", "比赛准入失败",
				newContestErrorView(fullContest.ContestID, &fullContest, access.Deny, access.Deny == ContestSubnet))
			return
		}

		// NOIP 锁榜（status.php:51-64）：赛中查看比赛提交状态会直接泄题，
		// PHP 干脆整页拦下并提示，只有管理员、限时赛持有人、代码审查员
		// 与比赛创建者能看。这与"封榜"是两套机制，不要混为一谈 ——
		// 封榜只隐藏部分行，NOIP 模式下整页都不可达。
		//
		// 必须放在查询之前：既然整页都不会渲染，就没必要去查提交表。
		if isNOIPContest(&contest, s.cfg.NoIPKeyword, now) && !statusNOIPExempt(identity, filter.CID) {
			s.renderPage(w, r, http.StatusOK, "errorPage", "比赛进行中", map[string]string{
				"Message": "比赛进行中，暂不提供提交状态查询",
			})
			return
		}

		// 封榜判定（status.php:69-78）。
		//
		// $OJ_RANK_LOCK_PERCENT 默认是 0，即默认不封榜；这里保持同一语义，
		// 由配置显式开启，避免两端默认值不一致导致"Go 封榜、PHP 不封榜"。
		lockTime, lockActive := contestLockWindow(&contest, now, s.cfg.RankLockPercent)

		// 封榜期丢弃 jresult 筛选（status.php:170 的 `&& !$lock`）。
		// 不这么做的话，用户在封榜期勾一个"只看 AC"就能精确反推对手是否
		// 已通过，等于把封榜要遮的信息直接问出来。
		qfilter := filter
		if lockActive {
			qfilter.HasResult = false
		}

		var qargs []interface{}
		qwhere := "WHERE s.contest_id = ? " + statusFilterWhere(qfilter, &qargs)
		// contest_id 的占位符写在最前面，所以它的参数也必须排在最前面。
		qargs = append([]interface{}{filter.CID}, qargs...)
		rows, err = listStatus(r.Context(), s.db, qwhere, qargs, statusPageSize)
		if err != nil {
			s.log.Error("查询比赛提交记录失败", "err", err)
			http.Error(w, "提交记录暂时不可用", http.StatusInternalServerError)
			return
		}

		// 封榜期内的行是否隐藏，取决于提交时间与当前用户的身份。
		if lockActive {
			applyRankLockMasking(rows, lockTime, identity.UserID)
		}
		markViewableSources(rows, identity)

		title := fmt.Sprintf("比赛 #%d 提交状态", filter.CID)
		s.renderPage(w, r, http.StatusOK, "statusPage", title, newStatusPage(rows, filter))
		return
	}

	// 练习模式
	where, args := s.buildWhere(filter, identity)
	rows, err = listStatus(r.Context(), s.db, where, args, statusPageSize)
	if err != nil {
		s.log.Error("查询提交记录失败", "err", err)
		http.Error(w, "提交记录暂时不可用", http.StatusInternalServerError)
		return
	}

	title := "状态"
	if filter.ProblemID > 0 {
		title = fmt.Sprintf("题目 %d 提交状态", filter.ProblemID)
	}

	markViewableSources(rows, identity)
	s.renderPage(w, r, http.StatusOK, "statusPage", title, newStatusPage(rows, filter))
}

// 以下辅助函数保持不变...
func judgeResultLabel(result int32) string {
	labels := []string{"等待", "编译中", "运行中", "判题中", "通过", "格式错误", "错误答案", "超时", "内存超限", "输出超限", "运行错误", "编译错误", "受限函数", "非零退出", "信号终止", "内部错误"}
	if int(result) < len(labels) {
		return labels[result]
	}
	return fmt.Sprintf("未知(%d)", result)
}

// judgeResultClasses 是结果码对应的 CSS 类，索引即 result。
// 抽成包级变量以便状态页把同一份映射嵌进自动刷新的 JS。
var judgeResultClasses = []string{"waiting", "compiling", "running", "judging", "ac", "pe", "wa", "tle", "mle", "ole", "re", "ce", "rf", "nzec", "sig", "ie"}

func judgeResultClass(result int32) string {
	if int(result) < len(judgeResultClasses) {
		return judgeResultClasses[result]
	}
	return ""
}

func languageName(lang int32) string {
	names := map[int32]string{
		0: "GCC", 1: "G++", 2: "Java", 3: "Pascal", 4: "C#", 5: "Python", 6: "PHP",
		7: "Perl", 8: "Ruby", 9: "Bash", 10: "Node.js", 11: "Go", 12: "Lua",
		13: "Haskell", 14: "Rust", 15: "Swift", 16: "Kotlin", 17: "Dart",
		25: "Rust",
	}
	if n, ok := names[lang]; ok {
		return n
	}
	return fmt.Sprintf("Lang %d", lang)
}

func (s *Server) registerStatusRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /status.php", s.handleStatus)
}

// statusHref 拼出状态页链接，top/prevtop 是 keyset 游标。
//
// top 表示"只看 solution_id <= top 的提交"，prevtop 记录上一页的游标，
// 这样从下一页返回上一页时不会因为新提交插入而跳过数据。
// 两者都只在非默认值时出现，保持 URL 干净、可缓存。
func statusHref(top int64, prevTop int64, f StatusFilter) template.URL {
	q := url.Values{}
	if f.CID > 0 {
		q.Set("cid", strconv.Itoa(int(f.CID)))
	}
	if f.ProblemID > 0 {
		q.Set("problem_id", strconv.Itoa(int(f.ProblemID)))
	}
	if f.UserID != "" {
		q.Set("user_id", f.UserID)
	}
	if f.HasResult {
		q.Set("jresult", strconv.Itoa(int(f.Result)))
	}
	if f.HasLanguage {
		q.Set("language", strconv.Itoa(int(f.Language)))
	}
	if f.School != "" {
		q.Set("school", f.School)
	}
	if f.GroupName != "" {
		q.Set("group_name", f.GroupName)
	}
	// 游标只从 top/prevtop 两个入参取，绝不从 f.Top 继承。
	//
	// f.Top 是"当前请求的游标"，把它继承进来的话，Top 链接（top=0）
	// 会把当前游标原样带回去，点"回到最新"结果原地不动 —— 实测就是
	// 第二页的 Top 链接仍带着 top=<本页最后一条>。
	//
	// 这里用 top>0 而不是照抄查询侧的 HasTop 判定：链接里的 0 表示
	// "不带游标"，而查询侧的 ?top=0 是 PHP 会照单全收的畸形输入。
	if top > 0 {
		q.Set("top", strconv.FormatInt(top, 10))
	}
	if prevTop > 0 {
		q.Set("prevtop", strconv.FormatInt(prevTop, 10))
	}
	return template.URL("status.php?" + q.Encode())
}

// newStatusPage 从查询结果推导 keyset 游标。
//
// $top 是第一行（solution_id 最大）、$bottom 是最后一行（最小），
// 对应 status.php:289-290。prevTop 取请求里的 prevtop；没有时 PHP 用
// $top + statusPageSize 作为上一页游标（template/syzoj/status.php:134）。
func newStatusPage(rows []StatusRow, f StatusFilter) *StatusPage {
	p := &StatusPage{Rows: rows, Filter: f, CID: f.CID, Bottom: -1, Top: -1, PrevTop: -1}
	if len(rows) > 0 {
		p.Top = int64(rows[0].SolutionID)
		p.Bottom = int64(rows[len(rows)-1].SolutionID)
	}
	if f.PrevTop > 0 {
		p.PrevTop = f.PrevTop
	} else if p.Top > 0 {
		p.PrevTop = p.Top + statusPageSize
	}
	return p
}

// SelectedLanguage / SelectedResult 供模板回显下拉框选中项。
//
// 未指定时返回 -1 而不是 0：0 是合法的 language（C）与 jresult（等待中），
// 直接回显会让"未筛选"的页面看起来像筛了语言 0。
//
// 必须导出 —— html/template 调不到未导出方法，模板里写小写名会在渲染
// 期直接报错（不是编译期），页面返回 500。
func (f StatusFilter) SelectedLanguage() int32 {
	if f.HasLanguage {
		return f.Language
	}
	return -1
}

func (f StatusFilter) SelectedResult() int32 {
	if f.HasResult {
		return f.Result
	}
	return -1
}

// TopHref 是"回到最新"（PHP 的 Top 链接），不带游标。
func (p *StatusPage) TopHref() template.URL { return statusHref(0, 0, p.Filter) }

// prevHref 是"更早的提交"（PHP 的 Prev 链接）。
func (p *StatusPage) PrevHref() template.URL {
	if p.PrevTop <= 0 {
		return p.TopHref()
	}
	return statusHref(p.PrevTop, 0, p.Filter)
}

// nextHref 是"更新的提交"（PHP 的 Next 链接）。
//
// 只有当前页取满时才有意义：不满说明已经到顶，再"下一页"只会重复。
func (p *StatusPage) NextHref() template.URL {
	if len(p.Rows) < statusPageSize || p.Bottom <= 0 {
		return ""
	}
	return statusHref(p.Bottom, p.Top, p.Filter)
}
