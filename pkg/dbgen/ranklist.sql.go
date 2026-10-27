// 全局排名页（ranklist.php）的查询集合。
//
// 与 userinfo.sql.go 一样，这批查询需要按运行期参数拼一段可变长度的
// `NOT IN (...)`（$OJ_RANK_HIDDEN），以及 scope 模式下随周期变化的
// solution_id 区间，因此不能用 sqlc 静态生成，手写并保持同一套命名风格。
//
// 对拍基准是 phpweb/ranklist.php 与 template/syzoj/ranklist.php。
// 注释里标了 PHP 行号。

package dbgen

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// RanklistUserRow 是全局榜单里的一行。
//
// solved / submit 在 users 表里可空，沿用 sql.NullInt32 贴合列定义；
// nick / group_name 是 NOT NULL DEFAULT ”，用 string。starred 是 ent。
type RanklistUserRow struct {
	UserID    string
	Nick      string
	Solved    sql.NullInt32
	Submit    sql.NullInt32
	GroupName string
	Starred   int32
}

// hiddenNotIn 构造 `<column> NOT IN (?,?,...)` 与对应实参。
//
// hidden 为空时返回空串：PHP 的 $OJ_RANK_HIDDEN 恒非空（默认 "'admin','zhblue'"），
// 这里只是防御，避免拼出 `NOT IN ()` 这种非法 SQL。
func hiddenNotIn(column string, hidden []string) (string, []any) {
	if len(hidden) == 0 {
		return "", nil
	}
	ph := make([]string, len(hidden))
	args := make([]any, len(hidden))
	for i, h := range hidden {
		ph[i] = "?"
		args[i] = h
	}
	return column + " NOT IN (" + strings.Join(ph, ",") + ")", args
}

// CountRunningNOIPContests 复刻 ranklist.php:10 的 NOIP 拦截计数。
//
// PHP 把 $OJ_NOIP_KEYWORD 直接插进 `title like '%...%'`，这里改成绑定参数；
// % 与 _ 在两端都当通配符，语义一致。now 是 PHP 的 date('Y-m-d H:i')
// （进程时区，分钟精度），比较的是 datetime 列。
func (q *Queries) CountRunningNOIPContests(ctx context.Context, now string, keyword string) (int64, error) {
	const stmt = `SELECT count(contest_id) FROM contest
WHERE start_time < ? AND end_time > ? AND (title LIKE ? OR (contest_type & 20) > 0)`
	var n int64
	err := q.db.QueryRowContext(ctx, stmt, now, now, "%"+keyword+"%").Scan(&n)
	return n, err
}

// CountRanklistUsers 是**非 scope 模式**的分页总数（ranklist.php:43-46）。
//
// 注意这是 defunct='N' 的**全部**账号数：既不减 $OJ_RANK_HIDDEN，也不受
// prefix/group 过滤影响 —— PHP 就是这么算分页链接的，照做。
func (q *Queries) CountRanklistUsers(ctx context.Context) (int64, error) {
	const stmt = `SELECT count(1) FROM users WHERE defunct = 'N'`
	var n int64
	err := q.db.QueryRowContext(ctx, stmt).Scan(&n)
	return n, err
}

// ListRanklistUsers 取**非 scope 模式**的一页（ranklist.php:31-35、:60）。
//
// PHP 的 where 子句顺序是：prefix（若设置）→ hidden → defunct → group。
// LIMIT $rank, $page_size 即 offset=rank、limit=page_size。
func (q *Queries) ListRanklistUsers(ctx context.Context, f RanklistFilter, offset, limit int32) ([]RanklistUserRow, error) {
	var where []string
	var args []any
	if f.PrefixSet {
		where = append(where, "user_id LIKE ?")
		args = append(args, f.Prefix+"%")
	}
	if h, ha := hiddenNotIn("user_id", f.Hidden); h != "" {
		where = append(where, h)
		args = append(args, ha...)
	}
	where = append(where, "defunct = 'N'")
	if f.GroupName != "" {
		where = append(where, "group_name LIKE ?")
		args = append(args, f.GroupName+"%")
	}

	stmt := `SELECT user_id, nick, solved, submit, group_name, starred FROM users
WHERE ` + strings.Join(where, " AND ") + `
ORDER BY solved DESC, submit, reg_time LIMIT ? OFFSET ?`
	args = append(args, limit, offset)

	rows, err := q.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	return scanRanklistUsers(rows)
}

// RanklistFilter 是非 scope 模式的筛选条件，字段与 PHP 的
// $where / prefix / group_name 一一对应。
type RanklistFilter struct {
	// PrefixSet 对应 isset($_GET['prefix'])：**空串也算设置了**，
	// 此时 WHERE 里会出现 `user_id LIKE '%'`，与不设置时的结果集相同。
	PrefixSet bool
	Prefix    string
	// GroupName 非空时才加 `group_name LIKE '...%'`（PHP 的 !empty 判断）。
	GroupName string
	// Hidden 对应 $OJ_RANK_HIDDEN。
	Hidden []string
}

