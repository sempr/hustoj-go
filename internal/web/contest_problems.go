// 比赛题目页的状态标记逻辑（contest.php?cid=X）。
//
// 拆成独立文件是因为这里的规则密度远高于数据查询：
// NOIP 掩码、封榜、按角色豁免、赛后对未开赛比赛的题目隐藏，
// 每一条都对应 contest.php 里的一段条件判断。

package web

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// pidArrayLen 是 const.inc.php:14 中 $PID 数组的长度（PHP 实测 702）。
const pidArrayLen = 702

// pidLetter 把题号下标转成字母，对应 const.inc.php:14 的 $PID。
//
// $PID 是 0 基的"电子表格列名"序列：下标 0 是 A，25 是 Z，26 是 AA……
// 比赛里的题目通常不超过几十个，但 PHP 那个数组有 1828 项（A..ZZZ），
// 这里用同样的进制换算，并按实际长度设上限。
//
// 真实 $PID 是 0 基：A(0) … Z(25) … ZZ(701)。比 3 位所需的 18278 项少得多，
// 所以 3 位分支实际不会走到 —— 保留它只是为了让算法本身自洽。
func pidLetter(idx int32) string {
	// $PID 只有 702 项（下标 0..701，最后一个是 "ZZ"）。
	// 越界时 PHP 读到未定义下标，得到 null 并在模板里渲染成空串。
	// 这里同样返回空串，保持可观察输出一致。
	if idx < 0 || idx > pidArrayLen-1 {
		return ""
	}
	var sb []byte
	for n := int(idx); ; n = n/26 - 1 {
		sb = append([]byte{byte('A' + n%26)}, sb...)
		if n < 26 {
			break
		}
	}
	return string(sb)
}

// contestACMark 复刻 check_ac()（my_func.inc.php:616）的返回值语义。
//
// 返回值是三种状态之一（PHP 返回的是带样式的 label 文本）：
//
//	markAC    有 AC   → "Y"
//	markBad   交过但没 AC → "N"
//	markAsked NOIP 下交过 → "?"
//	markNone  没交过  → ""
//
// 注意 NOIP 分支只看"有没有交过"，不看是否 AC —— 那是防泄题的关键：
// 赛中只能告诉学生"你交过这道题"，不能告诉他对不对。
type acMark int

const (
	markNone acMark = iota
	markAC
	markBad
	markAsked
)

// String 返回题号旁的标记字符，空串表示不显示。
func (m acMark) String() string {
	switch m {
	case markAC:
		return "Y"
	case markBad:
		return "N"
	case markAsked:
		return "?"
	default:
		return ""
	}
}

// contestACMarkOf 由三个计数推出标记，对应 check_ac 的三条 SQL。
func contestACMarkOf(c dbgen.ContestProblemACCounts, noip bool) acMark {
	if noip {
		if c.SubTotal > 0 {
			return markAsked
		}
		return markNone
	}
	if c.ACCount > 0 {
		return markAC
	}
	if c.BadCount > 0 {
		return markBad
	}
	return markNone
}

// contestProblemFlags 是一次渲染所需的全部开关。
type contestProblemFlags struct {
	// NOIP 为真时题目状态只显示"交过/没交过"，AC/提交数打码。
	NOIP bool
	// HideOthers 对应 contest_locked($cid, 8)：
	// 屏蔽其他人的提交，AC/提交数打码。
	HideOthers bool
	// IsManager 是管理员或本场限时赛标记（m<cid>）的持有者，
	// 两者都能看到 AC/提交数，且不受赛后隐藏标题影响。
	IsManager bool
	// ContestOK 对应 $contest_ok：赛后是否仍可按 cid 打开题目。
	ContestOK bool
	// Running 为真表示比赛尚未结束。
	Running bool
	// LoggedIn 为真才会查询该用户的 AC 标记。
	LoggedIn bool
}

