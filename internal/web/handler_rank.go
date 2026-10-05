// 比赛排名页（/contestrank.php 与 /contestrank-oi.php）。
//
// 对应 PHP 的 contestrank.php（ACM 赛制）与 contestrank-oi.php（OI 赛制）。
// 两个 PHP 文件各自内联了一份 TM 类，计分规则并不相同；这里按赛制拆成
// 两个 Add 方法，共用取数与排序骨架。
//
// ── 与 PHP 的两处有意分歧 ────────────────────────────────────────────
//
//  1. 缺席选手。PHP 两个文件都在主循环后写 $U[$user_cnt] = new TM()，而主
//     循环里 $user_cnt 已经自增过了，于是第一个缺席者会覆盖掉最后一名真实
//     选手。这里把缺席者追加到末尾，不覆盖。
//
//  2. OI 的 p_pass_rate 哨兵。PHP 在 AC 分支里先写
//     p_pass_rate[pid] = res*100、紧接着又写 = res，前一句必然被覆盖，属于
//     无效代码。这里直接写 res。
//
// ── 关于排序 ──────────────────────────────────────────────────────────
//
// PHP 的 s_cmp 写法很反直觉：直接 return 比较表达式的 bool。实测（PHP 8.5）
// 它其实是**正确**的 —— 排序把 cmp<=0 当作"a 在前"，于是
// "a 更好"返回 false(0) → a 在前，"a 更差"返回 true(1) → b 在前，
// 恰好是一个合法的比较器；6 种 ACM 排列与 4 种 OI 排列都能得到正确名次。
//
// 这里仍然显式写清三段排序规则而不是照抄 bool，是因为 bool 比较器依赖
// "恰好为 0/1"这个隐含约定，读的人很容易误判成坏的；写明确后行为与 PHP
// 一致且可读。差异仅在完全同分时：PHP 依赖 usort 的稳定性保留 user_id
// 顺序，这里显式以 user_id 兜底，结果相同但不再依赖排序稳定性。
package web

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// penaltyPerWA 是一次错误提交的罚时秒数（20 分钟），对应 PHP
// `$this->p_wa_num[$pid] * 1200`。
const penaltyPerWA = 1200

// rankLockDelaySec 对应 $OJ_RANK_LOCK_DELAY（include/db_info.inc.php:67）：
// 赛后封榜仍持续的时间，秒。期内封榜时点之后的提交一律按未通过处理。
const rankLockDelaySec = 3600

// contestRankLockBits 是 contest_locked($cid, 20) 里的 20，等于 1<<2 | 1<<4，
// 对应 $contest_locks 的"禁止排名列表"与"禁止查看比赛状态"两位
// （include/const.inc.php:18）。
const contestRankLockBits = 20

// rankACM / rankOI 区分两种赛制。
type rankMode int

const (
	rankACM rankMode = iota
	rankOI
)

// contestTeam 复刻 PHP 的 TM 类，一个选手在单场比赛中的累计成绩。
type contestTeam struct {
	UserID string
	Nick   string

	// Solved 是已通过题目数，Time 是累计罚时秒数（OI 也显示这个值）。
	Solved int
	Time   int

	// Total 是 OI 赛制的总分，ACM 赛制恒为 0。
	Total float64

	// 以下按题目序号（solution.num）索引，不是 problem_id。
	WANum    map[int]int
	ACSec    map[int]int
	PassRate map[int]float64
}

func newContestTeam(userID, nick string) *contestTeam {
	return &contestTeam{
		UserID:   userID,
		Nick:     nick,
		WANum:    make(map[int]int),
		ACSec:    make(map[int]int),
		PassRate: make(map[int]float64),
	}
}

// acSec 返回该题的通过时刻与是否已通过。
//
// 判据用 sec > 0，与两个 PHP 模板一致：`isset($U[$i]->p_ac_sec[$j]) &&
// $U[$i]->p_ac_sec[$j] > 0`。通过时刻为 0（开赛瞬间 AC）不算已通过。
func (t *contestTeam) acSec(num int) (int, bool) {
	sec, ok := t.ACSec[num]
	return sec, ok && sec > 0
}

