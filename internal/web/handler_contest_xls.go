// contestrank.xls.php 迁移。
//
// 与页面内其他榜单的差异（不要照抄 contestrank.php）：
//  - 分数是 mark 而非题目分：Add() 里按 mark_base/mark_per_problem/mark_per_punish
//    累加，但这个值随后会被 getMark() 的正态分布**整体覆盖**，最终 mark 只由
//    排序后的名次决定。Add() 里的 mark 运算属于死代码，这里保留移植是为了
//    语义完整，实际输出不受影响。
//  - 查询不带时间窗口：contestrank.php 会加 [start_time, end_time)，
//    这里只有 contest_id + num>=0 + problem_id>0。
//  - Add() 没有 `if ($sec < 0) return;`，续赛的负时间提交照样计入。
//  - 单题比赛把 mark_base 从 60 改成 100。
//  - 缺考选手挂在分数表末尾，且只有"报名但本场零提交"才算缺考。

package web

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// mark 评分的固定区间，对应 $mark_start/$mark_end/$mark_sigma。
const (
	xlsMarkBase  = 60.0
	xlsMarkStart = 60
	xlsMarkEnd   = 100
	xlsMarkSigma = 5
)

// xlsTeam 复刻 TM 类。
type xlsTeam struct {
	UserID string
	Nick   string
	Solved int
	Time   int
	WANum  map[int]int
	ACSec  map[int]int
	Mark   float64
}

func newXlsTeam(userID, nick string) *xlsTeam {
	return &xlsTeam{
		UserID: userID,
		Nick:   nick,
		WANum:  make(map[int]int),
		ACSec:  make(map[int]int),
	}
}

