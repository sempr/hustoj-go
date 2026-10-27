-- 后台题目新增/编辑（admin/problem_add_page.php、problem_add.php、problem_edit.php）。
--
-- addproblem() 的 INSERT 以 defunct='Y' 落库（新题先隐藏，补完测试数据再启用）；
-- 需要 LastInsertId，故用 :execresult。编辑页 UPDATE 带 in_date=NOW() 与 coin。

-- name: InsertProblem :execresult
INSERT INTO problem (title, time_limit, memory_limit, description, input, output, sample_input, sample_output, hint, source, spj, in_date, defunct)
VALUES (sqlc.arg(title), sqlc.arg(time_limit), sqlc.arg(memory_limit), sqlc.arg(description), sqlc.arg(input), sqlc.arg(output), sqlc.arg(sample_input), sqlc.arg(sample_output), sqlc.arg(hint), sqlc.arg(source), sqlc.arg(spj), NOW(), 'Y');

-- name: UpdateProblemFull :exec
UPDATE problem SET title=sqlc.arg(title), time_limit=sqlc.arg(time_limit), memory_limit=sqlc.arg(memory_limit),
  description=sqlc.arg(description), input=sqlc.arg(input), output=sqlc.arg(output),
  sample_input=sqlc.arg(sample_input), sample_output=sqlc.arg(sample_output), hint=sqlc.arg(hint),
  source=sqlc.arg(source), spj=sqlc.arg(spj), remote_oj=sqlc.arg(remote_oj), remote_id=sqlc.arg(remote_id),
  in_date=NOW(), coin=sqlc.arg(coin)
WHERE problem_id=sqlc.arg(problem_id);

-- name: InsertContestProblem :exec
INSERT INTO contest_problem (problem_id, contest_id, num) VALUES (sqlc.arg(problem_id), sqlc.arg(contest_id), sqlc.arg(num));

-- name: ListFutureContests :many
SELECT contest_id, title FROM contest WHERE start_time > NOW() ORDER BY contest_id;

-- name: GetLastProblemSource :one
SELECT source FROM problem ORDER BY problem_id DESC LIMIT 1;