// RanklistScope 描述按时间窗口排行（scope=d/w/m/y）的 solution_id 区间。
//
// Pre 为真时是"上一周期"：preID < solution_id <= lastID；
// 否则是 solution_id > lastID。区间是半开/闭开由 PHP 原文决定（:81、:83）。
type RanklistScope struct {
	Pre    bool
	LastID int64
	PreID  int64
}

// clause 返回区间条件与实参。
func (s RanklistScope) clause() (string, []any) {
	if s.Pre {
		return "solution_id > ? AND solution_id <= ?", []any{s.PreID, s.LastID}
	}
	return "solution_id > ?", []any{s.LastID}
}

// LastSolutionIDBefore 取某日期之前最后一条提交的 solution_id（ranklist.php:69）。
//
// in_date < str_to_date(date,'%Y-%m-%d') 即"该日 00:00 之前"。
// 无匹配行时返回 0，与 PHP 把空结果置 0 一致。
func (q *Queries) LastSolutionIDBefore(ctx context.Context, date string) (int64, error) {
	const stmt = `SELECT solution_id FROM solution
WHERE in_date < STR_TO_DATE(?, '%Y-%m-%d') ORDER BY solution_id DESC LIMIT 1`
	var id int64
	err := q.db.QueryRowContext(ctx, stmt, date).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// CountRanklistScopedUsers 是 scope 模式的分页总数（ranklist.php:85）。
//
// 统计的是区间内出现过的**不同 user_id**，不减隐藏账号、不要求 result=4 ——
// 与数据查询的口径不同，分页条会比表里行数大，这是 PHP 的既有表现。
func (q *Queries) CountRanklistScopedUsers(ctx context.Context, sc RanklistScope) (int64, error) {
	cond, args := sc.clause()
	const stmt = `SELECT count(DISTINCT user_id) FROM solution WHERE `
	var n int64
	err := q.db.QueryRowContext(ctx, stmt+cond, args...).Scan(&n)
	return n, err
}

// ListRanklistScopedUsers 取 scope 模式的一页（ranklist.php:86-99）。
//
// 这条 SQL 的结构必须逐字复刻，两处不对称是有意的：
//   - 内层 s 只数 `result=4 AND first_time=1` 的题，并已排除隐藏账号；
//     它带 `LIMIT offset, page_size`，即**按解题数分页**。
//   - 内层 t 数区间内全部提交，且**不**排除隐藏账号、**不**过滤 problem_id；
//     隐藏与 defunct 的排除放在 JOIN 的 ON 里，而不是 t 的 WHERE。
//
// 参数顺序按 SQL 文本出现顺序：s(cond, hidden) → s limit/offset →
// t(cond) → ON(hidden)。
func (q *Queries) ListRanklistScopedUsers(ctx context.Context, sc RanklistScope, hidden []string, offset, limit int32) ([]RanklistUserRow, error) {
	cond, condArgs := sc.clause()

	sHidden, sHiddenArgs := hiddenNotIn("user_id", hidden)
	tHidden, tHiddenArgs := hiddenNotIn("users.user_id", hidden)

	sWhere := cond
	if sHidden != "" {
		sWhere += " AND " + sHidden
	}
	sWhere += " AND problem_id > 0 AND result = 4 AND first_time = 1"

	stmt := `SELECT users.user_id, nick, s.solved, t.submit, group_name, starred
FROM users
INNER JOIN (
  SELECT count(DISTINCT problem_id) AS solved, user_id FROM solution
  WHERE ` + sWhere + `
  GROUP BY user_id ORDER BY solved DESC LIMIT ? OFFSET ?
) s ON users.user_id = s.user_id
INNER JOIN (
  SELECT count(problem_id) AS submit, user_id FROM solution
  WHERE ` + cond + `
  GROUP BY user_id ORDER BY submit DESC
) t ON users.user_id = t.user_id`
	if tHidden != "" {
		stmt += "\n  AND " + tHidden
	}
	stmt += "\n  AND defunct = 'N'\nORDER BY s.solved DESC, t.submit, reg_time LIMIT 0, 50"

	args := make([]any, 0, len(condArgs)*2+len(sHiddenArgs)+len(tHiddenArgs)+2)
	args = append(args, condArgs...)
	args = append(args, sHiddenArgs...)
	args = append(args, limit, offset)
	args = append(args, condArgs...)
	args = append(args, tHiddenArgs...)

	rows, err := q.db.QueryContext(ctx, stmt, args...)
	if err != nil {
		return nil, err
	}
	return scanRanklistUsers(rows)
}

// scanRanklistUsers 扫描统一的榜单列。
func scanRanklistUsers(rows *sql.Rows) ([]RanklistUserRow, error) {
	defer rows.Close()
	var out []RanklistUserRow
	for rows.Next() {
		var r RanklistUserRow
		if err := rows.Scan(&r.UserID, &r.Nick, &r.Solved, &r.Submit, &r.GroupName, &r.Starred); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
