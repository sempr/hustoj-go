// 题目列表/详情的仓储层。
//
// 分层不是审美选择，而是绞杀期的硬性要求：查询逻辑必须与
// phpweb/problemset.php、phpweb/problem.php **逐条对应**，否则用户在
// 流量被 nginx 切到 Go 的那一刻会看到列表少题、顺序变化或 AC 标记消失。
// 因此每处都注明了 PHP 侧来源分支与等价性约束，改动前先读注释。
package web

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/sempr/hustoj-go/pkg/dbgen"
)

// pageSize 对应 problemset.php 的 $page_cnt = 50。
// 刻意不做成可配置：两端分页必须一致，否则翻页会出现重复或漏题。
const pageSize = 50

// resultAC 是判罚码"Accepted"。取自 pkg/constants 的 OJ_* 定义，
// 勿改用字面量 4，也不要改成 >=4（>=4 只是"提交过"，含部分分）。
const resultAC = 4

// Viewer 描述访问者的可见范围，对应 problemset.php 的三分支。
type Viewer int

const (
	// ViewerMember 普通用户：只看 defunct='N' 且排除比赛进行中的题目。
	ViewerMember Viewer = iota
	// ViewerFreePractice 自由练习（$OJ_FREE_PRACTICE=1）：只看 defunct='N'，
	// 不排除比赛题。
	ViewerFreePractice
	// ViewerAdmin 管理员：不加 defunct/比赛过滤，能看到已删除题目。
	ViewerAdmin
)

// String 让日志与错误信息里的模式名可读。
func (v Viewer) String() string {
	switch v {
	case ViewerAdmin:
		return "admin"
	case ViewerFreePractice:
		return "free_practice"
	default:
		return "member"
	}
}

// ProblemSummary 是列表页一行。
// 注意 Accepted 有两个不同含义，用不同名字区分：
//   - AcceptedStat：problem.accepted 列（本题全站通过次数，可为 NULL）
//   - AcceptedByUser：当前用户是否 AC
//
// 混用一个名字必然互相覆盖，是很容易犯且很难查的 bug。
type ProblemSummary struct {
	ProblemID int32
	Title     string
	// Source 是空格分隔的多个来源标签，PHP 侧用 explode(" ", source) 拆开渲染。
	Source       string
	SubmitStat   sql.NullInt32
	AcceptedStat sql.NullInt32
	Defunct      string
	Coin         uint32

	// Contests 是这道题参加过的比赛（题库列表的"比赛"列）。
	Contests []ContestLink

	SolvedByUser   bool
	AcceptedByUser bool
}

// ContestLink 是题库列表里指向某个比赛的链接。
type ContestLink struct {
	ContestID int32
	Title     string
}

// ProblemListPage 是列表页渲染所需的数据。
type ProblemListPage struct {
	Problems  []ProblemSummary
	Total     int64
	Page      int
	PageSize  int
	TotalPage int64
	Search    string
	Viewer    Viewer
}

// ProblemDetail 是题目详情页数据。
type ProblemDetail struct {
	ProblemID    int32
	Title        string
	Description  string
	Input        string
	Output       string
	SampleInput  sql.NullString
	SampleOutput sql.NullString
	SPJ          string
	Hint         string
	Source       sql.NullString
	InDate       sql.NullTime
	TimeLimit    float64
	MemoryLimit  int32
	Defunct      string
	Coin         uint32
	SubmitStat   sql.NullInt32
	AcceptedStat sql.NullInt32
	SolvedStat   sql.NullInt32
	RemoteOJ     sql.NullString
	RemoteID     sql.NullString

	// ── 比赛模式专属字段（problem.php:53-110 的 $co_flag 分支）──
	//
	// 练习模式全部为零值。Contest 为 nil 表示不是比赛模式。
	Contest       *ContestDetail
	ContestIsOver bool
	// StatsHidden 表示 NOIP 锁榜：AC 数与提交数不可见（problem.php:158）。
	// HintHidden 表示同时隐藏提示（problem.php:163-167，OJ_NOIP_HINT 为 false 时）。
	StatsHidden bool
	HintHidden  bool

	// ── 强制文件读写（freopen）提示 ──
	//
	// InputFileName / OutputFileName 非空表示该题要求文件读写：程序需
	// freopen 到这些文件，而不是使用标准输入/输出。取自题目数据目录的
	// input.name / output.name（见 problemFileIONames）。
	InputFileName  string
	OutputFileName string
}

