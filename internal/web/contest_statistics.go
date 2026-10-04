// 比赛统计页（conteststatistics.php）。
//
// 这个页面的逻辑密度在"聚合"和"题号链接"两处：
//   - 聚合是把每一条 solution 同时累加到「所属题」和「合计行」两组桶里，
//     语言维度用 language+11 作为列号，与 PHP 的数组下标完全对应；
//   - 题号链接分赛中和赛后两种：赛中直接按 cid&pid 打开，
//     赛后要先确认该题没有被别的"未结束比赛"占用，才放出 problem.php?id。
//
// 细节都尽量贴着 PHP 原文写，注释里标了对应行号。

package web

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// 统计页用到的 $MSG_*（lang/cn.php），未见于 contest_error.go 的这里补齐。
const (
	msgContestPend   = "未开始"    // $MSG_Contest_Pending :492
	msgEnded         = "已结束"    // $MSG_Ended :250
	msgRunning       = "运行中"    // $MSG_Running :245
	msgStart         = "开始于"    // $MSG_Start :246
	msgEnd           = "结束于"    // $MSG_End :247
	msgTotalTime     = "总赛时"    // $MSG_TotalTime :248
	msgLeftTime      = "剩余"     // $MSG_LeftTime :249
	msgServerTime    = "服务器时间"  // $MSG_SERVER_TIME :218
	msgEndTime       = "结束时间"   // $MSG_END_TIME :220
	msgContestStatus = "作业比赛状态" // $MSG_CONTEST_STATUS :225
	msgProblems      = "问题"     // $MSG_PROBLEMS :6
	msgIPVerify      = "IP验证"   // $MSG_IP_VERIFICATION :229
	msgSetLoginIP    = "指定登录IP" // $MSG_SET_LOGIN_IP :200
)

// statLanguageNames 是 const.inc.php:12 的 $language_name。
//
// 与 status 页的 languageNames（只列本机装了的语言）不同：统计表的语言列
// 直接取自这份全量表，列名也照抄，哪怕某语言本机没装 ——
// 只要历史上有过该语言的提交，这一列就该出现，否则合计行对不上。
var statLanguageNames = [27]string{
	"C", "C++", "Pascal", "Java", "Ruby", "Bash", "Python", "PHP",
	"Perl", "C#", "Obj-C", "FreeBasic", "Scheme", "Clang", "Clang++",
	"Lua", "JavaScript", "Go", "SQL", "Fortran", "Matlab", "Cobol",
	"R", "Scratch3", "Cangjie", "Rust", "UnknownLanguage",
}

// statResultLabels 是统计表 j=0..10 的表头（template :137-147）。
//
// j=9 的 "|" 对应 result 13..15 的汇总列，j=10 是每题的提交总数。
var statResultLabels = [11]string{
	"AC", "PE", "WA", "TLE", "MLE", "OLE", "RE", "CE", "TR", "|", "Total",
}

// contestStatRow 是统计表的一行。
type contestStatRow struct {
	Label string
	// Link 为空表示题号是纯文本。只有题目行会用到。
	Link  string
	Cells []string
	// IsOdd 对应 PHP 的 oddrow / evenrow 斑马纹（template :167-170）。
	IsOdd bool
}

// contestStatDaily 是图表的一个数据点。
type contestStatDaily struct {
	Day    string
	Submit int32
	AC     int32
}

// contestStatisticsView 是统计页模板的数据。
type contestStatisticsView struct {
	CID         int32
	Title       string
	Description string
	ServerTime  string
	LockTime    string // 非空时渲染 "Lock Board Time: ..."
	IsPrivate   bool

	// 第一行状态：已结束 / 开始于+总赛时 / 运行中+剩余。
	StatusText   string
	StatusTone   string // muted / success / danger
	StatusExtra  string // 总赛时 / 剩余
	StatusLength string

	// 第二行状态（$MSG_CONTEST_STATUS）。
	ContestStatusText string
	ContestStatusTone string

	StartTime string
	EndTime   string

	CanManage bool // 管理员或 contest_creator，显示三个管理按钮

	LockedMsg string

	Header []string
	Rows   []contestStatRow
	Total  contestStatRow

	Chart []contestStatDaily
}

