// 比赛页面：contest.php（题目列表）、contestrank.php（排名）、conteststatistics.php（统计）。
//
// 这些页面在切流前由 PHP 渲染，迁移到 Go 需要复刻：
//   - contest.php?cid=X：比赛题目列表（含 AC/提交状态、NOIP 锁榜）
//   - contestrank.php?cid=X：ACM/ICPC 排名表
//   - conteststatistics.php?cid=X：比赛统计
//   - contest.php 无参数：比赛列表页

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

// ── 比赛列表查询（contest.php 无 cid）───────────────────────────────────

// ContestListItem 单条比赛列表项。
type ContestListItem struct {
	CID         int32
	Title       string
	StartTime   time.Time
	EndTime     time.Time
	Defunct     string
	Private     int8
	ContestType int16
	Description string
}

// ListContests 返回比赛列表（对应 contest.php 无 cid 分支）。
func ListContests(ctx context.Context, q dbgen.FullQuerier, now time.Time) ([]ContestListItem, error) {
	// PHP: SELECT * FROM contest WHERE defunct='N' ORDER BY contest_id DESC
	rows, err := q.ListContests(ctx)
	if err != nil {
		return nil, err
	}
	var items []ContestListItem
	for _, r := range rows {
		if r.Defunct != "N" {
			continue // 只显示未关闭的比赛
		}
		items = append(items, ContestListItem{
			CID:         r.ContestID,
			Title:       r.Title.String,
			StartTime:   r.StartTime.Time,
			EndTime:     r.EndTime.Time,
			Defunct:     r.Defunct,
			Private:     r.Private,
			ContestType: r.ContestType.Int16,
			Description: r.Description.String,
		})
	}
	return items, nil
}

// ── 比赛题目列表（contest.php?cid=X）────────────────────────────────────

// ContestProblemItem 比赛里的单道题。
type ContestProblemItem struct {
	Num       int32
	ProblemID int32
	Title     string
	Source    string
	Coin      uint32
	// CAccepted/CSubmit 是本场累计通过数与提交数，由 DB 在插入提交时维护。
	// PHP contest.php:66 直接取 contest_problem 的这两列。
	CAccepted int32
	CSubmit   int32
	// 运行时填充，对应 contest.php:77-135 的循环体。
	Letter      string // 题号字母（$PID[$cnt]）
	ProblemKey  int32  // 实际使用的题号键（行下标，非 cp.num）
	LetterLink  string // 题号列的链接，空串表示纯文本
	TitleLink   string // 标题列的链接，空串表示纯文本
	Mark        string // 当前用户的题目标记："Y" / "N" / "?" / ""
	Accepted    int32  // 通过人数（未打码时）
	SubmitCount int32  // 提交次数（未打码时）
	Masked      bool   // AC/提交数是否被 NOIP 或封榜打码
	TitleHidden bool   // 赛后题目被未开赛比赛占用，标题是否藏起
	TitleNote   string // 藏起时显示的说明文字
	IsNOIP      bool
	HideOthers  bool
	IsRunning   bool
}

// GetContestProblems 取比赛的题目列表（对应 contest.php:59-63）。
func GetContestProblems(ctx context.Context, q dbgen.FullQuerier, cid sql.NullInt32) ([]ContestProblemItem, error) {
	rows, err := q.GetContestProblems(ctx, cid)
	if err != nil {
		return nil, err
	}
	var items []ContestProblemItem
	for _, r := range rows {
		items = append(items, ContestProblemItem{
			Num:       r.Num,
			ProblemID: r.ProblemID,
			Title:     r.Title,
			Source:    r.Source.String,
			Coin:      r.Coin,
			CAccepted: r.CAccepted,
			CSubmit:   r.CSubmit,
		})
	}
	return items, nil
}

// ── 排名查询（contestrank.php / contestrank-oi.php）──────────────────────

// RankRow 单行排名数据。
type RankRow struct {
	Rank     int
	UserID   string
	Nick     string
	Solved   int
	Penalty  int
	Problems []RankProblem // 每道题的提交情况
}