// ContestDetail 是比赛模式下渲染按钮所需的比赛信息。
//
// 字段都来自 contest 表，只取模板真正用到的：langmask 决定提交页的
// 语言选择，IsOver 决定提交入口走"按练习提交"还是"按比赛提交"。
type ContestDetail struct {
	CID      int32
	Title    string
	Langmask uint32
	PID      int32
	IsOver   bool
}

// SearchPattern 构造 PHP 侧的 "%kw%" 匹配串。
//
// 刻意不做 LIKE 转义：PHP 用的是裸 `like ?`，用户输入的 % 与 _ 在两端
// 都作为通配符。转义它们会让"搜 foo_bar"在 Go 侧查不到而在 PHP 侧查得到，
// 属于可见的行为差异；要改必须 PHP/Go 同时改，单改一侧即回归。
func SearchPattern(kw string) string { return "%" + kw + "%" }

// LikeArg 包装 LIKE 的匹配串参数。
//
// sqlc 沿用 source 列的可空性，把 `source LIKE ?` 的参数推成 sql.NullString，
// 即使在查询里套 COALESCE 也不改变推断。这里用一处仪式换取 SQL 与 PHP
// 逐字一致，不为了调用顺眼而改查询语义。
func LikeArg(pattern string) sql.NullString {
	return sql.NullString{String: pattern, Valid: true}
}

// nowArg 供成员模式排除比赛题使用。
// PHP 用 date('Y-m-d H:i', time())，其时区来自 init.php 的配置；本服务同样
// 不在代码里写死时区：time.Now() 取进程 TZ（systemd 注入），DSN 的 time_zone
// 默认跟随同一时区。跨时区部署时只需改单元文件里的 TZ（或 judge.conf 的
// HUSTOJ_DB_TIME_ZONE），两侧自动保持一致。
func nowArg(now time.Time) sql.NullTime { return sql.NullTime{Time: now, Valid: true} }

// SplitSource 按空白拆分题目来源标签。
// PHP 用 explode(" ", source) 再逐个 trim，末尾还会跳过 "" 与 "&nbsp"；
// strings.Fields 对连续空格与首尾空白结果一致，"&nbsp" 由模板层过滤。
func SplitSource(s string) []string { return strings.Fields(s) }

// count 按访问者模式选对应的计数查询。
func count(ctx context.Context, q dbgen.FullQuerier, v Viewer, kw string, now time.Time) (int64, error) {
	switch v {
	case ViewerAdmin:
		if kw != "" {
			return q.CountSearchProblemsForAdmin(ctx, dbgen.CountSearchProblemsForAdminParams{
				TitleLike: SearchPattern(kw), SourceLike: LikeArg(SearchPattern(kw))})
		}
		return q.CountProblemsForAdmin(ctx)
	case ViewerFreePractice:
		if kw != "" {
			return q.CountSearchProblemsFreePractice(ctx, dbgen.CountSearchProblemsFreePracticeParams{
				TitleLike: SearchPattern(kw), SourceLike: LikeArg(SearchPattern(kw))})
		}
		return q.CountProblemsFreePractice(ctx)
	default:
		if kw != "" {
			return q.CountSearchProblemsForMember(ctx, dbgen.CountSearchProblemsForMemberParams{
				TitleLike: SearchPattern(kw), SourceLike: LikeArg(SearchPattern(kw)), Now: nowArg(now)})
		}
		return q.CountProblemsForMember(ctx, nowArg(now))
	}
}

