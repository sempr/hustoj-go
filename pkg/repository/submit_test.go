// 提交入库的事务契约测试。
//
// 这里只测一件事：提交要么完整落库，要么什么都不留。
//
// 理由是判题机按 `result<2` 取件。如果事务只写了一半，最坏情况不是
// "用户少看到一条记录"，而是判题机取到一条 result 已置 0、源码却还没
// 落库的记录，编译出空程序并给出一个误导性的 CE。用户在 status 上看到
// CE，去看自己的代码又是对的，这类问题能查很久。
//
// 所以测试不验证"成功路径写了什么"（那由 internal/web 的端到端用例覆盖），
// 只验证"中途失败时没有残留"。

package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// rollbackDB 打开测试库；未设置 HUSTOJ_EQUIV_DSN 时跳过。
func rollbackDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("HUSTOJ_EQUIV_DSN")
	if dsn == "" {
		t.Skip("未设置 HUSTOJ_EQUIV_DSN，跳过需要真实库的事务测试")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("连接失败: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("Ping 失败: %v", err)
	}
	return db
}

const (
	rollbackUser = "rollbackprobe"
	// rollbackProblem 是本文件专用的题目 id。
	//
	// 不复用真实题号：计数器用例要写 problem.submit，复用会污染影子库里
	// 的真实题目统计。900001 与 internal/web 的提交测试共用，靠
	// t.Cleanup 复位。
	rollbackProblem = 900001
)

// seedRollbackUser 造一个可提交的用户与题目。
func seedRollbackUser(t *testing.T, db *sql.DB) {
	t.Helper()
	ctx := context.Background()
	// 清理顺序要紧：先删依赖 solution 的子表，再删 solution 本身，
	// 否则会留下无主行 —— 而这些无主行会让后面"没有残留"的断言
	// 读到上一次运行留下的垃圾。
	for _, q := range []string{
		`DELETE FROM custominput WHERE solution_id IN (SELECT solution_id FROM solution WHERE user_id = ?)`,
		`DELETE FROM source_code WHERE solution_id IN (SELECT solution_id FROM solution WHERE user_id = ?)`,
		`DELETE FROM source_code_user WHERE solution_id IN (SELECT solution_id FROM solution WHERE user_id = ?)`,
		`DELETE FROM solution WHERE user_id = ?`,
		`DELETE FROM users WHERE user_id = ?`,
	} {
		if _, err := db.ExecContext(ctx, q, rollbackUser); err != nil {
			t.Fatalf("清理失败 (%s): %v", q, err)
		}
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (user_id, email, ip, accesstime, password, reg_time, nick, school, group_name, defunct)
		 VALUES (?, '', '0', NOW(), '', NOW(), '回滚探针', '', 'user', 'N')`, rollbackUser); err != nil {
		t.Fatalf("准备用户失败: %v", err)
	}
	// description/input/output/hint 在 production 表结构里是 NOT NULL
	// 且无默认值，缺一个就报 1364。
	if _, err := db.ExecContext(ctx,
		`INSERT INTO problem (problem_id, title, defunct, time_limit, memory_limit,
			description, input, output, hint)
		 VALUES (?, '回滚测试题', 'N', 1000, 65536, '', '', '', '')
		 ON DUPLICATE KEY UPDATE defunct = 'N'`, rollbackProblem); err != nil {
		t.Fatalf("准备题目失败: %v", err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE problem SET submit = 0, accepted = 0 WHERE problem_id = ?`, rollbackProblem); err != nil {
		t.Fatalf("复位题目计数失败: %v", err)
	}
}

// countRows 统计满足条件的行数。
func countRows(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	return n
}