// RankProblem 单题的排名表现。
type RankProblem struct {
	Num      int32
	Solved   bool
	Attempts int
	Time     int // 通过时间(分钟)
}

// GetContestRankACM ACM 模式排名（contestrank.php）。
func GetContestRankACM(ctx context.Context, q dbgen.FullQuerier, cid int32) ([]RankRow, error) {
	// 复杂查询：按用户聚合 solution，计算 solved/penalty
	// 简化版：先返回空，后续补全
	return nil, nil
}

// GetContestRankOI OI 模式排名（contestrank-oi.php）。
func GetContestRankOI(ctx context.Context, q dbgen.FullQuerier, cid int32) ([]RankRow, error) {
	return nil, nil
}

// ── 统计查询（conteststatistics.php）───────────────────────────────────

// StatRow 统计单行。
type StatRow struct {
	Num       int32
	Title     string
	TotalSub  int
	ACCount   int
	ACUsers   int
	FirstAC   string
	FirstUser string
}

// GetContestStatistics 比赛统计（conteststatistics.php）。
func GetContestStatistics(ctx context.Context, q dbgen.FullQuerier, cid int32) ([]StatRow, error) {
	return nil, nil
}

// ── 比赛列表页渲染（contest.php 无 cid）────────────────────────────────

// contestSetPageSize 对应 contest.php:146 的 $page_cnt。
const contestSetPageSize = 25

// contestSetItem 是比赛列表的一行。
//
// 字段与 contestset.php:74-77 的 5 个单元格一一对应：
// 编号 / 名称 / 时间 / 开放 / 创建人。PHP 用 $view_contest[$i][0..6]
// 的下标错位（跳过 3 或 4）来凑够 5 格，这里直接展开成显式字段，
// 可观察输出一致但不再依赖那个下标技巧。
type contestSetItem struct {
	CID   int32
	Title string
	// 时间格的三个部分，对应 contest.php:255-265：
	// 状态词 + 日期 + （仅未结束）时长标签与数值。
	StatusText   string // 已结束 / 开始于 / 运行中
	StatusTone   string // muted / success / danger，决定 span 的 class
	DateText     string // 原始 start_time 或 end_time 字符串
	StatusLabel  string // 总赛时 / 剩余，未结束时才有
	StatusLength string // formatTimeLength 的输出，未结束时才有
	IsPrivate    bool
	Owner        string
}

// contestSetView 是比赛列表页模板的数据。
type contestSetView struct {
	Contests  []contestSetItem
	Page      int
	TotalPage int
	Total     int64
	// PageNumbers 是页码条要显示的页号（当前页 ±8）。
	PageNumbers []int
	Keyword     string
	MyOnly      bool
	// KeywordSource 标记关键词来自 POST 还是 GET。
	//
	// PHP 只读 $_POST['keyword']（contest.php:157），表单也是 method=post。
	// Go 额外接受 GET，方便直接用链接复现某个筛选条件；
	// 两者对渲染结果没有区别，只有回显与日志需要区分来源。
	KeywordSource string
}

