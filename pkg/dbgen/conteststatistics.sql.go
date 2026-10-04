// 比赛统计页（conteststatistics.php）的查询集合。
//
// 与 userinfo.sql.go 一样，这批查询手写而非 sqlc 生成：统计页需要把
// 每个 contest_problem.num 与 solution.num 做交叉聚合，sqlc 的 MySQL
// 解析器对派生表 + 动态别名支持不稳，手写反而更直观、更好逐条注释。

package dbgen

import (
	"context"
)

// ContestProblemID 是 contest_problem 的一行：题号下标与真实 problem_id。
//
// 统计页用 num 作为行下标（0 基，对应 $PID[$i] 的字母），
// 用 problem_id 拼赛后题目链接。这里不 JOIN problem 表，
// 是为了复刻 PHP `SELECT count(num) FROM contest_problem` 的计数语义：
// 即使题目被删，contest_problem 行仍计入 pid_cnt，行号不会整体前移。
type ContestProblemID struct {
	Num       int32
	ProblemID int32
}

// ListContestProblemIDs 取比赛的全部题目下标（conteststatistics.php:41）。
func (q *Queries) ListContestProblemIDs(ctx context.Context, contestID int32) ([]ContestProblemID, error) {
	const stmt = `SELECT num, problem_id FROM contest_problem WHERE contest_id = ? ORDER BY num`
	rows, err := q.db.QueryContext(ctx, stmt, contestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContestProblemID
	for rows.Next() {
		var r ContestProblemID
		if err := rows.Scan(&r.Num, &r.ProblemID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ContestSolutionStat 是统计聚合的原始输入：
// SELECT result,num,language FROM solution WHERE contest_id=? and num>=0
// （conteststatistics.php:47）。
type ContestSolutionStat struct {
	Result   int32
	Num      int32
	Language int32
}

// ListContestSolutionStats 取本场全部有效提交的判定结果。
//
// 注意 `num >= 0` 会剔除 num=-1 的题外提交（练习模式提交进这场比赛时
// contest_id 会写但 num 保持 -1），与 PHP 完全一致。
func (q *Queries) ListContestSolutionStats(ctx context.Context, contestID int32) ([]ContestSolutionStat, error) {
	const stmt = `SELECT result, num, language FROM solution WHERE contest_id = ? AND num >= 0`
	rows, err := q.db.QueryContext(ctx, stmt, contestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContestSolutionStat
	for rows.Next() {
		var r ContestSolutionStat
		if err := rows.Scan(&r.Result, &r.Num, &r.Language); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ContestDailyCount 是图表的一个数据点：某天 + 计数。
type ContestDailyCount struct {
	Day   string
	Count int32
}

// ListContestDailySubmits 对应 conteststatistics.php:82 的"每日提交总数"。
//
// 过滤条件 `result>=4 and result<13 and problem_id>0` 与 PHP 原文一致：
// 只统计已判定的正式结果，剔除等待/编译中/运行中等未出结果的状态。
// MySQL 的 `date(in_date)` 在 DATETIME 上按服务器时区取日期。
func (q *Queries) ListContestDailySubmits(ctx context.Context, contestID int32) ([]ContestDailyCount, error) {
	const stmt = `SELECT DATE_FORMAT(date(in_date),'%Y-%m-%d') md, count(1)
FROM solution WHERE contest_id = ? AND result < 13 AND problem_id > 0 AND result >= 4
GROUP BY md ORDER BY md DESC LIMIT 1000`
	return q.queryDailyCounts(ctx, stmt, contestID)
}

// ListContestDailyAC 对应 conteststatistics.php:91 的"每日 AC 数"。
func (q *Queries) ListContestDailyAC(ctx context.Context, contestID int32) ([]ContestDailyCount, error) {
	const stmt = `SELECT DATE_FORMAT(date(in_date),'%Y-%m-%d') md, count(1)
FROM solution WHERE contest_id = ? AND result = 4 AND problem_id > 0
GROUP BY md ORDER BY md DESC LIMIT 1000`
	return q.queryDailyCounts(ctx, stmt, contestID)
}

func (q *Queries) queryDailyCounts(ctx context.Context, stmt string, contestID int32) ([]ContestDailyCount, error) {
	rows, err := q.db.QueryContext(ctx, stmt, contestID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContestDailyCount
	for rows.Next() {
		var r ContestDailyCount
		if err := rows.Scan(&r.Day, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ProblemUsedInOngoingContest 判断题目是否仍被某场未结束的比赛占用。
//
// 对应 conteststatistics.php:181-186 的嵌套查询，条件是 `defunct='N' AND
// now()<end_time`。注意它与 problem.go 里 ProblemUsedInUpcomingContest
// 的区别：那个查的是"未开始"（now<start_time），用于隐藏赛前题目标题；
// 这个查的是"未结束"，用于决定赛后统计页里题号是否还能点开。
// 把两者合并会让未开始比赛的题目在统计页意外变成可点。
func (q *Queries) ProblemUsedInOngoingContest(ctx context.Context, problemID int32) (bool, error) {
	const stmt = `SELECT count(1) FROM problem p
WHERE p.problem_id = ? AND p.problem_id IN (
  SELECT cp.problem_id FROM contest_problem cp
  WHERE cp.contest_id IN (
    SELECT c.contest_id FROM contest c
    WHERE c.defunct = 'N' AND NOW() < c.end_time
  )
)`
	var n int64
	if err := q.db.QueryRowContext(ctx, stmt, problemID).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}