// TestInsertSubmissionRollsBackOnSourceTooLong 验证源码写不进去时整体回滚。
//
// 用一个超过 TEXT 列容量的源码触发 1406 Data too long —— 这不是假设出来的
// 故障，而是真实存在过的：PHP 侧的长度检查放行 65536 字节，而列上限是
// 65535，INSERT 必然失败。
//
// 故障点选在写 source_code_user 这一步，也就是 solution 行已经 INSERT
// 之后。若没有事务（或事务范围写小了），库里就会留下一条 result=14 或
// result=0 的孤儿记录。
func TestInsertSubmissionRollsBackOnSourceTooLong(t *testing.T) {
	db := rollbackDB(t)
	seedRollbackUser(t, db)
	t.Cleanup(func() { seedRollbackUser(t, db) })

	// 用一个每次运行都不同的标记串来识别"本次尝试写的数据"。
	//
	// 不用"高水位 solution_id"或"全库无主行计数"这两种做法：影子库是
	// 从生产导入的，里面有 solution_id 高达 880006 的历史无主行
	//（solution 表里没有对应记录），任何按 id 划区间的断言都会被它
	// 永久干扰。按内容认领则只可能命中本次写的数据。
	marker := fmt.Sprintf("rollbackprobe-%d", time.Now().UnixNano())

	// source_code_user.source 是 TEXT，容量 65535 字节。
	// 造一个超过容量的源码，故障点必然落在写用户源码这一步。
	tooLong := marker + strings.Repeat("a", 70000)
	_, err := InsertSubmission(context.Background(), db, SubmitParams{
		ProblemID:  rollbackProblem,
		UserID:     rollbackUser,
		Nick:       "回滚探针",
		Language:   0,
		IP:         "127.0.0.1",
		CodeLength: int32(len(tooLong)),
		Source:     tooLong,
		SourceUser: tooLong,
	})
	if err == nil {
		t.Fatal("源码超出列容量时本应报错，却成功了 —— 说明列类型或 sql_mode 与预期不符")
	}
	if !strings.Contains(err.Error(), "用户源码") {
		t.Errorf("错误信息 = %q，期望指向 source_code_user 这一步", err)
	}

	if n := countRows(t, db, `SELECT count(1) FROM solution WHERE user_id = ?`, rollbackUser); n != 0 {
		t.Errorf("失败后 solution 仍有 %d 行 —— solution 行没有被回滚。\n"+
			"判题机会取到这条 result<2 但无源码的记录", n)
	}

	// 确认本次尝试的数据没有半张落库。
	for _, tc := range []struct{ table, why string }{
		{"source_code", "送判源码"},
		{"source_code_user", "用户源码"},
	} {
		if n := countRows(t, db,
			`SELECT count(1) FROM `+tc.table+` WHERE source LIKE ?`, "%"+marker+"%"); n != 0 {
			t.Errorf("%s 表里残留 %d 条本次尝试的记录（%s）", tc.table, n, tc.why)
		}
	}
}

// TestInsertSubmissionDoesNotBumpCountersOnFailure 验证失败不污染题目计数。
//
// problem.submit 是在同一个事务里 +1 的。若它逃出了事务，用户一次失败的
// 提交就会让题目的提交数虚高，而 status 页面的"提交次数"是从这张表读的。
func TestInsertSubmissionDoesNotBumpCountersOnFailure(t *testing.T) {
	db := rollbackDB(t)
	seedRollbackUser(t, db)
	t.Cleanup(func() { seedRollbackUser(t, db) })

	pid := int32(rollbackProblem)
	var before int
	if err := db.QueryRow(`SELECT submit FROM problem WHERE problem_id = ?`, pid).Scan(&before); err != nil {
		t.Fatalf("读取提交数失败: %v", err)
	}

	tooLong := strings.Repeat("a", 70000)
	if _, err := InsertSubmission(context.Background(), db, SubmitParams{
		ProblemID: pid, UserID: rollbackUser, Nick: "回滚探针",
		Language: 0, IP: "127.0.0.1", CodeLength: int32(len(tooLong)),
		Source: tooLong, SourceUser: tooLong,
	}); err == nil {
		t.Fatal("本应失败")
	}

	var after int
	if err := db.QueryRow(`SELECT submit FROM problem WHERE problem_id = ?`, pid).Scan(&after); err != nil {
		t.Fatalf("读取提交数失败: %v", err)
	}
	if after != before {
		t.Errorf("problem.submit = %d（提交前 %d），失败的提交不该影响计数", after, before)
	}
}