// AddACM 复刻 contestrank.php 的 TM::Add($pid, $sec, $res)。
//
// sec < 0 的提交直接丢弃：续赛比赛会把旧提交的时间算成负值。
//
// cePenalty 对应 $OJ_CE_PENALTY（线上为 false）：关掉时 CE（result=11）
// 直接返回，既不累加错误次数也不产生罚时，见 config.CEPenalty 的说明。
func (t *contestTeam) AddACM(num, sec int, res int32, cePenalty bool) {
	if sec < 0 {
		return
	}
	if _, solved := t.acSec(num); solved {
		return
	}
	if res != 4 {
		if !cePenalty && res == 11 {
			return
		}
		t.WANum[num]++
		return
	}
	t.ACSec[num] = sec
	t.Solved++
	t.Time += sec + t.WANum[num]*penaltyPerWA
}

// AddOI 复刻 contestrank-oi.php 的 TM::Add($pid, $sec, $res, $result)。
//
// OI 的分数是每题最优 pass_rate 之和（×100）：通过记 1.0 即 100 分，未通过
// 取历次提交里最大的 pass_rate（调用方已封顶 0.95，即 95 分）。与 ACM 不同，
// OI 在通过前会持续更新 pass_rate 并累计错误次数。
func (t *contestTeam) AddOI(num, sec int, res float64, result int32) {
	if _, solved := t.acSec(num); solved {
		return
	}
	if result != 4 {
		if cur, ok := t.PassRate[num]; ok {
			// PHP 只在 res 变大时替换，并同步调整总分。
			if res > cur {
				t.Total -= cur * 100
				t.Total += res * 100
				t.PassRate[num] = res
			}
		} else {
			t.PassRate[num] = res
			t.Total += res * 100
		}
		t.WANum[num]++
		return
	}
	t.ACSec[num] = sec
	t.Solved++
	if _, ok := t.WANum[num]; !ok {
		t.WANum[num] = 0
	}
	if cur, ok := t.PassRate[num]; ok {
		t.Total -= cur * 100
	}
	t.Total += res * 100
	t.PassRate[num] = res
	t.Time += sec + t.WANum[num]*penaltyPerWA
}

// sortTeams 按赛制排序。末位以 user_id 兜底，保证同样的数据永远得到同样
// 的名次（不依赖跨实现的排序稳定性）。
func sortTeams(teams []*contestTeam, mode rankMode) {
	sort.SliceStable(teams, func(i, j int) bool {
		a, b := teams[i], teams[j]
		if mode == rankOI && a.Total != b.Total {
			return a.Total > b.Total
		}
		if a.Solved != b.Solved {
			return a.Solved > b.Solved
		}
		if a.Time != b.Time {
			return a.Time < b.Time
		}
		return a.UserID < b.UserID
	})
}

// contestProblemLetter 把 contest_problem.num 转成题号字母，复刻 PHP 的
// $PID 数组：0→A，25→Z，26→AA（Excel 列名式的两位编号）。
func contestProblemLetter(num int) string {
	if num < 0 {
		return ""
	}
	name := ""
	for num >= 0 {
		name = string(rune('A'+num%26)) + name
		num = num/26 - 1
	}
	return name
}

// sec2str 复刻 PHP 的 sec2str()（include/my_func.inc.php:605）。
//
// PHP 是 sprintf("%02d:%02d:%02d", $sec/3600, $sec%3600/60, $sec%60)：三个
// 参数都是表达式而非字面整除，先算出浮点再各自截断为整数，负数按"向零截断"
// 处理且 %02d 会把负号算进宽度（-3600 → "-1:00:00"，-61 → "00:-1:-1"）。
// 续赛比赛会把开赛前的提交算成负时间，所以不能把负值夹到 0。
func sec2str(sec int) string {
	hours := int(float64(sec) / 3600)
	minutes := int(float64(sec%3600) / 60)
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, sec%60)
}

// rankSolution 是取数用的中间结构，对应 contest_solutions.php 的查询列。
type rankSolution struct {
	UserID   string
	Nick     string
	Result   int32
	Num      int
	InDate   time.Time
	PassRate float64
}

// absentUser 是报名但没提交的选手。
type absentUser struct {
	UserID string
	Nick   string
}

