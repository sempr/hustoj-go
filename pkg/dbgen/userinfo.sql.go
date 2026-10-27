// userinfo 档案页的查询集合（对应 phpweb/userinfo.php）。
//
// 与其他 *.sql.go 的区别：这批查询的 SQL 需要按运行期参数拼一段
// `AND contest_id NOT IN (...)` —— PHP 侧对应 $not_in_noip，
// 用来剔除正在进行的 NOIP 比赛，避免赛中泄题。
// PHP 把这段直接拼进 SQL（值来自自家查询的 contest_id），
// 这里改成占位符，个数随排除比赛数变化，因此这些方法不能用
// sqlc 的静态代码生成，手写并保持与其余查询同一套命名与注释风格。

package dbgen

import (
	"context"
	"database/sql"
	"strings"
)

// excludeContestClause 构造 `AND contest_id NOT IN (?,?,...)` 与对应实参。
//
// 空列表返回空串，此时 SQL 不带任何排除条件 —— 这与 PHP 的
// `if (!empty($exceptions))` 判断一致（userinfo.php:98-102）。
func excludeContestClause(ids []int32) (string, []any) {
	if len(ids) == 0 {
		return "", nil
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args[i] = id
	}
	return " AND contest_id NOT IN (" + strings.Join(ph, ",") + ")", args
}