// xlsAdd 复刻 TM::Add($pid, $sec, $res, $mark_base, $mark_per_problem,
// $mark_per_punish)。
//
// 与 contestrank.php 的 Add 不同：这里没有 sec<0 的续赛保护。
// CE 的处理相同：$OJ_CE_PENALTY 关闭时 CE（result=11）直接忽略。
func (t *xlsTeam) xlsAdd(num, sec int, res int32, cePenalty bool, markBase, markPerProblem, markPerPunish float64) {
	if _, ok := t.ACSec[num]; ok {
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
	if t.Mark == 0 {
		t.Mark = markBase
	} else {
		t.Mark += markPerProblem
	}
	// PHP 先把空串（null）归零，再 intval 截断。截断必须保留，否则当
	// mark_per_punish 是小数时惩罚分会在边界上差 1。
	punish := int(float64(t.WANum[num]) * markPerPunish)
	punishCap := int(markPerProblem * 0.8)
	if punish < punishCap {
		t.Mark -= float64(punish)
	} else {
		t.Mark -= float64(punishCap)
	}
}

// acSec 输出用。PHP 判据是 `isset(...) && $U[$i]->p_ac_sec[$j] > 0`，
// 开赛瞬间（sec=0）的 AC 不显示时间。
func (t *xlsTeam) acSec(num int) (int, bool) {
	sec, ok := t.ACSec[num]
	return sec, ok && sec > 0
}

func (t *xlsTeam) waNum(num int) int {
	return t.WANum[num]
}

// xlsNormalDistribution 复刻 normalDistribution()。
func xlsNormalDistribution(x, u, s float64) float64 {
	return 1 / (s * math.Sqrt(2*math.Pi)) * math.Pow(math.E, -math.Pow(x-u, 2)/(2*s*s))
}

// xlsGetMark 复刻 getMark($users, $start, $end, $s)。
//
// 从 end 往 start 累加正态分布概率，每累积超过半个刻度就给下一个名次
// 记上当前分值 —— 所以高分集中给排名靠前的人。这是纯按名次分配，与选手
// 解题数无关。
func xlsGetMark(users []*xlsTeam, start, end, s float64) {
	cn := len(users)
	if cn == 0 {
		// PHP 会在这里除零（PHP 8 抛 DivisionByZeroError），Go 直接跳过。
		return
	}
	u := (start+end)/2 + 10
	sigma := (end - start) / s
	prob := func(v float64) float64 { return float64(cn) * xlsNormalDistribution(v, u, sigma) }

	accum := 0.0
	for i := end; i > start; i-- {
		accum += prob(i)
	}
	p := accum / float64(cn)

	accum = 0
	ret := 0
	for i := end; i > start; i-- {
		accum += prob(i)
		for accum > p/2 {
			if ret < cn {
				users[ret].Mark = i
			}
			accum -= p
			ret++
		}
	}
	// 名次分配不完时，后续名次沿用前一个的分数。
	for ret < cn {
		users[ret].Mark = users[ret-1].Mark
		ret++
	}
}

// sortXlsTeams 复刻 usort($U, "s_cmp")。
//
// s_cmp 写的是 `$A->solved < $B->solved`，但 usort 把回调返回的 true 当作
// "排在后面"，所以实际结果是 solved 降序、time 升序 —— 正好是期望的排名。
// 直接照抄 PHP 的 `<` 会把整张表倒过来。
//
// PHP 8 的 usort 是稳定排序，cid=1000 有两千多个并列 0 解选手，一旦排序
// 不稳定整个导出顺序就与 PHP 不同，所以这里必须用 SliceStable。
func sortXlsTeams(teams []*xlsTeam) {
	sort.SliceStable(teams, func(i, j int) bool {
		a, b := teams[i], teams[j]
		if a.Solved != b.Solved {
			return a.Solved > b.Solved
		}
		return a.Time < b.Time
	})
}

// loadXlsSolutions 复刻 contestrank.xls.php 的取数 SQL。
//
// 与 loadRankSolutions 的区别是没有 [start_time, end_time) 窗口，也没有
// pass_rate。
func (s *Server) loadXlsSolutions(ctx context.Context, cid int32) ([]rankSolution, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT user_id, nick, result, num, in_date
		FROM solution
		WHERE contest_id = ? AND num >= 0 AND problem_id > 0
		ORDER BY user_id, solution_id
	`, cid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []rankSolution
	for rows.Next() {
		var r rankSolution
		if err := rows.Scan(&r.UserID, &r.Nick, &r.Result, &r.Num, &r.InDate); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// xlsFilename 复刻 $ftitle 的拼装：标题截 32 字节 → 拼 "C<cid>-" → 去空格
// → 去掉 . \ | : * ? " < > → 追加 .xls → rawurlencode。
func xlsFilename(cid int32, title string) string {
	if utf8.RuneCountInString(title) > 32 {
		title = string([]rune(title)[:32])
	}
	raw := "C" + fmt.Sprint(cid) + "-" + title
	raw = strings.ReplaceAll(raw, " ", "")
	var b strings.Builder
	for _, r := range raw {
		switch r {
		case '.', '\\', '|', ':', '*', '?', '"', '<', '>':
		default:
			b.WriteRune(r)
		}
	}
	return phpRawURLEncode(b.String() + ".xls")
}

// phpRawURLEncode 对应 PHP rawurlencode()：按 RFC 3986 百分号编码，
// 除 A-Za-z0-9-_.~ 之外全部转义（含空格和中文），不能直接用
// url.QueryEscape（它把空格编码成 +，且放行 ~）。
func phpRawURLEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}

// phpExcelInjectionRune 复现 PHP 的 preg_match('/^[=+-@]/', $nick)。
//
// 字符类里的 `-` 位于 `+` 和 `@` 之间，构成 0x2B..0x40 的闭区间，所以命中的
// 是 `=` 以及 `+ , - . / 0-9 : ; < = > ? @`；只有 `! " # $ % & ' ( ) * A-W Y Z`
// 之类才不命中。作者本意只是 Excel 的 `= + - @`。
func phpExcelInjectionRune(r rune) bool {
	return r == '=' || (r >= '+' && r <= '@')
}

// phpHTMLSpecialChars 对应 PHP 8 的 htmlspecialchars() 默认行为
// （ENT_QUOTES | ENT_SUBSTITUTE | ENT_HTML401），转义 & < > " '。
func phpHTMLSpecialChars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&#039;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// handleContestRankXLS 处理 GET /contestrank.xls.php。
func (s *Server) handleContestRankXLS(w http.ResponseWriter, r *http.Request) {
	cid := int32(parseInt(r.URL.Query().Get("cid")))
	if cid <= 0 {
		// 对应 die("No Such Contest!")，返回 200 以兼容旧链接。
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("No Such Contest!"))
		return
	}

	contest, err := s.queries().GetContestForRank(r.Context(), cid)
	if err != nil {
		if err == sql.ErrNoRows {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("No Such Contest"))
			return
		}
		s.log.Error("查询比赛失败", "err", err)
		http.Error(w, "比赛信息暂时不可用", http.StatusInternalServerError)
		return
	}

	title := contest.Title.String
	// 附件头在"比赛未开始"检查之前就设好（contestrank.xls.php:176-178），
	// 所以未开始时 PHP 也是带着 Content-Disposition 输出一行纯文本。
	w.Header().Set("Content-Type", "application/excel")
	w.Header().Set("Content-Disposition", "attachment; filename*=utf-8''"+xlsFilename(cid, title))

	now := time.Now()
	start := contest.StartTime.Time
	end := contest.EndTime.Time
	if now.Before(start) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("Contest Not Started!"))
		return
	}

	identity := s.resolveIdentity(w, r)
	if !statusNOIPExempt(identity, cid) {
		if isNOIPContest(&contest, s.cfg.NoIPKeyword, now) || s.contestRankLocked(r.Context(), cid, now) {
			// PHP 走 template/error.php 输出整页；Go 侧沿用站内 errorPage
			// （与 contestrank.php / conteststatistics.go 一致），页面外框
			// 不同但提示语义相同。
			s.renderPage(w, r, http.StatusOK, "errorPage", "比赛", map[string]string{
				"Message": "<h2>" + s.noipWarning() + "</h2>",
			})
			return
		}
	}

	var pidCnt int
	if err := s.db.QueryRowContext(r.Context(),
		"SELECT COUNT(1) FROM contest_problem WHERE contest_id = ?", cid,
	).Scan(&pidCnt); err != nil {
		s.log.Error("查询比赛题目数失败", "err", err)
		http.Error(w, "比赛题目暂时不可用", http.StatusInternalServerError)
		return
	}

	// 单题比赛直接给满分，mark_per_problem 归零；无题比赛 PHP 会算出负数
	// (-40)，但没有提交能命中，同样保留。
	markBase, markPerProblem := xlsMarkBase, (xlsMarkEnd-xlsMarkBase)/float64(pidCnt-1)
	if pidCnt == 1 {
		markBase = xlsMarkEnd
		markPerProblem = 0
	}
	markPerPunish := markPerProblem / 5

	lockPercent := s.cfg.RankLockPercent
	lock := end.Add(-time.Duration(float64(end.Sub(start)) * lockPercent))
	// 封榜豁免只看 administrator（contestrank.xls.php:244），比 noip 豁免窄。
	hideLocked := now.Before(end.Add(rankLockDelaySec*time.Second)) && !identity.Admin

	sols, err := s.loadXlsSolutions(r.Context(), cid)
	if err != nil {
		s.log.Error("查询比赛提交失败", "err", err)
		http.Error(w, "提交记录暂时不可用", http.StatusInternalServerError)
		return
	}

	teams := make([]*xlsTeam, 0, 16)
	byUser := make(map[string]*xlsTeam)
	cePenalty := s.cfg.CEPenalty
	for _, sol := range sols {
		t, ok := byUser[sol.UserID]
		if !ok {
			t = newXlsTeam(sol.UserID, sol.Nick)
			byUser[sol.UserID] = t
			teams = append(teams, t)
		}
		res := sol.Result
		if hideLocked && sol.InDate.After(lock) {
			res = 0
		}
		t.xlsAdd(sol.Num, int(sol.InDate.Sub(start).Seconds()), res, cePenalty,
			markBase, markPerProblem, markPerPunish)
	}

	sortXlsTeams(teams)

	// 缺考选手排在所有有提交的人之后，顺序由 users 主键扫描决定。
	absent, err := s.loadAbsentUsers(r.Context(), cid)
	if err != nil {
		s.log.Error("查询缺考选手失败", "err", err)
		http.Error(w, "参赛名单暂时不可用", http.StatusInternalServerError)
		return
	}
	for _, a := range absent {
		if _, dup := byUser[a.UserID]; dup {
			continue
		}
		teams = append(teams, newXlsTeam(a.UserID, a.Nick))
	}

	xlsGetMark(teams, xlsMarkStart, xlsMarkEnd, xlsMarkSigma)

	w.WriteHeader(http.StatusOK)

	var b strings.Builder
	b.WriteString("<center><h3>Contest RankList -- " + title + "</h3></center>")
	b.WriteString("<table border=1><tr><td>Rank<td>User<td>Nick<td>Solved<td>Mark<td>Penalty")
	for i := 0; i < pidCnt; i++ {
		b.WriteString("<td>" + contestProblemLetter(i))
	}
	b.WriteString("</tr>")

	rank := 1
	for i, t := range teams {
		if i&1 == 1 {
			b.WriteString("<tr class=oddrow align=center>")
		} else {
			b.WriteString("<tr class=evenrow align=center>")
		}
		// 昵称以 * 开头的选手（管理员置顶）不占名次。
		if strings.HasPrefix(t.Nick, "*") {
			b.WriteString("<td>*")
		} else {
			b.WriteString("<td>" + fmt.Sprint(rank))
			rank++
		}
		b.WriteString("<td>" + t.UserID)

		nick := t.Nick
		// 防 Excel 公式注入。注意 PHP 写的字符类是 /^[=+-@]/，中间的 `-`
		// 没转义，被 PCRE 当成 `+`(0x2B) 到 `@`(0x40) 的区间，于是
		// , - . / 0-9 : ; < > ? 也都会命中 —— 不只是本意的 = + - @。
		// 为了导出结果与线上逐字节一致，这里按实际行为（而不是本意）实现。
		if nick != "" && phpExcelInjectionRune(rune(nick[0])) {
			nick = "'" + nick
		}
		b.WriteString("<td>" + phpHTMLSpecialChars(nick) + "</td>")
		b.WriteString("<td>" + fmt.Sprint(t.Solved) + "</td>")

		mark := 0
		if t.Solved > 0 && t.Mark > 0 {
			mark = int(t.Mark)
		}
		b.WriteString("<td>" + fmt.Sprint(mark) + "</td>")
		b.WriteString("<td>" + sec2str(t.Time) + "</td>")

		for j := 0; j < pidCnt; j++ {
			b.WriteString("<td>")
			if sec, ok := t.acSec(j); ok {
				b.WriteString(sec2str(sec))
			}
			if wa := t.waNum(j); wa > 0 {
				b.WriteString("(-" + fmt.Sprint(wa) + ")")
			}
			b.WriteString("</td>")
		}
		b.WriteString("</tr>")
	}
	b.WriteString("</table>")

	_, _ = w.Write([]byte(b.String()))
}
