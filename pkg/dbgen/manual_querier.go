// 本文件不由 sqlc 生成，请勿删除。
//
// 这里的查询在运行时按参数拼可变长度的 NOT IN / solution_id 区间，
// sqlc 无法静态生成，因此 SQL 与扫描逻辑写在 userinfo.sql.go、
// ranklist.sql.go、conteststatistics.sql.go 中。生成文件 querier.go
// 只包含 sqlc 管理的方法，手写方法统一声明在这里，避免每次
// sqlc generate 都把手加的方法从 querier.go 里抹掉导致编译失败。
package dbgen

import "context"

// ManualQuerier 汇总 sqlc 无法静态生成的手写查询方法。
type ManualQuerier interface {
	// ── 档案页（userinfo.php）与比赛列表（contest.php）────────────────
	//
	// 实现见 userinfo.sql.go：这批查询需要按运行期参数拼一段
	// 可变长度的 NOT IN。
	// 参数语义逐条对应 PHP 源码，见该文件里每个方法的注释。

	// 剔除统计用的进行中 NOIP 比赛（userinfo.php:92）。
	ListActiveNOIPContests(ctx context.Context, now string, keyword string) ([]int32, error)
	// 按 user_id 取 users 行（userinfo.php:105）。
	GetUserProfile(ctx context.Context, userID string) (User, error)
	// 通过题数 / 提交题数，均去重（userinfo.php:130、:136）。
	CountUserACProblems(ctx context.Context, userID string, exclude []int32) (int64, error)
	CountUserSubmittedProblems(ctx context.Context, userID string, exclude []int32) (int64, error)
	// 把计数写回 users —— GET 请求里的写操作，PHP 亦然（userinfo.php:142）。
	UpdateUserSolvedSubmit(ctx context.Context, solved, submit int32, userID string) error
	// 名次 = 该计数 + 1（userinfo.php:144）。
	CountUsersAhead(ctx context.Context, solved int32, excludeHidden []string) (int64, error)
	// 按首杀题重算金币，PHP 每次打开档案页都重算（userinfo.php:150）。
	RecalcUserCoinEarned(ctx context.Context, userID string) error
	// 登录日志含明文口令，**仅管理员路径可调用**（userinfo.php:162）。
	ListUserLoginLog(ctx context.Context, userID string) ([]UserLoginLogRow, error)
	// 饼图：result>=4 的结果分布（userinfo.php:165）。
	ListUserResultStats(ctx context.Context, userID string, exclude []int32) ([]UserResultStatRow, error)
	// 比赛战绩列表（模板 userinfo.php:114）。
	ListUserContestStats(ctx context.Context, userID string, keyword string) ([]UserContestStatRow, error)
	// 已通过 / 未通过的题号与次数（模板 userinfo.php:184、:212）。
	ListUserACProblems(ctx context.Context, userID string, exclude []int32) ([]UserProblemCountRow, error)
	ListUserNonACProblems(ctx context.Context, userID string, exclude []int32) ([]UserProblemCountRow, error)
	// 日历热力图：最近一年每日提交数（模板 userinfo.php:366）。
	ListUserSubChart(ctx context.Context, userID string, exclude []int32) ([]UserSubChartRow, error)
	// 访客已通过的题号，决定色块绿/红（userinfo.php:194）。
	ListViewerACProblems(ctx context.Context, viewerID string, exclude []int32) ([]int32, error)
	// 比赛列表分页（contest.php:149、:218）。
	CountActiveContests(ctx context.Context) (int64, error)
	ListContestSet(ctx context.Context, f ContestSetFilter, limit, offset int32) ([]ContestSetRow, error)
	// "我的比赛"的另外两路来源（contest.php:170、:195）。
	ListMyContestIDs(ctx context.Context, userID string) ([]int32, error)
	ListVIPContestIDs(ctx context.Context) ([]int32, error)
	// 单题在比赛中的最好结果（contest.php:80 的 check_ac）。
	CountUserContestProblemAC(ctx context.Context, userID string, cid, num int32) (ContestProblemACCounts, error)
	// 题目是否被未开始的比赛占用，用于赛后隐藏标题（contest.php:99）。
	ProblemUsedInUpcomingContest(ctx context.Context, problemID int32) (bool, error)

	// ── 比赛统计页（conteststatistics.php）────────────────────────────
	//
	// 实现见 conteststatistics.sql.go。
	ListContestProblemIDs(ctx context.Context, contestID int32) ([]ContestProblemID, error)
	ListContestSolutionStats(ctx context.Context, contestID int32) ([]ContestSolutionStat, error)
	// 每日提交 / AC，驱动统计页底部的柱状图（conteststatistics.php:82、:91）。
	ListContestDailySubmits(ctx context.Context, contestID int32) ([]ContestDailyCount, error)
	ListContestDailyAC(ctx context.Context, contestID int32) ([]ContestDailyCount, error)
	// 题目是否仍被未结束的比赛占用，决定赛后题号是否可点（模板 :181-186）。
	ProblemUsedInOngoingContest(ctx context.Context, problemID int32) (bool, error)

	// ── 全局排名页（ranklist.php）────────────────────────────────────
	//
	// 实现见 ranklist.sql.go：这批查询需要按 $OJ_RANK_HIDDEN 与 scope
	// 时间窗口拼可变长度的 NOT IN / solution_id 区间，不能静态生成。
	// 进行中 NOIP 比赛计数（ranklist.php:10）。
	CountRunningNOIPContests(ctx context.Context, now string, keyword string) (int64, error)
	// 非 scope 模式的分页总数（ranklist.php:43）。
	CountRanklistUsers(ctx context.Context) (int64, error)
	// 非 scope 模式取一页（ranklist.php:60）。
	ListRanklistUsers(ctx context.Context, f RanklistFilter, offset, limit int32) ([]RanklistUserRow, error)
	// 某日期之前最后一条提交的 solution_id（ranklist.php:69、:79）。
	LastSolutionIDBefore(ctx context.Context, date string) (int64, error)
	// scope 模式的分页总数（ranklist.php:85）。
	CountRanklistScopedUsers(ctx context.Context, sc RanklistScope) (int64, error)
	// scope 模式取一页（ranklist.php:86）。
	ListRanklistScopedUsers(ctx context.Context, sc RanklistScope, hidden []string, offset, limit int32) ([]RanklistUserRow, error)

	// ── 题目提交状态页（problemstatus.php）──────────────────────────
	//
	// 实现见 problemstatus_manual.go。
	ListProblemBestACSolutions(ctx context.Context, problemID int32, offset, limit int32) ([]ProblemBestACSolRow, error)

	// ── 排行榜导出（admin/ranklist_export.php）────────────────────────
	//
	// 实现见 ranklist_export_manual.go：where 子句随 prefix 变化，
	// scope 用 in_date 窗口而非 solution_id 区间，不能静态生成。
	ExportRanklistUsers(ctx context.Context, f RanklistExportFilter, offset, limit int32) ([]RanklistExportRow, error)
	ExportRanklistScoped(ctx context.Context, since string, offset, limit int32) ([]RanklistExportRow, error)
}

// FullQuerier 是 sqlc 生成的 Querier 与手写 ManualQuerier 的合集。
// Web 层统一使用这个接口，这样生成文件与手写文件都能被同一处持有。
type FullQuerier interface {
	Querier
	ManualQuerier
}

var _ FullQuerier = (*Queries)(nil)