// ListActiveNOIPContests 返回需要从统计中剔除的进行中比赛。
//
// 对应 userinfo.php:92。这批比赛正处 NOIP 模式：赛中所有人的 AC 数与
// 提交数都不计入档案统计，否则学生能通过反复刷档案页反推他人的进度。
//
// 第三段 `((contest_type & 20) > 0 and end_time > now())` 是 PHP 原文里
// 少写 c. 前缀的那一支：它不带 start_time/end_time 的双重约束，
// 也就是说任何标记了"考试中"位且尚未结束的比赛都会被排除，
// 包括还没开始的那场。这里原样保留 —— 改窄会让赛前就漏出答案。
func (q *Queries) ListActiveNOIPContests(ctx context.Context, now string, keyword string) ([]int32, error) {
	const stmt = `SELECT contest_id FROM contest c
WHERE c.start_time < ? AND c.end_time > ?
  AND ( c.title LIKE CONCAT('%', ?, '%') OR ((c.contest_type & 20) > 0 AND c.end_time > NOW()) )`
	rows, err := q.db.QueryContext(ctx, stmt, now, now, keyword)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int32
	for rows.Next() {
		var id int32
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// GetUserProfile 按 user_id 取 users 行。
//
// 对应 userinfo.php:105-108。PHP 对管理员查全表、对其他人附加
// `and user_id not in ('admin','zhblue')`；这里一律取行，
// 由调用方按身份决定是否判定为"No such User!" ——
// 见 handler_userinfo.go 里 rankHidden 的用法，两种分支的结果集一致。
func (q *Queries) GetUserProfile(ctx context.Context, userID string) (User, error) {
	const stmt = `SELECT user_id, email, submit, solved, defunct, ip, accesstime, volume,
       language, password, reg_time, expiry_date, nick, school, parent_phone,
       group_name, activecode, starred, coin_earned, coin_bonus, coin_spent
FROM users WHERE user_id = ?`
	var u User
	err := q.db.QueryRowContext(ctx, stmt, userID).Scan(
		&u.UserID, &u.Email, &u.Submit, &u.Solved, &u.Defunct, &u.Ip, &u.Accesstime,
		&u.Volume, &u.Language, &u.Password, &u.RegTime, &u.ExpiryDate, &u.Nick,
		&u.School, &u.ParentPhone, &u.GroupName, &u.Activecode, &u.Starred,
		&u.CoinEarned, &u.CoinBonus, &u.CoinSpent)
	return u, err
}

// CountUserACProblems 统计通过题数（去重）。
//
// 对应 userinfo.php:130。注意是 count(DISTINCT problem_id) 而不是
// count(1)：重复提交同一题只算一题，这个语义直接决定用户看到的
// 「通过 N 题」和由此推出的等级。
func (q *Queries) CountUserACProblems(ctx context.Context, userID string, exclude []int32) (int64, error) {
	clause, args := excludeContestClause(exclude)
	stmt := `SELECT count(DISTINCT problem_id) FROM solution
WHERE user_id = ? AND result = 4 AND problem_id > 0` + clause
	all := append([]any{userID}, args...)
	var n int64
	err := q.db.QueryRowContext(ctx, stmt, all...).Scan(&n)
	return n, err
}

// CountUserSubmittedProblems 统计提交过的题目数（去重，不分结果）。
//
// 对应 userinfo.php:136。同样是 DISTINCT：它写的是 users.submit 字段，
// 榜单之外还会参与 Rank 的反查，所以口径必须与 PHP 一致。
func (q *Queries) CountUserSubmittedProblems(ctx context.Context, userID string, exclude []int32) (int64, error) {
	clause, args := excludeContestClause(exclude)
	stmt := `SELECT count(DISTINCT problem_id) FROM solution
WHERE user_id = ? AND problem_id > 0` + clause
	all := append([]any{userID}, args...)
	var n int64
	err := q.db.QueryRowContext(ctx, stmt, all...).Scan(&n)
	return n, err
}

// UpdateUserSolvedSubmit 把刚算出的两个计数写回 users。
//
// 对应 userinfo.php:142-143。这是一个 GET 请求里的写操作 —— PHP 确实
// 这么做，所以每次有人打开档案页都会重算并落库。Go 侧保留该行为：
// users.solved 是榜单排序依据，若只在提交时更新，管理员手工改数据或
// NOIP 排除比赛结束之后，档案页算出的 AC 数与榜单就会长期不一致。
func (q *Queries) UpdateUserSolvedSubmit(ctx context.Context, solved, submit int32, userID string) error {
	const stmt = `UPDATE users SET solved = ?, submit = ? WHERE user_id = ?`
	_, err := q.db.ExecContext(ctx, stmt, solved, submit, userID)
	return err
}

// CountUsersAhead 数出 solved 严格大于给定值的未 defunct 账号数。
//
// 对应 userinfo.php:144-147，Rank = 该计数 + 1。
// excludeHidden 对应 $OJ_RANK_HIDDEN，被隐藏的账号不参与名次计算。
func (q *Queries) CountUsersAhead(ctx context.Context, solved int32, excludeHidden []string) (int64, error) {
	clause := ""
	var args []any = []any{solved}
	if len(excludeHidden) > 0 {
		ph := make([]string, len(excludeHidden))
		for i, u := range excludeHidden {
			ph[i] = "?"
			args = append(args, u)
		}
		clause = " AND user_id NOT IN (" + strings.Join(ph, ",") + ")"
	}
	stmt := `SELECT count(*) FROM users WHERE solved > ? AND defunct = 'N'` + clause
	var n int64
	err := q.db.QueryRowContext(ctx, stmt, args...).Scan(&n)
	return n, err
}

// RecalcUserCoinEarned 按"首杀题"重算 coin_earned。
//
// 对应 userinfo.php:150-151。口径是 result=4 且 first_time=1 的题目的
// coin 之和，也就是只有"第一个做出来的人"能拿到题目的金币。
// PHP 每次打开档案页都重算一遍，用途是自愈：题目的 coin 被管理员改过、
// 或题目被下架后，这里的数字都会自动回到一致状态。
func (q *Queries) RecalcUserCoinEarned(ctx context.Context, userID string) error {
	const stmt = `UPDATE users SET coin_earned = COALESCE((
  SELECT SUM(p.coin) FROM problem p
  WHERE p.problem_id IN (
    SELECT DISTINCT problem_id FROM solution
    WHERE user_id = users.user_id AND result = 4 AND first_time = 1 AND problem_id > 0
  )
), 0) WHERE user_id = ?`
	_, err := q.db.ExecContext(ctx, stmt, userID)
	return err
}

// UserLoginLogRow 是 loginlog 的一行。Password 只在管理员视图里出现，
// 见 ListUserLoginLog 的说明。
type UserLoginLogRow struct {
	UserID   string
	Password sql.NullString
	IP       sql.NullString
	Time     sql.NullTime
}

// ListUserLoginLog 取最近 50 条登录日志。
//
// 对应 userinfo.php:162，**仅管理员可调用**，且模板里的表头就是
// UserID/Password/IP/Time —— 即管理员能看到历史明文口令。
// 这是 PHP 的既有行为，本次迁移原样保留；调用方必须已经完成
// 管理员判定，切勿在非管理员路径上调用（见 handler_userinfo.go）。
func (q *Queries) ListUserLoginLog(ctx context.Context, userID string) ([]UserLoginLogRow, error) {
	const stmt = `SELECT user_id, password, ip, time FROM loginlog
WHERE user_id = ? ORDER BY time DESC LIMIT 0, 50`
	rows, err := q.db.QueryContext(ctx, stmt, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserLoginLogRow
	for rows.Next() {
		var r UserLoginLogRow
		if err := rows.Scan(&r.UserID, &r.Password, &r.IP, &r.Time); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UserResultStatRow 是"某结果码的提交次数"，用于饼图。
type UserResultStatRow struct {
	Result int32
	Count  int64
}

// ListUserResultStats 统计 result>=4 的结果分布。
//
// 对应 userinfo.php:165。>=4 是"已判完"的下界，所以饼图只统计有结论的
// 提交，等待/运行中的不计入。
func (q *Queries) ListUserResultStats(ctx context.Context, userID string, exclude []int32) ([]UserResultStatRow, error) {
	clause, args := excludeContestClause(exclude)
	stmt := `SELECT result, count(1) FROM solution
WHERE user_id = ? AND result >= 4` + clause + ` GROUP BY result ORDER BY result`
	all := append([]any{userID}, args...)
	rows, err := q.db.QueryContext(ctx, stmt, all...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserResultStatRow
	for rows.Next() {
		var r UserResultStatRow
		if err := rows.Scan(&r.Result, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UserContestStatRow 是"某场比赛里该用户做了多少题、AC 多少题"。
type UserContestStatRow struct {
	ContestID int32
	Title     sql.NullString
	StartTime sql.NullTime
	Tried     int64
	AC        int64
}

// ListUserContestStats 列出该用户参加过的比赛及战绩。
//
// 对应模板 userinfo.php:114-117。子查询只统计 contest_id>0 的提交
// （即正式比赛，练习提交不算），再 join contest 拿标题与开始时间。
// `not (title like '%noip%' or ...)` 与 PHP 原文一致：NOIP 比赛的战绩
// 在赛中对所有人隐藏。
//
// start_time 原样返回，由模板决定是否显示。
func (q *Queries) ListUserContestStats(ctx context.Context, userID string, keyword string) ([]UserContestStatRow, error) {
	const stmt = `SELECT c.contest_id, c.title, c.start_time, rt.tried, rt.ac
FROM contest c
INNER JOIN (
  SELECT contest_id,
         count(DISTINCT problem_id) AS tried,
         count(DISTINCT IF(result = 4, problem_id, NULL)) AS ac
  FROM solution
  WHERE user_id = ? AND contest_id > 0
  GROUP BY contest_id ORDER BY contest_id
) rt ON c.contest_id = rt.contest_id
 AND NOT ( c.title LIKE CONCAT('%', ?, '%')
           OR ((c.contest_type & 20) > 0 AND c.end_time > NOW()) )`
	rows, err := q.db.QueryContext(ctx, stmt, userID, keyword)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserContestStatRow
	for rows.Next() {
		var r UserContestStatRow
		if err := rows.Scan(&r.ContestID, &r.Title, &r.StartTime, &r.Tried, &r.AC); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UserProblemCountRow 是"某题的提交次数"。
type UserProblemCountRow struct {
	ProblemID int32
	Count     int64
}

// ListUserACProblems 列出已通过的题及提交次数。
//
// 对应模板 userinfo.php:184，题号升序 —— 模板靠这个顺序渲染一排色块标签。
func (q *Queries) ListUserACProblems(ctx context.Context, userID string, exclude []int32) ([]UserProblemCountRow, error) {
	clause, args := excludeContestClause(exclude)
	stmt := `SELECT problem_id, count(1) FROM solution
WHERE user_id = ? AND result = 4 AND problem_id != 0` + clause + `
GROUP BY problem_id ORDER BY problem_id ASC`
	all := append([]any{userID}, args...)
	rows, err := q.db.QueryContext(ctx, stmt, all...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserProblemCountRow
	for rows.Next() {
		var r UserProblemCountRow
		if err := rows.Scan(&r.ProblemID, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListUserNonACProblems 列出未通过的题及提交次数。
//
// 对应模板 userinfo.php:212。与已通过列表分开查，模板再用 $ac 集合
// 做差集去重 —— 同一题既有 AC 又有 WA 时只应出现在"通过"区，
// 这里保留 PHP 的原始形状（可能带重复），由调用方去重。
func (q *Queries) ListUserNonACProblems(ctx context.Context, userID string, exclude []int32) ([]UserProblemCountRow, error) {
	clause, args := excludeContestClause(exclude)
	stmt := `SELECT sol.problem_id, count(1) FROM solution sol
WHERE sol.user_id = ? AND sol.result != 4 AND sol.problem_id != 0` + clause + `
GROUP BY sol.problem_id ORDER BY sol.problem_id ASC`
	all := append([]any{userID}, args...)
	rows, err := q.db.QueryContext(ctx, stmt, all...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserProblemCountRow
	for rows.Next() {
		var r UserProblemCountRow
		if err := rows.Scan(&r.ProblemID, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UserSubChartRow 是"某一天的提交次数"，用于日历热力图。
type UserSubChartRow struct {
	Date  string
	Count int64
}

// ListUserSubChart 取最近一年的每日提交数。
//
// 对应模板 userinfo.php:366。两处限制必须一起看：
//   - in_date >= DATE_SUB(CURDATE(), INTERVAL 1 YEAR)：热力图只画一年，
//     更早的数据在图上根本不可见，取出来只是浪费。
//   - result < 13：把"受限函数(12)"和"非零退出(13)"这类终态排除，
//     统计的是"正常的提交尝试"而非所有行。PHP 原文是 < 13，
//     即 13 不含在内，与 Go 的 < 严格对应。
func (q *Queries) ListUserSubChart(ctx context.Context, userID string, exclude []int32) ([]UserSubChartRow, error) {
	clause, args := excludeContestClause(exclude)
	stmt := `SELECT DATE(in_date) AS d, count(*) FROM solution
WHERE user_id = ? AND in_date >= DATE_SUB(CURDATE(), INTERVAL 1 YEAR) AND result < 13` + clause + `
GROUP BY DATE(in_date)`
	all := append([]any{userID}, args...)
	rows, err := q.db.QueryContext(ctx, stmt, all...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserSubChartRow
	for rows.Next() {
		var r UserSubChartRow
		if err := rows.Scan(&r.Date, &r.Count); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListViewerACProblems 取访客已通过的题号集合。
//
// 对应 userinfo.php:194。用途是给色块上色：访客自己过的题显示绿色，
// 别人看同一个人时全部显示红色。注意查的是**访客**的记录，
// 不是被查看者的 —— 模板里据此决定 label 的 class。
func (q *Queries) ListViewerACProblems(ctx context.Context, viewerID string, exclude []int32) ([]int32, error) {
	clause, args := excludeContestClause(exclude)
	stmt := `SELECT DISTINCT problem_id FROM solution
WHERE user_id = ? AND result = 4` + clause
	all := append([]any{viewerID}, args...)
	rows, err := q.db.QueryContext(ctx, stmt, all...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int32
	for rows.Next() {
		var id int32
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// CountActiveContests 统计未关闭的比赛数，用于列表分页。
//
// 对应 contest.php:149。
func (q *Queries) CountActiveContests(ctx context.Context) (int64, error) {
	const stmt = `SELECT count(1) FROM contest WHERE defunct = 'N'`
	var n int64
	err := q.db.QueryRowContext(ctx, stmt).Scan(&n)
	return n, err
}

// ContestSetFilter 是比赛列表的筛选条件，字段与 PHP 的
// $keyword / $wheremy 一一对应。
type ContestSetFilter struct {
	// Keyword 非空时按标题模糊匹配（PHP 里是 POST 提交的 %keyword%）。
	Keyword string
	// MyContests 非空时只显示这些 cid，以及 UserID 名下的比赛
	// （PHP: `and( contest_id in (...) or user_id='<当前用户>' )`）。
	MyContests []int32
	UserID     string
}

// sql 拼出带占位符的筛选条件与实参。
func (f ContestSetFilter) sql() (string, []any) {
	var where []string
	var args []any
	if f.Keyword != "" {
		where = append(where, "title LIKE ?")
		args = append(args, "%"+f.Keyword+"%")
	}
	if len(f.MyContests) > 0 {
		ph := make([]string, len(f.MyContests))
		for i, cid := range f.MyContests {
			ph[i] = "?"
			args = append(args, cid)
		}
		where = append(where, "( contest_id IN ("+strings.Join(ph, ",")+") OR user_id = ? )")
		args = append(args, f.UserID)
	}
	if len(where) == 0 {
		return "", nil
	}
	return " AND " + strings.Join(where, " AND "), args
}

// ContestSetRow 是比赛列表的一行（对应 contest.php 的 $view_contest）。
type ContestSetRow struct {
	ContestID   int32
	Title       sql.NullString
	StartTime   sql.NullTime
	EndTime     sql.NullTime
	Private     int8
	ContestType sql.NullInt16
	UserID      string
}

// ListContestSet 取比赛列表的一页。
//
// 对应 contest.php:218-229，排序恒为 contest_id DESC（新的在前）。
// 关键词与"我的比赛"两个条件都是 AND 拼接，与 PHP 的
// `WHERE defunct='N' AND title LIKE ? $wheremy` 顺序一致。
func (q *Queries) ListContestSet(ctx context.Context, f ContestSetFilter, limit, offset int32) ([]ContestSetRow, error) {
	clause, args := f.sql()
	stmt := `SELECT contest_id, title, start_time, end_time, private, contest_type, user_id
FROM contest WHERE defunct = 'N'` + clause + ` ORDER BY contest_id DESC LIMIT ? OFFSET ?`
	all := append(args, limit, offset)
	rows, err := q.db.QueryContext(ctx, stmt, all...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ContestSetRow
	for rows.Next() {
		var r ContestSetRow
		if err := rows.Scan(&r.ContestID, &r.Title, &r.StartTime, &r.EndTime,
			&r.Private, &r.ContestType, &r.UserID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListMyContestIDs 列出该用户参加过的比赛（contest_id>0）。
//
// 对应 contest.php:170，"我的比赛"的第一路来源。
func (q *Queries) ListMyContestIDs(ctx context.Context, userID string) ([]int32, error) {
	const stmt = `SELECT DISTINCT contest_id FROM solution WHERE contest_id > 0 AND user_id = ?`
	rows, err := q.db.QueryContext(ctx, stmt, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int32
	for rows.Next() {
		var id int32
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListVIPContestIDs 列出标题带 [VIP] 标记的比赛。
//
// 对应 contest.php:195，VIP 用户可见这些比赛。
func (q *Queries) ListVIPContestIDs(ctx context.Context) ([]int32, error) {
	const stmt = `SELECT contest_id FROM contest WHERE title LIKE '%[VIP]%'`
	rows, err := q.db.QueryContext(ctx, stmt)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int32
	for rows.Next() {
		var id int32
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ContestProblemACCounts 是某用户在比赛某题上的三个提交计数。
type ContestProblemACCounts struct {
	// SubTotal 是提交总数（problem_id!=0），NOIP 模式下用来判断"是否交过"。
	SubTotal int64
	// ACCount 是 result=4 的次数。
	ACCount int64
	// BadCount 是 result!=4 且 problem_id!=0 的次数。
	BadCount int64
}

// CountUserContestProblemAC 一次取回 check_ac() 需要的三个计数。
//
// 对应 my_func.inc.php:616 的 check_ac。PHP 分三条 SQL 查三次：
// NOIP 模式数提交总数，正常模式先数 AC 再数非 AC。
// 这里合并成一条 —— 三次往返在比赛期间（几十道题 × 每人一页）
// 是实打实的延迟，且 PHP 那三条的结果互不依赖，合并无副作用。
//
// 三个计数的过滤条件各不相同，合并时必须逐条对齐：
//   - SubTotal 带 problem_id!=0，练习提交（problem_id=0）不算交过
//   - ACCount **不带** problem_id 过滤：PHP 的 SQL 原文如此，
//     所以"练了某题并且交进了比赛"也可能被算成 AC
//   - BadCount 同时要求 result!=4 与 problem_id!=0
func (q *Queries) CountUserContestProblemAC(ctx context.Context, userID string, cid, num int32) (ContestProblemACCounts, error) {
	const stmt = `SELECT
  coalesce(sum(problem_id != 0), 0)                 AS sub_total,
  coalesce(sum(result = 4), 0)                      AS ac_count,
  coalesce(sum(result != 4 AND problem_id != 0), 0) AS bad_count
FROM solution WHERE user_id = ? AND contest_id = ? AND num = ?`
	var c ContestProblemACCounts
	err := q.db.QueryRowContext(ctx, stmt, userID, cid, num).
		Scan(&c.SubTotal, &c.ACCount, &c.BadCount)
	return c, err
}

// ProblemUsedInUpcomingContest 报告某题是否被尚未开始的比赛占用。
//
// 对应 contest.php:99-105。比赛结束后，若这道题还排在某场未开始的
// 比赛里，PHP 会把题目标题换成 "--using in another private contest--"
// 对非本场管理员隐藏，防止赛前泄题。
func (q *Queries) ProblemUsedInUpcomingContest(ctx context.Context, problemID int32) (bool, error) {
	const stmt = `SELECT count(1) FROM problem p
WHERE p.problem_id = ? AND p.problem_id IN (
  SELECT cp.problem_id FROM contest_problem cp
  WHERE cp.contest_id IN (
    SELECT c.contest_id FROM contest c
    WHERE c.defunct = 'N' AND NOW() < c.start_time
  )
)`
	var n int64
	err := q.db.QueryRowContext(ctx, stmt, problemID).Scan(&n)
	return n > 0, err
}