// TestInsertSubmissionWritesResultAndSourceAtomically 验证成功路径的一致性。
//
// 这是"回滚"的对偶：事务一提交，源码与 result 必须同时可见。
// 分两次查询之间不加锁，判题机同样可能在这个缝隙里取件。
func TestInsertSubmissionWritesResultAndSourceAtomically(t *testing.T) {
	db := rollbackDB(t)
	seedRollbackUser(t, db)
	t.Cleanup(func() { seedRollbackUser(t, db) })

	out, err := InsertSubmission(context.Background(), db, SubmitParams{
		ProblemID:  rollbackProblem,
		TestRun:    false,
		UserID:     rollbackUser,
		Nick:       "回滚探针",
		Language:   0,
		IP:         "127.0.0.1",
		CodeLength: 5,
		Source:     "int x;",
		SourceUser: "int x;",
	})
	if err != nil {
		t.Fatalf("提交失败: %v", err)
	}
	if out.FinalResult != 0 {
		t.Errorf("FinalResult = %d，期望 0（正常提交进判题队列）", out.FinalResult)
	}

	// 事务已提交：result 与源码此时必须都已可见。
	var result int
	if err := db.QueryRow(`SELECT result FROM solution WHERE solution_id = ?`, out.SolutionID).
		Scan(&result); err != nil {
		t.Fatalf("读取提交失败: %v", err)
	}
	if result != 0 {
		t.Errorf("result = %d，期望 0", result)
	}
	var src string
	if err := db.QueryRow(`SELECT source FROM source_code WHERE solution_id = ?`, out.SolutionID).
		Scan(&src); err != nil {
		t.Fatalf("送判源码缺失: %v", err)
	}
	if src != "int x;" {
		t.Errorf("送判源码 = %q", src)
	}
	if n := countRows(t, db, `SELECT count(1) FROM source_code_user WHERE solution_id = ?`, out.SolutionID); n != 1 {
		t.Errorf("source_code_user 行数 = %d，期望 1", n)
	}
}

// TestInsertSubmissionTestRunStaysPending 验证测试运行停在 14 且不计数。
//
// 判题机按 result<2 取件，测试运行一旦是 0 就会被取走；而 judged-go 不读
// custominput，取走也只是拿题目自带数据跑一遍，给用户一个假的"运行成功"。
func TestInsertSubmissionTestRunStaysPending(t *testing.T) {
	db := rollbackDB(t)
	seedRollbackUser(t, db)
	t.Cleanup(func() { seedRollbackUser(t, db) })

	pid := int32(rollbackProblem)
	var before int
	if err := db.QueryRow(`SELECT submit FROM problem WHERE problem_id = ?`, pid).Scan(&before); err != nil {
		t.Fatalf("读取提交数失败: %v", err)
	}

	out, err := InsertSubmission(context.Background(), db, SubmitParams{
		ProblemID: pid, TestRun: true, UserID: rollbackUser, Nick: "回滚探针",
		Language: 0, IP: "127.0.0.1", CodeLength: 5,
		Source: "int x;", SourceUser: "int x;", InputText: "1 2 3",
	})
	if err != nil {
		t.Fatalf("测试运行入库失败: %v", err)
	}
	if out.FinalResult != 14 {
		t.Errorf("FinalResult = %d，期望 14（OJ_MC，不进判题队列）", out.FinalResult)
	}
	if out.FinalResult < 2 {
		t.Errorf("FinalResult = %d < 2，判题机会取走测试运行", out.FinalResult)
	}

	var storedPID int32
	if err := db.QueryRow(`SELECT problem_id FROM solution WHERE solution_id = ?`, out.SolutionID).
		Scan(&storedPID); err != nil {
		t.Fatalf("读取 problem_id 失败: %v", err)
	}
	if storedPID != int32(pid) {
		t.Errorf("problem_id = %d，期望 %d（测试运行也写真实 id；存 0 会让判题机去找 data/0）", storedPID, pid)
	}

	if n := countRows(t, db, `SELECT count(1) FROM custominput WHERE solution_id = ?`, out.SolutionID); n != 1 {
		t.Errorf("custominput 行数 = %d，期望 1", n)
	}

	var after int
	if err := db.QueryRow(`SELECT submit FROM problem WHERE problem_id = ?`, pid).Scan(&after); err != nil {
		t.Fatalf("读取提交数失败: %v", err)
	}
	if after != before {
		t.Errorf("problem.submit = %d（之前 %d），测试运行不该计入提交数", after, before)
	}
}