// handleContestStatistics 对应 conteststatistics.php:1-124 + 模板。
func (s *Server) handleContestStatistics(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	// conteststatistics.php:10 —— 缺 cid 直接 die，纯文本，连外壳都不套。
	if !q.Has("cid") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("No Such Contest!"))
		return
	}

	// :11 intval，无 abs（与 contest.php 的 abs(intval()) 不同）。
	cid := int32(parseInt(q.Get("cid")))
	now := time.Now()

	// :14-21 比赛必须存在且已开始，否则统一按"未开始"错误页处理。
	contest, err := s.queries().GetContestByID(r.Context(), cid)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.log.Error("查询比赛失败", "err", err, "cid", cid)
		http.Error(w, "比赛信息暂时不可用", http.StatusInternalServerError)
		return
	}
	started := err == nil && contest.StartTime.Valid && now.After(contest.StartTime.Time)
	if !started {
		// :18 $view_errors = "$MSG_CONTEST $MSG_Contest_Pending!"，
		// 走 error.php（HTTP 200）。
		s.renderContestStatsError(w, msgContestWord+" "+msgContestPend+"!")
		return
	}

	// :27-37 NOIP 判定。注意这里**不**走 contestNoIPFlags：
	// 统计页只看标题关键字与 contest_type&20，且豁免名单里没有 problem_editor。
	id := s.resolveIdentity(w, r)
	if s.contestStatsNOIP(contest, cid, id, now) {
		// :34 $view_errors = "<h2>$MSG_NOIP_WARNING</h2>"。
		s.renderContestStatsError(w, "<h2>"+s.noipWarning()+"</h2>")
		return
	}

	// :41-44 pid_cnt 只数 contest_problem 行数，不 JOIN problem。
	problemIDs, err := s.queries().ListContestProblemIDs(r.Context(), cid)
	if err != nil {
		s.log.Error("查询比赛题目失败", "err", err, "cid", cid)
		http.Error(w, "比赛统计暂时不可用", http.StatusInternalServerError)
		return
	}
	pidCnt := int32(len(problemIDs))

	// :47-79 聚合。res 把 result<4 与 result=12 折叠到 j=8(TR)，
	// language 映射到 j=lang+11。
	stats, err := s.queries().ListContestSolutionStats(r.Context(), cid)
	if err != nil {
		s.log.Error("查询比赛提交失败", "err", err, "cid", cid)
		http.Error(w, "比赛统计暂时不可用", http.StatusInternalServerError)
		return
	}
	agg := map[int32]map[int]int32{}
	bump := func(pid int32, j int) {
		m := agg[pid]
		if m == nil {
			m = map[int]int32{}
			agg[pid] = m
		}
		m[j]++
	}
	for _, sol := range stats {
		res := int(sol.Result) - 4
		if res < 0 {
			res = 8
		}
		lag := int(sol.Language)
		bump(sol.Num, res)
		bump(sol.Num, lag+11)
		bump(pidCnt, res)
		bump(pidCnt, lag+11)
		bump(sol.Num, 10)
		bump(pidCnt, 10)
	}

	// :148-157 语言列：只输出合计行有非零计数的语言。
	var langCols []int
	for i := range statLanguageNames {
		if agg[pidCnt][i+11] > 0 {
			langCols = append(langCols, i+11)
		}
	}
	sort.Ints(langCols)

	header := make([]string, 0, 11+len(langCols))
	header = append(header, statResultLabels[:]...)
	for _, j := range langCols {
		header = append(header, statLanguageNames[j-11])
	}

	// num -> problem_id，供赛后链接查询（template :177 按 num 取题）。
	pidByNum := make(map[int32]int32, len(problemIDs))
	for _, p := range problemIDs {
		pidByNum[p.Num] = p.ProblemID
	}

	endTime := contest.EndTime.Time
	running := now.Before(endTime)

	rows := make([]contestStatRow, 0, pidCnt)
	for i := int32(0); i < pidCnt; i++ {
		row := contestStatRow{Label: pidLetter(i)}
		if running {
			// :172-174 赛中：按 cid&pid 打开。
			row.Link = "problem.php?cid=" + itoa32(cid) + "&pid=" + itoa32(i)
		} else {
			// :175-192 赛后：题被未结束比赛占用则纯文本，否则按 problem_id 打开。
			pid := pidByNum[i]
			used, err := s.queries().ProblemUsedInOngoingContest(r.Context(), pid)
			if err != nil {
				s.log.Error("查询题目占用失败", "err", err, "pid", pid)
				http.Error(w, "比赛统计暂时不可用", http.StatusInternalServerError)
				return
			}
			if !used {
				row.Link = "problem.php?id=" + itoa32(pid)
			}
		}
		row.Cells = statCells(agg[i], langCols)
		row.IsOdd = i&1 == 1
		rows = append(rows, row)
	}
	total := contestStatRow{Label: "Total", Cells: statCells(agg[pidCnt], langCols)}

	// :82-106 图表数据。chart_data_ac 按同一批日期对齐，缺失记 0。
	chart, err := s.contestStatsChart(r.Context(), cid)
	if err != nil {
		s.log.Error("查询统计图表失败", "err", err, "cid", cid)
		http.Error(w, "比赛统计暂时不可用", http.StatusInternalServerError)
		return
	}

	startTime := contest.StartTime.Time
	view := contestStatisticsView{
		CID:         cid,
		Title:       contest.Title.String,
		Description: contest.Description.String,
		ServerTime:  now.Format("2006-01-02 15:04:05"),
		IsPrivate:   contest.Private != 0,
		StartTime:   startTime.Format("2006-01-02 15:04:05"),
		EndTime:     endTime.Format("2006-01-02 15:04:05"),
		CanManage:   id.Admin || id.ContestCreator,
		Header:      header,
		Rows:        rows,
		Total:       total,
		Chart:       chart,
	}

	// :109-117 封榜。生产 RankLockPercent=0，所以 view_lock_time==end，
	// 仅当 now 落在 (end, end+delay) 区间才提示。
	lockPercent := s.cfg.RankLockPercent
	if lockPercent != 0 {
		viewLockTime := endTime.Add(-time.Duration(float64(endTime.Sub(startTime)) * lockPercent))
		view.LockTime = viewLockTime.Format("2006-01-02 15:04:05")
	}
	viewLockTime := endTime.Add(-time.Duration(float64(endTime.Sub(startTime)) * lockPercent))
	if now.After(viewLockTime) && now.Before(endTime.Add(rankLockDelaySec*time.Second)) {
		view.LockedMsg = "The board has been locked."
	}

	// 第一行状态（:72-82）。
	switch {
	case now.After(endTime):
		view.StatusText, view.StatusTone = msgEnded, "muted"
	case now.Before(startTime):
		view.StatusText, view.StatusTone = msgStart, "success"
		view.StatusExtra = msgTotalTime
		view.StatusLength = formatTimeLength(int(endTime.Sub(startTime) / time.Second))
	default:
		view.StatusText, view.StatusTone = msgRunning, "danger"
		view.StatusExtra = msgLeftTime
		view.StatusLength = formatTimeLength(int(endTime.Sub(now) / time.Second))
	}
	// 第二行状态（:87-96）。
	switch {
	case now.After(endTime):
		view.ContestStatusText, view.ContestStatusTone = msgEnd, "muted"
	case now.Before(startTime):
		view.ContestStatusText, view.ContestStatusTone = msgStart, "success"
	default:
		view.ContestStatusText, view.ContestStatusTone = msgRunning, "danger"
	}

	renderPage(w, http.StatusOK, "contestStatistics", msgStatistics+" - "+s.cfg.OJName, view)
}

