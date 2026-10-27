package repository

// 提交入库。
//
// 为什么不放 sqlc：MySQL 的 INSERT 没有 RETURNING，取自增主键必须用
// Result.LastInsertId()，而 sqlc 的 :one 要求查询带 RETURNING 子句
// （codegen 会直接报 "specifies parameter :one without containing a
// RETURNING clause"）。把这条 INSERT 手写在这里，SQL 与其余查询一样
// 在 schema/queries/solution.sql 里留有文档。
//
// 为什么要事务：一次提交要写四张表（solution、source_code_user、
// source_code、可能还有 custominput），并更新 problem/contest_problem
// 计数。任何一步失败都必须整体回滚 —— 尤其不能在 solution 已落库、
// 源码还没写时提交事务，否则判题机会取到一条 result 已置 0 但
// 读不到源码的记录，编译出"空程序"并给出误导性的 CE。

import (
	"context"
	"database/sql"
	"fmt"
)

// solutionNickMaxLen 是 solution.nick 的字符容量。
//
// schema/schema.sql 定义该列为 char(20)。MySQL 严格模式下超长会整条
// INSERT 失败（Error 1406 Data too long），表现为"点提交就报错"，而
// 问题其实只在这一列的显示上。写入前按此宽度截断，提交不因昵称长短而失败。
const solutionNickMaxLen = 20

// truncateRunes 按 rune 截断，避免在 UTF-8 字符中间切断。
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// SubmitParams 是一次提交的完整参数。
type SubmitParams struct {
	// ProblemID 是目标题目的**正** id（练习/比赛模式）。
	//
	// 测试运行（TestRun）落库时会被改写成 0（见 insertSolutionRow）：
	// 与所有 `problem_id>0` 的统计口径隔离，且和 Go status-ajax 的
	// `problem_id==0` 测试运行分支对齐。
	ProblemID int32

	// TestRun 标记这是一次测试运行（提交页的"测试运行"按钮）。
	//
	// 客户端把 id 取负后再 POST（submitpage.php:576
	// `problem_id.value=-problem_id.value`），服务端据此判定。
	// 显式传标志而不是从 ProblemID 的符号推：比赛模式下 ProblemID
	// 与练习模式无关，任何"靠符号推断"的写法都会在比赛提交上判错。
	//
	// 测试运行写 custominput 并把 result 推进到 0（进判题队列）。judged-go
	// 取件后检测到 custominput 行，走"只跑自定义输入、不比对"的测试运行路径。
	TestRun bool

	// UserID 是提交者。
	UserID string

	// Nick 冗余写入 solution.nick，供列表页免 JOIN users。
	//
	// 调用方传什么就写什么（当前传提交者的 user_id，不再取 users.nick：
	// 昵称可能被 user_import / modifypage / 后台改资料写成超过 solution.nick
	// 容量的长度）。写入时按列容量截断，见 insertSolutionRow —— 这一列只
	// 影响显示，不该让整次提交失败。
	Nick string

	// Language 是语言下标（$language_ext 的下标）。
	Language int32

	// IP 是提交者 IP，已取 X-Forwarded-For 的第一跳。
	IP string

	// CodeLength 是送判源码的字节长度。
	CodeLength int32

	// Source 是实际送进编译器的文本（可能已拼接 prepend/append）。
	Source string

	// SourceUser 是用户提交的原文（含上传文件的原始内容）。
	SourceUser string

	// InputText 非空时写入 custominput，即测试运行的自定义输入。
	InputText string

	// ContestID 为 0 表示练习模式；此时 Num 不参与写入。
	ContestID int32

	// Num 是比赛内的展示序号（1-based），仅比赛模式有效。
	Num int32

	// IsContest 区分练习/比赛模式。
	//
	// 不可由 ContestID==0 推导：PHP 在练习模式下根本不传 contest_id
	// 这两列，走的是列默认值（contest_id=0、num=-1），语义上仍是
	// 练习模式；而"cid=0"在 PHP 里是另一个含义（走练习分支但不带 id）。
	IsContest bool
}

// SubmitResult 是一次成功提交的结果。
type SubmitResult struct {
	// SolutionID 是新生成的提交 ID。
	SolutionID int64

	// FinalResult 是事务提交时 solution.result 的最终值。
	//
	// 正常提交为 0（OJ_WT0，等待判题）；测试运行为 14（OJ_MC，
	// 等人工确认，不进判题队列）。
	FinalResult int
}