// handleContestList 渲染比赛列表，对应 contest.php 无 cid 分支。
//
// 与旧版的差别：旧实现一次性把所有 defunct='N' 的比赛全查出来，
// 而 PHP 是每页 25 条并带总页数。这里补上分页、状态文案、
// 公开/私有标记与"我的比赛"筛选。
func (s *Server) handleContestList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := s.queries()
	id := s.resolveIdentity(w, r)
	rq := r.URL.Query()

	page := parseInt(rq.Get("page"))
	if page < 1 {
		page = 1
	}

	total, err := q.CountActiveContests(ctx)
	if err != nil {
		s.log.Error("统计比赛数失败", "err", err)
		http.Error(w, "比赛列表暂时不可用", http.StatusInternalServerError)
		return
	}
	// PHP: intval($total / 25) + 1（contest.php:154）。
	// 总数恰好是 25 的倍数时会多出一页空列表，属于 PHP 的既有表现，
	// 照搬以免页码链接的形状与 PHP 不一致。
	totalPage := int(total/contestSetPageSize) + 1

	// 关键词：PHP 读 POST，这里 POST 优先、GET 兜底。
	keyword, src := "", ""
	if v := r.FormValue("keyword"); v != "" {
		keyword, src = v, "post"
	} else if v := rq.Get("keyword"); v != "" {
		keyword, src = v, "get"
	}

	filter := dbgen.ContestSetFilter{Keyword: keyword}

	// "我的比赛"：仅在带 my 参数且确实有参赛记录时才生效（contest.php:214）。
	if rq.Get("my") != "" && id.LoggedIn {
		mine, err := s.myContestIDs(ctx, id)
		if err != nil {
			s.log.Error("查询我的比赛失败", "err", err, "user", id.UserID)
			http.Error(w, "比赛列表暂时不可用", http.StatusInternalServerError)
			return
		}
		if len(mine) > 0 {
			filter.MyContests = mine
			filter.UserID = id.UserID
		}
	}

	offset := int32((page - 1) * contestSetPageSize)
	rows, err := q.ListContestSet(ctx, filter, contestSetPageSize, offset)
	if err != nil {
		s.log.Error("查询比赛列表失败", "err", err)
		http.Error(w, "比赛列表暂时不可用", http.StatusInternalServerError)
		return
	}

	now := time.Now().Unix()
	view := contestSetView{
		Page:          page,
		TotalPage:     totalPage,
		Total:         total,
		Keyword:       keyword,
		MyOnly:        rq.Get("my") != "" && len(filter.MyContests) > 0,
		KeywordSource: src,
		PageNumbers:   contestPageWindow(page, totalPage),
		Contests:      make([]contestSetItem, 0, len(rows)),
	}
	for _, row := range rows {
		title := strings.TrimSpace(row.Title.String)
		if title == "" {
			// PHP: $row['title'] = $MSG_CONTEST . $row['contest_id']
			title = "比赛" + itoa32(row.ContestID)
		}
		item := contestSetItem{
			CID:       row.ContestID,
			Title:     title,
			IsPrivate: row.Private != 0,
			Owner:     row.UserID,
		}
		if row.StartTime.Valid && row.EndTime.Valid {
			st := contestStatusText(row.StartTime.Time.Unix(), row.EndTime.Time.Unix(), now)
			item.StatusText = st.State
			item.DateText = st.Detail
			item.StatusLabel = st.Extra
			item.StatusLength = st.Length
			item.StatusTone = st.Tone
		}
		view.Contests = append(view.Contests, item)
	}

	renderPage(w, http.StatusOK, "contestList", "比赛列表", view)
}

// myContestIDs 汇总"我参加过的比赛"，对应 contest.php:169-213。
//
// 三路来源，与 PHP 一致：
//  1. solution 里 contest_id>0 的记录（真参加过）
//  2. 会话里的 c<cid> 受邀标记
//  3. 会话里的 m<cid> 限时赛标记
//
// 第 3 路的 m 标记语义特殊：PHP 把 m 也算进 $mycontests，
// 于是"被指派为限时赛管理员但一场没打"的人也能在"我的比赛"里看到它。
// 照搬。VIP 用户的比赛在登录时已展开进 ContestInvites，无需单独查。
func (s *Server) myContestIDs(ctx context.Context, id Identity) ([]int32, error) {
	seen := make(map[int32]bool)
	var out []int32
	add := func(ids []int32) {
		for _, cid := range ids {
			if cid <= 0 || seen[cid] {
				continue
			}
			seen[cid] = true
			out = append(out, cid)
		}
	}
	sub, err := s.queries().ListMyContestIDs(ctx, id.UserID)
	if err != nil {
		return nil, err
	}
	add(sub)
	for cid := range id.ContestInvites {
		add([]int32{cid})
	}
	for cid := range id.ContestTimeLimitUsed {
		add([]int32{cid})
	}
	sortInt32(out)
	return out, nil
}

