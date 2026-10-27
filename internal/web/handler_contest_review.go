// 比赛补题榜：contestrank4.php（ACM）与 contestrank5.php（OI）。
//
// 这两个页面和 contestrank.php / contestrank-oi.php 是两套独立的实现，不能简单
// 复用 handleContestRankMode 的取数逻辑，差别有四点：
//
//  1. 取数口径完全不同。按 problem_id + 开赛时间取，而不是按 contest_id：
//  部署版 contestrank4.php:120-124 的 SQL 是
//     solution where unix_timestamp(in_date)>=start_time
//                and problem_id in (<contest_problem 的 problem_id>)
//                and user_id not in ('admin','zhblue')
//     也就是"比赛结束后在练习里补做比赛原题"的所有提交都会被统计，既不受
//     contest_id 约束，也没有结束时间上界。这就是页面标题里"补题榜"三个字的
//     含义，MSG_REVIEW_CONTESTRANK 的注释写得很明确。
//  2. 计分数组以 problem_id 为键，模板再用 $j=$pida[$k] 把比赛序号映射回
//     problem_id 去读（template/syzoj/contestrank4.php:84-85）。所以这里照
//     PHP 用 problem_id 调用 AddACM/AddOI，渲染时再翻译成题号，避免错位。
//  3. 部署版没有缺席名单。contestrank.php 会把报名未提交的人追加进 $U 并渲染
//     "缺席"区块（contestrank4.php 完全没有这段），补题榜的语义是"练过什么"
//     而不是"报名了谁"，所以这里不查缺席。
//  4. 锁榜比例与判分阈值另有口径，见下面各处注释。
//
// 与部署版一致、与仓库 phpweb/ 副本不一致的地方：仓库副本给这两个文件加了
// 比赛管理员置顶（global $manager、昵称加 * 前缀）。部署版没有该逻辑，已上线的
// contestrank.php 也没有，Go 侧同样没有，这里保持一致不引入。

package web

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// reviewSolution 是补题榜的取数结果。
//
// 注意没有 ContestID 字段：补题榜的提交可能属于任意比赛甚至不属于任何比赛。
type reviewSolution struct {
	UserID    string
	Nick      string
	Result    int32
	ProblemID int
	InDate    time.Time
	PassRate  float64
}

// reviewPlaceholders 生成 `?,?,?` 形式的占位符列表。
func reviewPlaceholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// reviewProblemFilter 拼出补题榜共用的过滤条件。
//
// 空题目列表时返回 ok=false，让调用方走"空榜"分支而不是发出 SQL：
// 部署版 PHP 直接把 implode 结果塞进 `problem_id in (...)`，没有题目的比赛会
// 生成 `problem_id in ()`，MariaDB 报 1064 语法错误，整页 500。这是明确的上游
// 缺陷，按"修明确缺陷"的原则在 Go 侧规避。
func reviewProblemFilter(problemIDs []int32, start time.Time, hidden []string) (string, []any, bool) {
	if len(problemIDs) == 0 {
		return "", nil, false
	}
	ph := reviewPlaceholders(len(problemIDs))
	args := make([]any, 0, len(problemIDs)+1+len(hidden))
	args = append(args, start)
	for _, id := range problemIDs {
		args = append(args, id)
	}
	cond := fmt.Sprintf("in_date >= ? AND problem_id IN (%s)", ph)
	if len(hidden) > 0 {
		cond += " AND user_id NOT IN (" + reviewPlaceholders(len(hidden)) + ")"
		for _, h := range hidden {
			args = append(args, h)
		}
	}
	return cond, args, true
}