// maskAccepted 判断 AC/提交数是否要打码，对应 contest.php:126。
//
// 条件是 `($noip || $hide_others) && !(m$cid || administrator)` ——
// 即"需要打码"与"有权看"取反。这两半必须分别对应，
// 因为 isManager 只聚合了 m 标记与管理员，不含 source_browser：
// PHP 里的 source_browser 豁免只作用于 $noip（:67-71），
// 不作用于这一处的打码判断。把两者混在一起会让源码浏览器
// 看到本不该看的提交数。
func (f contestProblemFlags) maskAccepted() bool {
	return (f.NOIP || f.HideOthers) && !f.IsManager
}

// fillContestProblems 补全每个题目的运行时字段。
//
// 对应 contest.php:77-135 的循环体。
func (s *Server) fillContestProblems(ctx context.Context, q dbgen.Querier,
	probs []ContestProblemItem, cid int32, id Identity, f contestProblemFlags) error {

	// isManager 决定两件事：能否看提交数、赛后能否看到被隐藏的标题。
	isManager := id.Admin || (id.ContestTimeLimitUsed != nil && id.ContestTimeLimitUsed[cid])

	for i := range probs {
		p := &probs[i]
		p.IsNOIP = f.NOIP
		p.HideOthers = f.HideOthers
		p.IsRunning = f.Running

		// ── 题号键 ──
		//
		// PHP 用 $cnt（结果集行下标，contest.php:55 起自增）而不是 cp.num：
		// 既当下标算 $PID[$cnt]，又拼进 pid=$cnt，还当 check_ac 的 num。
		// 正常比赛 num 恰是 0..N-1 的稠密序列，两者重合；
		// num 有空洞时 PHP 会查错题。这里照搬行下标，
		// 好让链接与 PHP 逐字节一致 —— 已有书签和外链都指向那个地址。
		key := int32(i)
		p.ProblemKey = key
		p.Letter = pidLetter(key)

		byContest := "problem.php?cid=" + itoa32(cid) + "&pid=" + itoa32(key)
		byProblem := "problem.php?id=" + itoa32(p.ProblemID)

		// ── 链接与标题 ──
		//
		// contest.php:92-117 有三种情形，且题号列与标题列的地址并不相同：
		//   进行中          两列都链到 problem.php?cid=&pid=
		//   赛后·题目被占用  两列都是纯文本，只留字母与占位说明
		//   赛后·可见       题号列链 problem.php?id=，标题列链 problem.php?cid=&pid=
		//                     （$contest_ok 为假时标题列退回纯文本）
		switch {
		case f.Running:
			p.LetterLink = byContest
			p.TitleLink = byContest

		default:
			// 比赛已结束：该题若还排在某场**尚未开始**的比赛里，
			// 除本场限时赛管理员外一律藏起标题（contest.php:99-110）。
			used, err := q.ProblemUsedInUpcomingContest(ctx, p.ProblemID)
			if err != nil {
				return err
			}
			if used && !isManager {
				p.TitleHidden = true
				p.TitleNote = "--using in another private contest--"
				break
			}
			p.LetterLink = byProblem
			if f.ContestOK {
				p.TitleLink = byContest
			}
		}

		// ── AC / 提交数 ──
		// 直接赋值而不是"只在需要时置 true"：同一批 ContestProblemItem
		// 复用（例如换一组 flags 重跑）时打码状态必须能复位，
		// 否则上一次留下的 Masked=true 会一直粘着。
		p.Masked = f.maskAccepted()
		if !p.Masked {
			p.Accepted = p.CAccepted
			p.SubmitCount = p.CSubmit
		}

		// ── 当前用户的题目标记 ──
		if !f.LoggedIn {
			continue
		}
		counts, err := q.CountUserContestProblemAC(ctx, id.UserID, cid, key)
		if err != nil {
			return err
		}
		p.Mark = contestACMarkOf(counts, f.NOIP).String()
	}
	return nil
}