// statCells 把一行聚合结果摊平成与表头同序的字符串。
//
// 固定列 j=0..10 恒定输出（未设键则空），语言列只输出全局用到的那些。
func statCells(m map[int]int32, langCols []int) []string {
	cells := make([]string, 0, 11+len(langCols))
	cell := func(j int) string {
		if v := m[j]; v != 0 {
			return strconv.Itoa(int(v))
		}
		return ""
	}
	for j := 0; j < 11; j++ {
		cells = append(cells, cell(j))
	}
	for _, j := range langCols {
		cells = append(cells, cell(j))
	}
	return cells
}

// contestStatsNOIP 复刻 conteststatistics.php:27-32 的 NOIP 判定。
func (s *Server) contestStatsNOIP(contest dbgen.GetContestByIDRow, cid int32, id Identity, now time.Time) bool {
	if !now.Before(contest.EndTime.Time) {
		return false
	}
	keyword := s.cfg.NoIPKeyword
	titleHit := keyword != "" &&
		strings.Contains(strings.ToLower(contest.Title.String), strings.ToLower(keyword))
	// contest_type & 20（const.inc.php:18 的 1<<2|1<<4）。
	typeHit := (contest.ContestType.Int16 & 20) > 0
	noip := titleHit || typeHit
	if noip && (id.Admin || id.HasContestTimeLimitUsed(cid) || id.SourceBrowser || id.ContestCreator) {
		noip = false
	}
	return noip
}

// noipWarning 返回 $MSG_NOIP_WARNING（lang/cn.php:216）。
func (s *Server) noipWarning() string {
	return s.cfg.NoIPKeyword + " 比赛进行中，结束后才能查看结果。"
}

// contestStatsChart 组装图表数据（conteststatistics.php:82-106）。
func (s *Server) contestStatsChart(ctx context.Context, cid int32) ([]contestStatDaily, error) {
	all, err := s.queries().ListContestDailySubmits(ctx, cid)
	if err != nil {
		return nil, err
	}
	ac, err := s.queries().ListContestDailyAC(ctx, cid)
	if err != nil {
		return nil, err
	}
	acByDay := make(map[string]int32, len(ac))
	for _, r := range ac {
		acByDay[r.Day] = r.Count
	}
	out := make([]contestStatDaily, 0, len(all))
	for _, r := range all {
		out = append(out, contestStatDaily{Day: r.Day, Submit: r.Count, AC: acByDay[r.Day]})
	}
	return out, nil
}

// renderContestStatsError 渲染统计页的两类错误（pending / NOIP）。
//
// 两者都走 error.php，状态码 200。Message 里可能含 <h2>，是 PHP 原文的一部分。
func (s *Server) renderContestStatsError(w http.ResponseWriter, message string) {
	renderPage(w, http.StatusOK, "contestStatsError", "错误信息", contestStatsErrorView{Message: message})
}

// contestStatsErrorView 是统计页错误模板的数据。
type contestStatsErrorView struct {
	Message string
}
