package repository

// solution.nick 的列宽回归测试。
//
// 与 submit_test.go 分开：那个文件只验证事务契约（失败不留残留），
// 这里验证一个具体的历史故障 —— 超长昵称把整次提交打挂。

import (
	"context"
	"strings"
	"testing"
)

// TestInsertSubmissionTruncatesOverlongNick 验证 Nick 超过列容量时提交仍成功。
//
// 回归用例：solution.nick 是 char(20)，而 users.nick 会被 user_import /
// modifypage / 后台改资料写成更长（那几处的 ALTER 只加宽 users.nick）。
// 旧实现把超长值原样写进 solution.nick，MySQL 严格模式直接
// Error 1406 (Data too long for column 'nick')，整次提交回滚 ——
// 线上表现就是"点提交就报错，但哈希、源码都没问题"。
//
// 断言两件事：不报错，且落库值是按 rune 截断的（不能按字节切，
// 否则中文会被切成半个字，存进去就是乱码）。
func TestInsertSubmissionTruncatesOverlongNick(t *testing.T) {
	db := rollbackDB(t)
	seedRollbackUser(t, db)
	t.Cleanup(func() { seedRollbackUser(t, db) })

	// 30 个汉字 = 90 字节，远超 char(20) 的 20 字符容量。
	longNick := strings.Repeat("超", 30)
	src := "int main(){return 0;}"

	res, err := InsertSubmission(context.Background(), db, SubmitParams{
		ProblemID:  rollbackProblem,
		UserID:     rollbackUser,
		Nick:       longNick,
		Language:   0,
		IP:         "127.0.0.1",
		CodeLength: int32(len(src)),
		Source:     src,
		SourceUser: src,
	})
	if err != nil {
		t.Fatalf("超长昵称导致提交失败（回归：Error 1406 Data too long for column 'nick'）: %v", err)
	}

	var got string
	if err := db.QueryRow(`SELECT nick FROM solution WHERE solution_id = ?`, res.SolutionID).Scan(&got); err != nil {
		t.Fatalf("读取 solution.nick 失败: %v", err)
	}
	want := strings.Repeat("超", solutionNickMaxLen)
	if got != want {
		t.Errorf("solution.nick = %q，期望按 rune 截断为 %q", got, want)
	}
}