// contestNoIPFlags 汇总 contest.php:61-71 的 NOIP 与封榜判定。
//
// 对应两处：
//   - $noip = 比赛进行中 && (标题含 NOIP 关键字 || contest_locked($cid,16))
//   - $hide_others = contest_locked($cid, 8)
//
// 随后是一段豁免：管理员、本场 m<cid>、source_browser、contest_creator
// 都把 $noip 强制置为 false。这段豁免**只作用于 $noip**，
// 不影响 $hide_others，也不影响 maskAccepted。
func (s *Server) contestNoIPFlags(ctx context.Context, q dbgen.Querier,
	contest dbgen.GetContestByIDRow, cid int32, id Identity, now int64) (contestProblemFlags, error) {

	f := contestProblemFlags{
		Running:   now < contest.EndTime.Time.Unix(),
		IsManager: id.Admin || (id.ContestTimeLimitUsed != nil && id.ContestTimeLimitUsed[cid]),
	}

	// contest_locked 的时间比较精确到**分钟**（date('Y-m-d H:i')），
	// 不是秒。这里跟着用 Truncate(time.Minute)，否则在同一分钟内
	// 会出现"PHP 说未开赛、Go 说进行中"的边缘差异。
	nowMin := time.Now().Truncate(time.Minute)
	// keyword 恒为 %NOIP_KEYWORD%：contest_locked 的 SQL 把
	// "标题含 NOIP 关键字" OR 在 contest_type 位判断之外，
	// 对 level=8/16 一视同仁。所以标题含 NOIP 的比赛
	// 会顺带让 hide_others 也为真 —— 这是 PHP 的既有行为，照搬。
	kw := "%" + s.cfg.NoIPKeyword + "%"

	locked16, err := q.IsContestLocked(ctx, dbgen.IsContestLockedParams{
		ContestID: cid,
		Level:     sql.NullInt16{Int16: 16, Valid: true},
		Keyword:   sql.NullString{String: kw, Valid: true},
		Now:       sql.NullTime{Time: nowMin, Valid: true},
	})
	if err != nil {
		return f, err
	}
	f.HideOthers, err = q.IsContestLocked(ctx, dbgen.IsContestLockedParams{
		ContestID: cid,
		Level:     sql.NullInt16{Int16: 8, Valid: true},
		Keyword:   sql.NullString{String: kw, Valid: true},
		Now:       sql.NullTime{Time: nowMin, Valid: true},
	})
	if err != nil {
		return f, err
	}

	if !f.Running {
		// 比赛已结束，锁榜与 NOIP 都不再影响可见性。
		return f, nil
	}

	// contest.php:61 用 stripos 判标题，再 || contest_locked($cid,16)。
	// locked16 里已经包含"标题含 NOIP"这一支，这里补的是
	// 「时间() < end_time 且标题命中」而比赛尚未开始的情况 ——
	// 那种情况下 locked16 因内部时间条件为假，
	// 但 PHP 的独立 stripos 判断仍会把它置为 NOIP。
	keyword := s.cfg.NoIPKeyword
	titleNoIP := keyword != "" &&
		strings.Contains(strings.ToLower(contest.Title.String), strings.ToLower(keyword))
	f.NOIP = titleNoIP || locked16

	// 豁免：这些角色不受 NOIP 限制。
	if f.NOIP && (id.Admin || f.IsManager || id.SourceBrowser || id.ContestCreator) {
		f.NOIP = false
	}
	return f, nil
}

// contestTitle 渲染比赛标题，对应 contest.php:242-243。
//
// 空标题回落到"比赛<id>"，与列表页同一套规则。
func contestTitle(title string, cid int32) string {
	if t := strings.TrimSpace(title); t != "" {
		return t
	}
	return "比赛" + itoa32(cid)
}

// coinStars 返回题目的金币星号个数。
//
// 对应 contest.php:95：
//
//	`intval($row['coin']) > 0 ? str_repeat('<span ...>✦</span>', min(intval($row['coin']), 5)) : ''`
//
// 关键在 min(...,5) —— 题目再贵也只画 5 颗星。
// 模板里用 upto5 手工展开同样数量的星号，这里负责算好个数。
// 注意调用点必须自己判 IsRunning —— PHP 只在 contest.php:95 的
// "比赛进行中"分支追加星星，结束后走 contest.php:105-117，
// 那条路径上没有任何 coin 输出。
func coinStars(coin uint32) string {
	switch {
	case coin == 0:
		return ""
	case coin > 5:
		return strings.Repeat("✦", 5)
	default:
		return strings.Repeat("✦", int(coin))
	}
}