// list 按访问者模式选对应的列表查询，并把各查询各自的 Row 类型归一到
// ProblemSummary。归一在这里做而不是让 sqlc 生成统一类型，是为了保持
// 每条查询都能直接对着 PHP 源码核对。
func list(ctx context.Context, q dbgen.FullQuerier, v Viewer, kw string, now time.Time, offset int) ([]ProblemSummary, error) {
	limit := int32(pageSize)
	off := int32(offset)

	switch v {
	case ViewerAdmin:
		if kw != "" {
			rows, err := q.SearchProblemsForAdmin(ctx, dbgen.SearchProblemsForAdminParams{
				TitleLike: SearchPattern(kw), SourceLike: LikeArg(SearchPattern(kw)),
				Limit: limit, Offset: off})
			if err != nil {
				return nil, err
			}
			out := make([]ProblemSummary, 0, len(rows))
			for _, r := range rows {
				out = append(out, ProblemSummary{
					ProblemID: r.ProblemID, Title: r.Title, Source: r.Source.String,
					SubmitStat: r.Submit, AcceptedStat: r.Accepted,
					Defunct: r.Defunct, Coin: r.Coin})
			}
			return out, nil
		}
		rows, err := q.ListProblemsForAdmin(ctx, dbgen.ListProblemsForAdminParams{
			Limit: limit, Offset: off})
		if err != nil {
			return nil, err
		}
		out := make([]ProblemSummary, 0, len(rows))
		for _, r := range rows {
			out = append(out, ProblemSummary{
				ProblemID: r.ProblemID, Title: r.Title, Source: r.Source.String,
				SubmitStat: r.Submit, AcceptedStat: r.Accepted,
				Defunct: r.Defunct, Coin: r.Coin})
		}
		return out, nil

	case ViewerFreePractice:
		if kw != "" {
			rows, err := q.SearchProblemsFreePractice(ctx, dbgen.SearchProblemsFreePracticeParams{
				TitleLike: SearchPattern(kw), SourceLike: LikeArg(SearchPattern(kw)),
				Limit: limit, Offset: off})
			if err != nil {
				return nil, err
			}
			out := make([]ProblemSummary, 0, len(rows))
			for _, r := range rows {
				out = append(out, ProblemSummary{
					ProblemID: r.ProblemID, Title: r.Title, Source: r.Source.String,
					SubmitStat: r.Submit, AcceptedStat: r.Accepted,
					Defunct: r.Defunct, Coin: r.Coin})
			}
			return out, nil
		}
		rows, err := q.ListProblemsFreePractice(ctx, dbgen.ListProblemsFreePracticeParams{
			Limit: limit, Offset: off})
		if err != nil {
			return nil, err
		}
		out := make([]ProblemSummary, 0, len(rows))
		for _, r := range rows {
			out = append(out, ProblemSummary{
				ProblemID: r.ProblemID, Title: r.Title, Source: r.Source.String,
				SubmitStat: r.Submit, AcceptedStat: r.Accepted,
				Defunct: r.Defunct, Coin: r.Coin})
		}
		return out, nil

	default:
		if kw != "" {
			rows, err := q.SearchProblemsForMember(ctx, dbgen.SearchProblemsForMemberParams{
				TitleLike: SearchPattern(kw), SourceLike: LikeArg(SearchPattern(kw)),
				Now: nowArg(now), Limit: limit, Offset: off})
			if err != nil {
				return nil, err
			}
			out := make([]ProblemSummary, 0, len(rows))
			for _, r := range rows {
				out = append(out, ProblemSummary{
					ProblemID: r.ProblemID, Title: r.Title, Source: r.Source.String,
					SubmitStat: r.Submit, AcceptedStat: r.Accepted,
					Defunct: r.Defunct, Coin: r.Coin})
			}
			return out, nil
		}
		rows, err := q.ListProblemsForMember(ctx, dbgen.ListProblemsForMemberParams{
			Now: nowArg(now), Limit: limit, Offset: off})
		if err != nil {
			return nil, err
		}
		out := make([]ProblemSummary, 0, len(rows))
		for _, r := range rows {
			out = append(out, ProblemSummary{
				ProblemID: r.ProblemID, Title: r.Title, Source: r.Source.String,
				SubmitStat: r.Submit, AcceptedStat: r.Accepted,
				Defunct: r.Defunct, Coin: r.Coin})
		}
		return out, nil
	}
}