// loadRankSolutions 复刻 include/contest_solutions.php 的查询。
//
// `num>=0 AND problem_id>0` 排除未分配题号的提交；时间窗口是闭开区间
// [start_time, end_time)；`ORDER BY user_id, solution_id` 是关键 —— Add 的
// 累加逻辑依赖同一选手同一题按时间先后处理，否则错题计数与通过时刻都会错。
func (s *Server) loadRankSolutions(ctx context.Context, cid int32, start, end time.Time) ([]rankSolution, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, nick, result, num, in_date, pass_rate
		FROM solution
		WHERE contest_id = ? AND num >= 0 AND problem_id > 0
		  AND in_date >= ? AND in_date < ?
		ORDER BY user_id, solution_id
	`, cid, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []rankSolution
	for rows.Next() {
		var r rankSolution
		var passRate sql.NullFloat64
		if err := rows.Scan(&r.UserID, &r.Nick, &r.Result, &r.Num, &r.InDate, &passRate); err != nil {
			return nil, err
		}
		r.PassRate = passRate.Float64
		out = append(out, r)
	}
	return out, rows.Err()
}

// loadFirstBlood 查每题第一个通过的人（两个 PHP 文件里同一段 SQL）。
//
// 按 num 分组取 min(solution_id)，也就是"最早一次 AC"；solution_id 单调递增，
// 所以它等价于最早的 AC 时刻。
func (s *Server) loadFirstBlood(ctx context.Context, cid int32) (map[int]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT s.num, s.user_id
		FROM solution s,
		     (SELECT num, MIN(solution_id) AS min_id
		      FROM solution WHERE contest_id = ? AND result = 4
		      GROUP BY num) c
		WHERE s.solution_id = c.min_id
	`, cid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	fb := make(map[int]string)
	for rows.Next() {
		var num int
		var userID string
		if err := rows.Scan(&num, &userID); err != nil {
			return nil, err
		}
		fb[num] = userID
	}
	return fb, rows.Err()
}