// contestPageSection 对应 contestset.php:39 的 $section=8：
// 页码条只显示当前页前后各 8 页。
const contestPageSection = 8

// contestPageWindow 算出要显示的页码区间，对应 contestset.php:40-41。
//
// PHP 的写法是 `page>8 ? page-8 : 1` 与 `page+8>total ? total : page+8`，
// 两端都可以超出总页数，因此靠循环里的 $i<=$end 兜底。
// 这里显式裁剪，输出一致。
func contestPageWindow(page, totalPage int) []int {
	start := page - contestPageSection
	if start < 1 {
		start = 1
	}
	end := page + contestPageSection
	if end > totalPage {
		end = totalPage
	}
	if totalPage < 1 || start > end {
		// start > end 覆盖页码越界的情况（如 ?page=9999）：
		// PHP 的 for($i=$start; $i<=$end; $i++) 天然一次都不进循环。
		return nil
	}
	out := make([]int, 0, end-start+1)
	for i := start; i <= end; i++ {
		out = append(out, i)
	}
	return out
}

// itoa32 是 int32 的十进制字符串形式，避免在模板外散落 strconv。
func itoa32(n int32) string { return strconv.FormatInt(int64(n), 10) }

// sortInt32 对 cid 列表排序。
//
// PHP 靠 SQL 的 IN 列表匹配，顺序不影响结果集；
// 但 Go 侧把 cid 列表写进占位符，顺序变化会让 sqlc 生成的
// 缓存键与日志错位，排查问题时不好对照，所以固定成升序。
func sortInt32(v []int32) { sort.Slice(v, func(i, j int) bool { return v[i] < v[j] }) }

// contestTypeName 比赛类型显示名。
func contestTypeName(t int16) string {
	switch t {
	case 1:
		return "ACM"
	case 2:
		return "OI"
	default:
		return "Unknown"
	}
}

// ── 比赛题目页（contest.php?cid=X）──────────────────────────────────────

func (s *Server) handleContestProblems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	// PHP 是 `abs(intval($_GET['cid']))`（contest-check.php:3）：
	// 非数字、负数、缺省统统变成 0 或其绝对值，随后走"查不到比赛"分支。
	// Go 侧的 parseInt 已经是非严格解析，这里再取一次绝对值即可对齐。
	cid := abs32(int32(parseInt(q.Get("cid"))))

	// 查不到比赛：PHP 把 $view_title 改成"比赛已经关闭!"、
	// $contest_ok 置 false，最后仍然渲染错误页并 exit(0)（HTTP 200）。
	// 不返回 404 —— 那会让浏览器缓存与监控把"比赛已关闭"误判成资源不存在。
	contest, err := s.queries().GetContestByID(r.Context(), cid)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			s.log.Error("查询比赛失败", "err", err, "cid", cid)
			http.Error(w, "比赛信息暂时不可用", http.StatusInternalServerError)
			return
		}
		// 传 nil 而不是空的 Contest 行：newContestErrorView 用它区分
		// "比赛不存在"（标题"比赛已经关闭!"、描述"Net in 子网 "）
		// 与"比赛存在但没权限"（用真实标题与描述）。
		renderPage(w, http.StatusOK, "contestError", "错误信息",
			newContestErrorView(cid, nil, ContestNotFound, false))
		return
	}

	id := s.resolveIdentity(w, r)
	now := time.Now()
	access, err := EvaluateContestAccess(r.Context(), s.queries(), contest, id, now,
		remoteIP(r), hasPHPSession(r))
	if err != nil {
		s.log.Error("比赛准入判定失败", "err", err, "cid", cid)
		http.Error(w, "比赛信息暂时不可用", http.StatusInternalServerError)
		return
	}
	if !access.OK() {
		renderPage(w, http.StatusOK, "contestError", "比赛准入失败",
			newContestErrorView(cid, &contest, access.Deny, access.Deny == ContestSubnet))
		return
	}

	problems, err := GetContestProblems(r.Context(), s.queries(), sql.NullInt32{Int32: cid, Valid: true})
	if err != nil {
		s.log.Error("查询比赛题目失败", "err", err, "cid", cid)
		http.Error(w, "比赛题目暂时不可用", http.StatusInternalServerError)
		return
	}

	flags, err := s.contestNoIPFlags(r.Context(), s.queries(), contest, cid, id, now.Unix())
	if err != nil {
		s.log.Error("判定 NOIP/封榜失败", "err", err, "cid", cid)
		http.Error(w, "比赛题目暂时不可用", http.StatusInternalServerError)
		return
	}
	flags.LoggedIn = id.LoggedIn
	// $contest_ok 由 contest-check.php 设置，代表赛后仍可按 cid 取题。
	// 未封榜（RankLockPercent=0）或已过封榜点时为真。
	flags.ContestOK = access.OK()

	if err := s.fillContestProblems(r.Context(), s.queries(), problems, cid, id, flags); err != nil {
		s.log.Error("填充题目状态失败", "err", err, "cid", cid)
		http.Error(w, "比赛题目暂时不可用", http.StatusInternalServerError)
		return
	}

	renderPage(w, http.StatusOK, "contestProblems", contestTitle(contest.Title.String, cid),
		contestProblemsView{Contest: contest, CID: cid, Problems: problems, Flags: flags})
}