// ListProblems 取题目列表，并按当前用户填充完成状态标记。
//
// page 从 1 开始，与 PHP 的 $page 语义一致。越界页返回空列表而非报错，
// 对应 PHP 在超出末页时 foreach 空结果什么都不输出。
func ListProblems(
	ctx context.Context, q dbgen.FullQuerier,
	v Viewer, userID string, page int, kw string, now time.Time,
) (ProblemListPage, error) {
	if page < 1 {
		page = 1
	}

	total, err := count(ctx, q, v, kw, now)
	if err != nil {
		return ProblemListPage{}, fmt.Errorf("统计题目数(%s)失败: %w", v, err)
	}

	rows, err := list(ctx, q, v, kw, now, (page-1)*pageSize)
	if err != nil {
		return ProblemListPage{}, fmt.Errorf("查询题目列表(%s)失败: %w", v, err)
	}

	totalPage := (total + pageSize - 1) / pageSize
	if totalPage == 0 {
		totalPage = 1
	}

	out := ProblemListPage{
		Problems:  rows,
		Total:     total,
		Page:      page,
		PageSize:  pageSize,
		TotalPage: totalPage,
		Search:    kw,
		Viewer:    v,
	}

	// 题库列表的"比赛"列：一道题参加过的比赛。
	// 独立于完成状态查询，匿名用户也要能看到比赛链接。
	if err := attachContests(ctx, q, out.Problems); err != nil {
		return ProblemListPage{}, err
	}

	// 当前用户完成状态：PHP 是单独一条
	//   select problem_id, MIN(result) from solution where user_id=? and result>=4 group by problem_id
	// 未登录时 PHP 根本不发这条查询，Go 侧同样跳过，避免无谓的库查询。
	if userID == "" {
		return out, nil
	}
	best, err := q.GetUserBestResults(ctx, userID)
	if err != nil {
		return ProblemListPage{}, fmt.Errorf("查询用户完成状态失败: %w", err)
	}
	solved := make(map[int32]bool, len(best))
	accepted := make(map[int32]bool, len(best))
	for _, r := range best {
		solved[r.ProblemID] = true
		if r.Result == resultAC {
			accepted[r.ProblemID] = true
		}
	}
	for i := range out.Problems {
		out.Problems[i].SolvedByUser = solved[out.Problems[i].ProblemID]
		out.Problems[i].AcceptedByUser = accepted[out.Problems[i].ProblemID]
	}
	return out, nil
}

// attachContests 给每道题挂上它参加过的比赛。
//
// 只查当前页的题号（最多 50 个），一条 IN 查询搞定。
// 查不到关联是正常情况（题库里的绝大多数题没参加过比赛）。
func attachContests(ctx context.Context, q dbgen.FullQuerier, problems []ProblemSummary) error {
	if len(problems) == 0 {
		return nil
	}
	ids := make([]int32, 0, len(problems))
	for _, p := range problems {
		ids = append(ids, p.ProblemID)
	}
	rows, err := q.ListProblemContests(ctx, ids)
	if err != nil {
		return fmt.Errorf("查询题目所属比赛失败: %w", err)
	}
	byProblem := make(map[int32][]ContestLink, len(rows))
	for _, r := range rows {
		byProblem[r.ProblemID] = append(byProblem[r.ProblemID], ContestLink{
			ContestID: r.ContestID,
			Title:     r.Title.String,
		})
	}
	for i := range problems {
		problems[i].Contests = byProblem[problems[i].ProblemID]
	}
	return nil
}