// loadAbsentUsers 查报名了但一次提交都没有的选手。
func (s *Server) loadAbsentUsers(ctx context.Context, cid int32) ([]absentUser, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT u.user_id, u.nick
		FROM users u
		WHERE u.user_id IN (
			SELECT p.user_id FROM privilege p
			WHERE p.rightstr = ?
			  AND p.user_id NOT IN (
			      SELECT DISTINCT s.user_id FROM solution s WHERE s.contest_id = ?)
		)
		ORDER BY u.user_id
	`, fmt.Sprintf("c%d", cid), cid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []absentUser
	for rows.Next() {
		var a absentUser
		if err := rows.Scan(&a.UserID, &a.Nick); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// handleContestRankMode 处理 GET /contestrank.php（ACM）与
// GET /contestrank-oi.php（OI）。
func (s *Server) handleContestRankMode(w http.ResponseWriter, r *http.Request, mode rankMode) {
	cid := int32(parseInt(r.URL.Query().Get("cid")))
	if cid <= 0 {
		http.Error(w, "缺少 cid", http.StatusBadRequest)
		return
	}

	contest, err := s.queries().GetContestForRank(r.Context(), cid)
	if err != nil {
		if err == sql.ErrNoRows {
			renderPage(w, http.StatusOK, "errorPage", "比赛不存在",
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

	// 比赛未开始（contestrank.php:130-134）。
	if now.Before(start) {
		renderPage(w, http.StatusOK, "errorPage", "比赛", map[string]string{
			"Message": "比赛尚未开始，暂不提供排名",
		})
		return
	}

	// NOIP / 封榜禁看排名（contestrank.php:137-146）。
	//
	// contest_locked($cid, 20) 命中"禁止排名列表"或"禁止查看状态"位时同样
	// 拦截；管理员、限时赛持有人、代码审查员、比赛创建者豁免。
	if !statusNOIPExempt(identity, cid) {
		if isNOIPContest(&contest, s.cfg.NoIPKeyword, now) ||
			s.contestRankLocked(r.Context(), cid, now) {
			renderPage(w, http.StatusOK, "errorPage", "比赛", map[string]string{
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

	sols, err := s.loadRankSolutions(r.Context(), cid, start, end)
	if err != nil {
		s.log.Error("查询比赛提交失败", "err", err)
		http.Error(w, "提交记录暂时不可用", http.StatusInternalServerError)
		return
	}

	// 封榜时点：$lock = end - (end-start)*percent。
	//
	// 封榜期内（now < end + delay）且提交时间晚于 lock 的提交，一律按
	// result=0 处理：不计通过、不计分数，但错误次数照算 —— 否则用户可以
	// 通过"某人在榜上却没有 AC"反推对手刚过题。
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
			// OI 的 pass_rate 封顶规则（contestrank-oi.php:184-186）：
			// 未通过最高 0.95，通过记 1.0。
			passRate := sol.PassRate
			if sol.Result == 4 {
				passRate = 1.0
			} else if passRate >= 0.95 {
				passRate = 0.95
			}
			if hidden {
				t.AddOI(sol.Num, sec, 0, 0)
			} else {
				t.AddOI(sol.Num, sec, passRate, sol.Result)
			}
			continue
		}
		if hidden {
			t.AddACM(sol.Num, sec, 0, cePenalty)
		} else {
			t.AddACM(sol.Num, sec, sol.Result, cePenalty)
		}
	}

	// 缺席选手追加在末尾（PHP 在这里有个 off-by-one，会覆盖最后一名）。
	absent, err := s.loadAbsentUsers(r.Context(), cid)
	if err != nil {
		s.log.Error("查询缺席选手失败", "err", err)
		http.Error(w, "参赛名单暂时不可用", http.StatusInternalServerError)
		return
	}
	for _, a := range absent {
		if _, dup := byUser[a.UserID]; dup {
			continue
		}
		teams = append(teams, newContestTeam(a.UserID, a.Nick))
	}

	sortTeams(teams, mode)

	fb, err := s.loadFirstBlood(r.Context(), cid)
	if err != nil {
		s.log.Error("查询首杀失败", "err", err)
		http.Error(w, "首杀信息暂时不可用", http.StatusInternalServerError)
		return
	}

	view := newContestRankView(cid, contest.Title.String, mode, problems, teams, fb, absent)
	view.Locked = withinLockDelay &&
		now.After(start.Add(time.Duration(float64(end.Sub(start))*(1-lockPercent))))
	view.ViewUser = strings.TrimSpace(r.URL.Query().Get("user_id"))

	name := "contestRank"
	if mode == rankOI {
		name = "contestRankOI"
	}
	renderPage(w, http.StatusOK, name, contest.Title.String, view)
}

// contestRankLocked 复刻 contest_locked($cid, 20)（include/my_func.inc.php:207）：
// 比赛中（start<now<end）且 contest_type 命中 20 的禁止位，或标题含 NOIP
// 关键字时，禁看排名。
//
// 豁免（管理员等）在调用方处理，这里只回答"是否命中禁止位"。
func (s *Server) contestRankLocked(ctx context.Context, cid int32, now time.Time) bool {
	var contestID sql.NullInt32
	// PHP 的 $now 是 "Y-m-d H:i"（分钟精度），比较的是 datetime 列。
	nowStr := now.Format("2006-01-02 15:04:05")
	err := s.db.QueryRowContext(ctx, `
		SELECT contest_id FROM contest
		WHERE contest_id = ?
		  AND (contest_type & ? > 0 OR title LIKE ?)
		  AND start_time < ? AND end_time > ?
	`, cid, contestRankLockBits,
		"%"+s.cfg.NoIPKeyword+"%", nowStr, nowStr,
	).Scan(&contestID)
	return err == nil && contestID.Valid && contestID.Int32 != 0
}

// ── 视图模型 ────────────────────────────────────────────────────────────

// rankProblemCell 是排名表里单个题目的格子。
type rankProblemCell struct {
	Num       int
	Letter    string
	Solved    bool   // 已通过
	First     bool   // 该题首杀
	WA        int    // 该题错误提交数
	ACS       string // 通过时刻，形如 00:12:34
	Point     string // OI 的得分
	BackStyle string // 背景色
}

// rankRow 是一行排名。
type rankRow struct {
	Rank     int
	UserID   string
	Nick     string
	Solved   int
	Time     string
	Total    string // OI 总分，ACM 为空
	TotalVal float64
	Cells    []rankProblemCell
}

// contestRankView 是排名模板共用的数据。
//
// Review / BackURL / ProblemIDs 只在补题榜（contestrank4.php、contestrank5.php）
// 里有值，正式榜保持零值，见 handler_contest_review.go。
type contestRankView struct {
	CID        int32
	Title      string
	Mode       rankMode
	OI         bool
	Review     bool // 补题榜：题头链 problem.php?id=，无缺席区块
	Letters    []string
	ProblemIDs []int32 // 补题榜：题号 → problem_id
	BackURL    string  // 补题榜：返回正式榜的链接
	Rows       []rankRow
	Absent     []absentUser
	Locked     bool
	ViewUser   string
}

func newContestRankView(cid int32, title string, mode rankMode,
	problems []dbgen.GetContestProblemsForRankRow, teams []*contestTeam,
	fb map[int]string, absent []absentUser) *contestRankView {

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
		Letters: letters,
		Absent:  absent,
	}

	// PHP 用 $nick[0] != "*" 判断是否为正式选手；以 * 开头的昵称不排名次。
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

		// PHP 用 $nick[0] != "*" 判断是否为正式选手；以 * 开头的昵称不排名次，
		// 单元格显示 *。缺席选手同样走这条规则、照常排名次（PHP 是把缺席名单
		// 追加进 $U 之后统一渲染的），他们只是额外出现在页面底部的缺席区块。
		//
		// 注意这里**不能**用"零解题 + 零错误"反推是否缺席：$OJ_CE_PENALTY=false
		// 时 CE 不计错误次数，只提交过 CE 的真实选手同样满足该条件。线上
		// contest_id=1000 尾部有 20 个这种选手，一度被误判而丢掉名次。
		if !strings.HasPrefix(t.Nick, "*") {
			row.Rank = rank
			rank++
		}

		for num := 0; num < pidCnt; num++ {
			sec, solved := t.acSec(num)
			wa := t.WANum[num]
			cell := rankProblemCell{
				Num:    num,
				Letter: letters[num],
				Solved: solved,
				WA:     wa,
			}
			if solved {
				cell.First = fb[num] == t.UserID
				cell.ACS = sec2str(sec)
				if mode == rankOI {
					cell.Point = formatPassRate(t.PassRate[num])
					cell.BackStyle = oiAccentStyle(wa)
				} else {
					cell.BackStyle = acmAccentStyle(wa)
				}
			} else if wa > 0 {
				if mode == rankOI {
					cell.Point = formatPassRate(t.PassRate[num])
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

// formatOITotal 渲染 OI 总分。PHP 直接 echo 一个 float，尾部的 .0 会显示
// 出来（"300" 而不是 "300.0" 取决于浮点表示），这里统一去掉尾零。
// phpFloat 按 PHP 的浮点→字符串规则输出（php.ini 的 precision=14，等价于
// %.14G）。
//
// OI 排名不能直接用 trimFloat：pass_rate 来自 decimal(4,3)，而 PHP 里
// `total += res*100` 是浮点累加，0.56*100 在 IEEE754 下等于
// 56.00000000000001。PHP 的 precision=14 会把它显示成 "56"，而
// strconv 的最短往返表示会原样吐出 "56.00000000000001"，在榜面上和 PHP
// 逐字符不同。线上 contest_id=1000 有约 60 处这种差异。
//
// 只用于 OI 得分渲染；ACM 全是整数，不受影响。
func phpFloat(f float64) string {
	if f == 0 {
		return "0"
	}
	return strconv.FormatFloat(f, 'G', 14, 64)
}

func formatOITotal(t *contestTeam, mode rankMode) string {
	if mode != rankOI {
		return ""
	}
	return phpFloat(t.Total)
}

// formatPassRate 渲染单题得分：pass_rate × 100，整数则不带小数。
func formatPassRate(rate float64) string {
	return phpFloat(rate * 100)
}

// 四个底色对应两个 PHP 模板里的内联样式，错题越多越深。
func acmAccentStyle(wa int) string { return fmt.Sprintf("rgb(%d, 255, %d)", 150+12*wa, 150+8*wa) }
func acmFailStyle(wa int) string {
	v := 240 - 9*wa
	return fmt.Sprintf("rgb(255, %d, %d)", v, v)
}
func oiAccentStyle(wa int) string {
	third := 150 + 8*wa
	if third > 230 {
		third = 230
	}
	return fmt.Sprintf("rgb(%d, 255, %d)", 150+12*wa, third)
}
func oiFailStyle(wa int) string {
	v := 240 - 9*wa
	if v < 130 {
		v = 130
	}
	return fmt.Sprintf("rgb(255, %d, %d)", v, v)
}