// InsertSubmission 在一个事务内写入提交记录，返回新提交 ID。
//
// 写入顺序（与 submit.php 一致，且顺序本身有语义）：
//
//  1. INSERT solution，result 先写 14（提交中）
//  2. INSERT source_code_user / source_code
//  3. 若是测试运行，INSERT custominput
//  4. 若是远程评测题目，result 置 16（远程等待）
//  5. 否则把 result 从 14 推进到 0 —— 这一步是"可以判了"的开关
//  6. 更新题目/比赛题目的提交计数
//
// 第 1 步为什么不用 result=0：判题机 fetcher 的取件条件是 `result<2`。
// 若插入时就是 0，judged-go 会在本事务提交之前就把这道题取走，
// 此时 source_code 尚未落库，编译必然失败。第 5 步与前四步同事务，
// 所以事务一提交，源码与 result 同时可见，不存在中间态。
//
// 练习模式不写 contest_id / num 两列，与 PHP 的两条不同 INSERT 保持
// 一致（PHP 练习分支的 SQL 里根本没有这两列，走列默认值 0 与 -1）。
func InsertSubmission(ctx context.Context, db *sql.DB, p SubmitParams) (SubmitResult, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("开启提交事务失败: %w", err)
	}
	// 任何一条错误路径都回滚。已提交的事务返回 nil，忽略是安全的。
	defer func() { _ = tx.Rollback() }()

	solutionID, err := insertSolutionRow(ctx, tx, p)
	if err != nil {
		return SubmitResult{}, err
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO source_code_user (solution_id, source) VALUES (?, ?)`,
		solutionID, p.SourceUser); err != nil {
		return SubmitResult{}, fmt.Errorf("写入用户源码失败: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO source_code (solution_id, source) VALUES (?, ?)`,
		solutionID, p.Source); err != nil {
		return SubmitResult{}, fmt.Errorf("写入送判源码失败: %w", err)
	}

	// 测试运行才写自定义输入。判罚码推进到 0（进判题队列）：judged-go 据此
	// 取件，检测到 custominput 行后走"只跑自定义输入、不比对"的测试运行路径。
	// PHP 是无条件插入 custominput（input_text 可为空），这里保持一致。
	final := 14
	if p.TestRun {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO custominput (solution_id, input_text) VALUES (?, ?)`,
			solutionID, p.InputText); err != nil {
			return SubmitResult{}, fmt.Errorf("写入自定义输入失败: %w", err)
		}
		final = 0
	} else {
		// 非测试运行才计入题目/比赛题目的提交数。
		// PHP 的 `if ($test_run) {...} else { UPDATE problem SET submit=submit+1 }`
		// 是同一个分叉：测试运行不影响任何统计。
		if _, err := tx.ExecContext(ctx,
			`UPDATE problem SET submit = submit + 1 WHERE problem_id = ?`,
			p.ProblemID); err != nil {
			return SubmitResult{}, fmt.Errorf("更新题目提交数失败: %w", err)
		}
		if p.IsContest {
			if _, err := tx.ExecContext(ctx,
				`UPDATE contest_problem SET c_submit = c_submit + 1 WHERE contest_id = ? AND num = ?`,
				p.ContestID, p.Num); err != nil {
				return SubmitResult{}, fmt.Errorf("更新比赛题目提交数失败: %w", err)
			}
		}
		// 正常提交进本地判题队列。
		final = 0
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE solution SET result = ? WHERE solution_id = ?`,
		final, solutionID); err != nil {
		return SubmitResult{}, fmt.Errorf("推进提交状态失败: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return SubmitResult{}, fmt.Errorf("提交事务失败: %w", err)
	}
	return SubmitResult{SolutionID: solutionID, FinalResult: final}, nil
}

// insertSolutionRow 插入 solution 行并返回自增 ID。
//
// 练习模式与比赛模式是两条不同的 INSERT，与 submit.php:329/332 一致：
// 练习分支的 SQL 不含 contest_id/num 两列。
func insertSolutionRow(ctx context.Context, tx *sql.Tx, p SubmitParams) (int64, error) {
	const base = `INSERT INTO solution
	  (problem_id, user_id, nick, in_date, language, ip, code_length, result)
	  VALUES (?, ?, ?, NOW(), ?, ?, ?, 14)`
	const contest = `INSERT INTO solution
	  (problem_id, user_id, nick, in_date, language, ip, code_length, contest_id, num, result)
	  VALUES (?, ?, ?, NOW(), ?, ?, ?, ?, ?, 14)`

	// 测试运行存 problem_id=0（PHP 对无题号的自测即如此）。这样它与真实提交
	// 的统计口径天然隔离：所有计数 SQL 都带 `problem_id>0`，0 不会被计入
	// problem.accepted / users.solved。判题侧靠 custominput 行识别测试运行。
	pid := p.ProblemID
	if p.TestRun {
		pid = 0
	}

	// solution.nick 只作显示快照，按列容量截断，保证超长值不会让整条
	// INSERT 以 1406 失败（真实发生过：users.nick 被导入加宽后，其用户
	// 一提交就报 Data too long for column 'nick'）。
	nick := truncateRunes(p.Nick, solutionNickMaxLen)

	var res sql.Result
	var err error
	if p.IsContest {
		res, err = tx.ExecContext(ctx, contest,
			pid, p.UserID, nick, p.Language, p.IP, p.CodeLength, p.ContestID, p.Num)
	} else {
		res, err = tx.ExecContext(ctx, base,
			pid, p.UserID, nick, p.Language, p.IP, p.CodeLength)
	}
	if err != nil {
		return 0, fmt.Errorf("写入提交记录失败: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("获取提交 ID 失败: %w", err)
	}
	return id, nil
}

// MarkSolutionRemote 把提交标记为远程评测（result=16）。
//
// 单独于 InsertSubmission，因为是否远程取决于 problem.remote_oj，
// 而它在准入阶段就要查（决定 problem_id 用正数还是负数），
// 但 PHP 是在插入之后才更新这一列 —— 沿用同样的顺序。
func MarkSolutionRemote(ctx context.Context, db *sql.DB, solutionID int64, remoteOJ string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE solution SET result = 16, remote_oj = ? WHERE solution_id = ?`,
		remoteOJ, solutionID)
	if err != nil {
		return fmt.Errorf("标记远程评测失败: %w", err)
	}
	return nil
}

// DemoteContestSubmissions 把同场同题的历史提交降级为测试运行。
//
// 返回受影响的行数；调用方据此决定是否需要重算题目计数
// （PHP 的 `if ($delete > 0)`）。
func DemoteContestSubmissions(ctx context.Context, db *sql.DB, contestID int32, userID string, num int32) (int64, error) {
	res, err := db.ExecContext(ctx,
		`UPDATE solution SET result = 13, pass_rate = 0.0
		 WHERE contest_id = ? AND user_id = ? AND num = ?`,
		contestID, userID, num)
	if err != nil {
		return 0, fmt.Errorf("降级历史提交失败: %w", err)
	}
	return res.RowsAffected()
}