// ── 比赛排名页（contestrank.php / contestrank-oi.php）────────────────────

// ── 比赛统计页（conteststatistics.php）──────────────────────────────────
//
// 实现见 contest_statistics.go。

// parseInt 复刻 PHP intval。
func parseInt(s string) int {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	sign := 1
	if s[0] == '-' {
		sign = -1
		s = s[1:]
	} else if s[0] == '+' {
		s = s[1:]
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
		if n > 1<<31-1 {
			n = 1<<31 - 1
			break
		}
	}
	return n * sign
}

// registerContestRoutes 挂载比赛相关路由。
func (s *Server) registerContestRoutes(mux *http.ServeMux) {
	// 比赛列表的搜索表单是 method=post（template/syzoj/contestset.php:7），
	// 而 PHP contest.php:157 也只读 $_POST['keyword']。
	// 只注册 GET 会让表单 POST 直接 405，所以两个方法都挂同一个 handler。
	mux.HandleFunc("/contest.php", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("cid") != "" {
			s.handleContestProblems(w, r)
		} else {
			s.handleContestList(w, r)
		}
	})
	// 两个赛制各走一个入口：contestrank.php 是 ACM 赛制，
	// contestrank-oi.php 是 OI 赛制（计分规则不同，见 handler_rank.go）。
	mux.HandleFunc("GET /contestrank.php", func(w http.ResponseWriter, r *http.Request) {
		s.handleContestRankMode(w, r, rankACM)
	})
	mux.HandleFunc("GET /contestrank-oi.php", func(w http.ResponseWriter, r *http.Request) {
		s.handleContestRankMode(w, r, rankOI)
	})
	// 补题榜：比赛结束后在练习里补做比赛原题的排名，取数口径与上面两个正式榜
	// 不同（按 problem_id + 开赛时间取，不按 contest_id），见
	// handler_contest_review.go 的说明。
	mux.HandleFunc("GET /contestrank4.php", func(w http.ResponseWriter, r *http.Request) {
		s.handleContestReviewRank(w, r, rankACM)
	})
	mux.HandleFunc("GET /contestrank5.php", func(w http.ResponseWriter, r *http.Request) {
		s.handleContestReviewRank(w, r, rankOI)
	})
	mux.HandleFunc("GET /contestrank.xls.php", s.handleContestRankXLS)
	mux.HandleFunc("GET /conteststatistics.php", s.handleContestStatistics)
}