// loadReviewSolutions 查补题榜的提交记录。
//
// ORDER BY user_id, solution_id 不能改：AddACM/AddOI 的错题计数与首过时刻都
// 依赖同一选手同一题按时间先后累加。
func (s *Server) loadReviewSolutions(ctx context.Context, problemIDs []int32,
	start time.Time, hidden []string) ([]reviewSolution, error) {

	cond, args, ok := reviewProblemFilter(problemIDs, start, hidden)
	if !ok {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, nick, result, problem_id, in_date, pass_rate
		FROM solution
		WHERE `+cond+`
		ORDER BY user_id, solution_id
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []reviewSolution
	for rows.Next() {
		var r reviewSolution
		var passRate sql.NullFloat64
		if err := rows.Scan(&r.UserID, &r.Nick, &r.Result, &r.ProblemID, &r.InDate, &passRate); err != nil {
			return nil, err
		}
		r.PassRate = passRate.Float64
		out = append(out, r)
	}
	return out, rows.Err()
}

// loadReviewFirstBlood 查补题榜每题的首杀，同样按 problem_id 分组。
func (s *Server) loadReviewFirstBlood(ctx context.Context, problemIDs []int32,
	start time.Time, hidden []string) (map[int]string, error) {

	fb := make(map[int]string)
	cond, args, ok := reviewProblemFilter(problemIDs, start, hidden)
	if !ok {
		return fb, nil
	}
	// 首杀查询要多一个 result=4 条件，插在 problem_id 的 IN 之后。
	args = append(args, int32(4))
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.problem_id, s.user_id
		FROM solution s,
		     (SELECT problem_id, MIN(solution_id) AS min_id
		      FROM solution
		      WHERE `+cond+` AND result = ?
		      GROUP BY problem_id) c
		WHERE s.solution_id = c.min_id
	`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var problemID int
		var userID string
		if err := rows.Scan(&problemID, &userID); err != nil {
			return nil, err
		}
		fb[problemID] = userID
	}
	return fb, rows.Err()
}

// handleContestReviewRank 处理 GET /contestrank4.php（ACM）与
// GET /contestrank5.php（OI）。
func (s *Server) handleContestReviewRank(w http.ResponseWriter, r *http.Request, mode rankMode) {
	cid := int32(parseInt(r.URL.Query().Get("cid")))
	if cid <= 0 {
		http.Error(w, "缺少 cid", http.StatusBadRequest)
		return
	}

	contest, err := s.queries().GetContestForRank(r.Context(), cid)
	if err != nil {
		if err == sql.ErrNoRows {
			s.renderPage(w, r, http.StatusOK, "errorPage", "比赛不存在",
				map[string]string{"Message": "没有这场比赛"})
			return
		}
		s.log.Error("查询比赛失败", "err", err)
		http.Error(w, "比赛信息暂时不可用", http.StatusInternalServerError)
		return
	}

	identity := s.resolveIdentity(w, r)
	start := contest.StartTime.Time
	end := contest.EndTime.Time
	now := time.Now()

	// contestrank4.php:88-91：尚未开赛不给看。
	if now.Before(start) {
		s.renderPage(w, r, http.StatusOK, "errorPage", "比赛", map[string]string{
			"Message": "比赛尚未开始，暂不提供排名",
		})
		return
	}

	// NOIP / 封榜禁看排名，与 contestrank.php 同口径。
	if !statusNOIPExempt(identity, cid) {
		if isNOIPContest(&contest, s.cfg.NoIPKeyword, now) ||
			s.contestRankLocked(r.Context(), cid, now) {
			s.renderPage(w, r, http.StatusOK, "errorPage", "比赛", map[string]string{
				"Message": "比赛进行中，暂不提供排名查询",
			})
			return
		}
	}

	problems, err := s.queries().GetContestProblemsForRank(r.Context(),
		sql.NullInt32{Int32: cid, Valid: true})
	if err != nil {
		s.log.Error("查询比赛题目失败", "err", err)
		http.Error(w, "比赛题目暂时不可用", http.StatusInternalServerError)
		return
	}

	problemIDs := make([]int32, 0, len(problems))
	for _, p := range problems {
		problemIDs = append(problemIDs, p.ProblemID)
	}

	// 没有题目的比赛：PHP 会拼出 `problem_id in ()` 直接 500，这里给出空榜。
	if len(problemIDs) == 0 {
		s.renderEmptyReviewRank(w, contest.Title.String, cid, mode)
		return
	}

	sols, err := s.loadReviewSolutions(r.Context(), problemIDs, start, s.cfg.RankHidden)
	if err != nil {
		s.log.Error("查询补题榜提交失败", "err", err)
		http.Error(w, "提交记录暂时不可用", http.StatusInternalServerError)
		return
	}

	// 封榜：$lock = end - (end-start)*percent。生产 db_info.inc.php:66 把
	// OJ_RANK_LOCK_PERCENT 固定为 0，所以这里与 contestrank.php 共用同一个
	// 全局配置即可（contestrank5.php 文件里的默认值 1 被 ini 覆盖，从未生效）。
	lockPercent := s.cfg.RankLockPercent
	lock := end.Add(-time.Duration(float64(end.Sub(start)) * lockPercent))
	withinLockDelay := now.Before(end.Add(rankLockDelaySec * time.Second))
	cePenalty := s.cfg.CEPenalty

	teams := make([]*contestTeam, 0, len(sols))
	byUser := make(map[string]*contestTeam)
	for _, sol := range sols {
		t, ok := byUser[sol.UserID]
		if !ok {
			t = newContestTeam(sol.UserID, sol.Nick)
			byUser[sol.UserID] = t
			teams = append(teams, t)
		}
		sec := int(sol.InDate.Sub(start).Seconds())
		hidden := withinLockDelay && sol.InDate.After(lock)

		if mode == rankOI {
			// contestrank5.php:233 的阈值与 contestrank-oi.php 不同：
			//   部署版 contestrank5.php：非 AC 且 pass_rate >= 0.99 直接归零
			//   部署版 contestrank-oi.php：非 AC 且 pass_rate >= 0.95 封顶 0.95
			// contestrank5 也不像 oi 那样把 AC 强制成 1.0，而是直接用库里的
			// pass_rate。线上 cid=1000 的 AC 行 pass_rate 全为 1.000，所以两条
			// 路径在现有数据上等价，这里仍按部署版原样实现。
			passRate := sol.PassRate
			if sol.Result != 4 && passRate >= 0.99 {
				passRate = 0
			}
			if hidden {
				t.AddOI(sol.ProblemID, sec, 0, 0)
			} else {
				t.AddOI(sol.ProblemID, sec, passRate, sol.Result)
			}
			continue
		}
		if hidden {
			t.AddACM(sol.ProblemID, sec, 0, cePenalty)
		} else {
			t.AddACM(sol.ProblemID, sec, sol.Result, cePenalty)
		}
	}

	sortTeams(teams, mode)

	fb, err := s.loadReviewFirstBlood(r.Context(), problemIDs, start, s.cfg.RankHidden)
	if err != nil {
		s.log.Error("查询补题榜首杀失败", "err", err)
		http.Error(w, "首杀信息暂时不可用", http.StatusInternalServerError)
		return
	}

	view := newReviewRankView(cid, contest.Title.String, mode, problems, teams, fb)
	view.Locked = withinLockDelay &&
		now.After(start.Add(time.Duration(float64(end.Sub(start))*(1-lockPercent))))
	view.ViewUser = strings.TrimSpace(r.URL.Query().Get("user_id"))

	name := "contestRankReviewACM"
	if mode == rankOI {
		name = "contestRankReviewOI"
	}
	s.renderPage(w, r, http.StatusOK, name, contest.Title.String, view)
}

// renderEmptyReviewRank 渲染"比赛没有题目"的补题榜。
func (s *Server) renderEmptyReviewRank(w http.ResponseWriter, title string, cid int32, mode rankMode) {
	name := "contestRankReviewACM"
	if mode == rankOI {
		name = "contestRankReviewOI"
	}
	s.renderPage(w, nil, http.StatusOK, name, title, &contestRankView{
		CID: cid, Title: title, Mode: mode, OI: mode == rankOI,
		Review: true, BackURL: reviewBackURL(mode, cid),
	})
}

// newReviewRankView 组装补题榜视图。
//
// 与 newContestRankView 的区别是格子要经过一次 problem_id → 题号 的翻译：
// 计分时按 PHP 原样以 problem_id 为键，渲染时才知道这一列对应第几题。
func newReviewRankView(cid int32, title string, mode rankMode,
	problems []dbgen.GetContestProblemsForRankRow, teams []*contestTeam,
	fb map[int]string) *contestRankView {

	pidCnt := 0
	for _, p := range problems {
		if int(p.Num)+1 > pidCnt {
			pidCnt = int(p.Num) + 1
		}
	}

	letters := make([]string, pidCnt)
	for i := range letters {
		letters[i] = contestProblemLetter(i)
	}

	view := &contestRankView{
		CID:     cid,
		Title:   title,
		Mode:    mode,
		OI:      mode == rankOI,
		Review:  true,
		BackURL: reviewBackURL(mode, cid),
		Letters: letters,
		// 补题榜的题头链到 problem.php?id=<problem_id>，与 contestrank.php 的
		// problem.php?cid=&pid=<题号> 不同（template/syzoj/contestrank4.php:74）。
		ProblemIDs: make([]int32, pidCnt),
	}

	// PHP 是 $pida[$num] = $problem_id，模板再用 $j=$pida[$k] 反查，
	// 这里按同样的映射把 problem_id 填到题号上。
	for _, p := range problems {
		if p.Num >= 0 && int(p.Num) < pidCnt {
			view.ProblemIDs[p.Num] = p.ProblemID
		}
	}

	rank := 1
	for _, t := range teams {
		row := rankRow{
			UserID: t.UserID,
			Nick:   t.Nick,
			Solved: t.Solved,
			Time:   sec2str(t.Time),
			Total:  formatOITotal(t, mode),
		}
		row.TotalVal = t.Total
		// 与 contestrank.php 同规则：以 * 开头的是非正式选手，不排名次。
		if !strings.HasPrefix(t.Nick, "*") {
			row.Rank = rank
			rank++
		}

		for num := 0; num < pidCnt; num++ {
			pid := int(view.ProblemIDs[num])
			sec, solved := t.acSec(pid)
			wa := t.WANum[pid]
			cell := rankProblemCell{
				Num:    num,
				Letter: letters[num],
				Solved: solved,
				WA:     wa,
			}
			if solved {
				cell.First = fb[pid] == t.UserID
				cell.ACS = sec2str(sec)
				if mode == rankOI {
					cell.Point = formatPassRate(t.PassRate[pid])
					cell.BackStyle = oiAccentStyle(wa)
				} else {
					cell.BackStyle = acmAccentStyle(wa)
				}
			} else if wa > 0 {
				if mode == rankOI {
					cell.Point = formatPassRate(t.PassRate[pid])
					cell.BackStyle = oiFailStyle(wa)
				} else {
					cell.BackStyle = acmFailStyle(wa)
				}
			}
			row.Cells = append(row.Cells, cell)
		}
		view.Rows = append(view.Rows, row)
	}
	return view
}

// reviewBackURL 返回补题榜"返回"按钮指向的正式榜单。
func reviewBackURL(mode rankMode, cid int32) string {
	if mode == rankOI {
		return fmt.Sprintf("contestrank-oi.php?cid=%d", cid)
	}
	return fmt.Sprintf("contestrank.php?cid=%d", cid)
}
