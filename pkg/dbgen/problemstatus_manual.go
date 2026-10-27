// 本文件不由 sqlc 生成。
//
// problemstatus.php:158-170 的"每个用户最优 AC 提交"查询用了派生表 JOIN，
// 当前 sqlc 的 MySQL 解析器无法处理（会报 column reference ambiguous），
// 因此按 PHP 原文手写，保持与上游逐字一致。
package dbgen

import (
	"context"
	"time"
)

// ProblemBestACSolRow 是每个用户的最优 AC 提交。
type ProblemBestACSolRow struct {
	// Att 是该用户在该题上的 AC 提交总数，展示为 "solution_id(att)"。
	Att        int64     `json:"att"`
	UserID     string    `json:"user_id"`
	SolutionID int32     `json:"solution_id"`
	Language   uint32    `json:"language"`
	Time       int32     `json:"time"`
	Memory     int32     `json:"memory"`
	CodeLength int32     `json:"code_length"`
	InDate     time.Time `json:"in_date"`
}

// ListProblemBestACSolutions 对应 problemstatus.php:158-170。
//
// 分数 = 1e19 + time*1e11 + memory*1e5 + code_length，取每题每用户的最小值；
// 外层按分数升序、solution_id 升序分页。PHP 用 `LIMIT $start, $sz`，
// 这里保持同样的 offset, limit 顺序。
func (q *Queries) ListProblemBestACSolutions(ctx context.Context, problemID int32, offset, limit int32) ([]ProblemBestACSolRow, error) {
	const stmt = `SELECT c.att, b.user_id, b.solution_id, b.language,
       b.time, b.memory, b.code_length, b.in_date
FROM (
  SELECT COUNT(*) AS att, user_id,
         MIN(10000000000000000000 + time*100000000000 + memory*100000 + code_length) AS score
  FROM solution
  WHERE problem_id = ? AND result = 4
  GROUP BY user_id
) c
INNER JOIN (
  SELECT solution_id, user_id, language, time, memory, code_length, in_date,
         10000000000000000000 + time*100000000000 + memory*100000 + code_length AS score
  FROM solution
  WHERE problem_id = ? AND result = 4
) b ON b.user_id = c.user_id AND b.score = c.score
ORDER BY c.score, b.solution_id ASC
LIMIT ?, ?`
	rows, err := q.db.QueryContext(ctx, stmt, problemID, problemID, offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProblemBestACSolRow
	for rows.Next() {
		var i ProblemBestACSolRow
		if err := rows.Scan(
			&i.Att,
			&i.UserID,
			&i.SolutionID,
			&i.Language,
			&i.Time,
			&i.Memory,
			&i.CodeLength,
			&i.InDate,
		); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
