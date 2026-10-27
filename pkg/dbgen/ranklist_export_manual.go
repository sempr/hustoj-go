// 排行榜 CSV 导出查询（admin/ranklist_export.php）。
//
// 与 ranklist.php 的榜单不同，导出页：
//   - 只取 user_id/nick/solved/submit 四列；
//   - prefix 分支的 WHERE 只有 `user_id LIKE ?`（**不含** hidden / defunct），
//     非 prefix 分支才是 `user_id NOT IN (hidden) AND defunct='N'`；
//   - 非 scope 分页 5000/页；scope 分支把窗口内解题榜 LIMIT 到 50 行（硬编码）。
// 因为 where 子句随参数变化、scope 用 in_date 窗口（而非 solution_id 区间），
// 不能复用 ranklist.sql.go 的 ListRanklist*，这里手写。

package dbgen

import (
	"context"
	"database/sql"
)

// RanklistExportRow 是导出的一行。
type RanklistExportRow struct {
	UserID string
	Nick   string
	Solved sql.NullInt32
	Submit sql.NullInt32
}

// RanklistExportFilter 是非 scope 模式的筛选条件。
type RanklistExportFilter struct {
	// PrefixSet 对应 isset($_GET['prefix'])：空串也算设置。
	PrefixSet bool
	Prefix    string
	// Hidden 对应 $OJ_RANK_HIDDEN（仅在非 prefix 分支生效）。
	Hidden []string
}

// ExportRanklistUsers 取非 scope 模式的一页（ranklist_export.php 的非 scope 分支）。
func (q *Queries) ExportRanklistUsers(ctx context.Context, f RanklistExportFilter, offset, limit int32) ([]RanklistExportRow, error) {
	var where string
	var args []any
	if f.PrefixSet {
		where = "user_id LIKE ?"
		args = append(args, f.Prefix+"%")
	} else {
		notIn, hidArgs := hiddenNotIn("user_id", f.Hidden)
		if notIn != "" {
			where = notIn + " AND defunct = 'N'"
			args = append(args, hidArgs...)
		} else {
			where = "defunct = 'N'"
		}
	}
	stmt := `SELECT user_id, nick, solved, submit FROM users WHERE ` + where +
		` ORDER BY solved DESC, submit, reg_time LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := q.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	return scanRanklistExport(rows)
}

// ExportRanklistScoped 取 scope 模式（ranklist_export.php 的 scope 分支）。
//
// since 是窗口起点日期（'%Y-%m-%d'）。offset=rank、limit=5000 用于内层解题榜，
// 外层再 LIMIT 0,50 —— 两处数字都照抄 PHP。
func (q *Queries) ExportRanklistScoped(ctx context.Context, since string, offset, limit int32) ([]RanklistExportRow, error) {
	const stmt = `SELECT users.user_id, nick, s.solved, t.submit FROM users
INNER JOIN (
  SELECT count(DISTINCT problem_id) solved, user_id FROM solution
  WHERE in_date > STR_TO_DATE(?, '%Y-%m-%d') AND result = 4
  GROUP BY user_id ORDER BY solved DESC LIMIT ? OFFSET ?
) s ON users.user_id = s.user_id
INNER JOIN (
  SELECT count(problem_id) submit, user_id FROM solution
  WHERE in_date > STR_TO_DATE(?, '%Y-%m-%d')
  GROUP BY user_id ORDER BY submit DESC
) t ON users.user_id = t.user_id
ORDER BY s.solved DESC, t.submit, reg_time LIMIT 0, 50`

	rows, err := q.db.QueryContext(ctx, stmt, since, limit, offset, since)
	if err != nil {
		return nil, err
	}
	return scanRanklistExport(rows)
}

func scanRanklistExport(rows *sql.Rows) ([]RanklistExportRow, error) {
	defer rows.Close()
	var out []RanklistExportRow
	for rows.Next() {
		var r RanklistExportRow
		if err := rows.Scan(&r.UserID, &r.Nick, &r.Solved, &r.Submit); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