// GetProblem 按访问者模式取题目详情。
//
// 可见性规则来自 problem.php:33-50，比列表页更严：成员模式下题目被
// 「进行中的比赛」或「私有比赛」引用时都查不到（problem.php:44 的
// `c.private='1'`）。列表页没有 private 条件，两页规则不一致是上游既有行为，
// 这里照搬 —— 放宽或收紧任何一侧都会让用户在切流瞬间看到不同的题目。
//
// 查不到（sql.ErrNoRows）由调用方转成 404。
func GetProblem(ctx context.Context, q dbgen.FullQuerier, v Viewer, id int32, now time.Time) (ProblemDetail, error) {
	var (
		r   dbgen.Problem
		err error
	)
	switch v {
	case ViewerAdmin:
		// 管理员不加过滤，能看到已删除题目（problem.php:35）
		r, err = q.GetProblemByID(ctx, id)
	case ViewerFreePractice:
		// 自由练习只要求 defunct='N'（problem.php:38）
		r, err = q.GetProblemFreePractice(ctx, id)
	default:
		// 成员：defunct='N' 且不被进行中/私有比赛引用（problem.php:41-44）
		r, err = q.GetProblemForMember(ctx, dbgen.GetProblemForMemberParams{
			ProblemID: id, Now: nowArg(now)})
	}
	if err != nil {
		return ProblemDetail{}, fmt.Errorf("查询题目 %d 失败: %w", id, err)
	}
	return problemFromRow(r), nil
}

// problemFromRow 把 dbgen 行映射成视图模型。
//
// 字段改名的地方（Submit → SubmitStat 等）都在这里收口，
// 避免每条查询各写一份映射后逐渐走样。
func problemFromRow(r dbgen.Problem) ProblemDetail {
	return ProblemDetail{
		ProblemID:    r.ProblemID,
		Title:        r.Title,
		Description:  r.Description,
		Input:        r.Input,
		Output:       r.Output,
		SampleInput:  r.SampleInput,
		SampleOutput: r.SampleOutput,
		SPJ:          r.Spj,
		Hint:         r.Hint,
		Source:       r.Source,
		InDate:       r.InDate,
		TimeLimit:    r.TimeLimit,
		MemoryLimit:  r.MemoryLimit,
		Defunct:      r.Defunct,
		Coin:         r.Coin,
		SubmitStat:   r.Submit,
		AcceptedStat: r.Accepted,
		SolvedStat:   r.Solved,
		RemoteOJ:     r.RemoteOj,
		RemoteID:     r.RemoteID,
	}
}

// judgeTypeName 把 problem.spj 的数值翻译成题面顶部展示的中文类型名。
// 与后台题目编辑页的选项保持一致（0 普通 / 1 SPJ / 2 判断题 / 3 交互 / 4 通讯）；
// 0 与空串返回空，表示不需要展示类型标签。
func judgeTypeName(spj string) string {
	switch spj {
	case "", "0":
		return ""
	case "1":
		return "特殊评测（SPJ）"
	case "2":
		return "判断题（Raw Text）"
	case "3":
		return "交互题"
	case "4":
		return "通讯题（多轮交互）"
	default:
		return "特殊评测（SPJ）"
	}
}

// judgeTypeTip 给类型标签补充一句说明：讲清这种题目与普通题的区别，
// 让选手在题面顶部就能知道该怎么读写数据。
func judgeTypeTip(spj string) string {
	switch spj {
	case "1":
		return "答案可能不唯一，由评测程序检查你的输出是否合法，而不是逐字比对标准答案。"
	case "2":
		return "按原始文本比对输出，不做数值或空白归一化。"
	case "3":
		return "程序需要与评测器通过标准输入/输出双向交互，每个测试点是一次独立会话。"
	case "4":
		return "同一程序会在一组数据上被重复运行多轮；轮与轮之间内存清空，只能依据评测器在轮间给出的输入作答。"
	default:
		return ""
	}
}
