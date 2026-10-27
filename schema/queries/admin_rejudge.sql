-- 后台重新判题（admin/rejudge.php）与解题统计（admin/solution_statistics.php）。
--
-- rejudge 把 solution.result 置 1（等待重判）并清 pass_rate；judged-go 轮询
-- result<2 会自动取件，因此 Go 侧不复刻 PHP 的 Redis lpush / UDP trigger_judge。
--
-- solution_statistics 需要在 Go 侧做透视（每用户 × 每题的 min(result)），
-- PHP 用动态 case 列实现；这里取回 (user,problem,min) 明细后由 Go 透视。

-- name: RejudgeProblemSolutions :exec
UPDATE solution SET result = 1, pass_rate = 0 WHERE problem_id = sqlc.arg(problem_id) AND problem_id > 0;

-- name: ResetProblemAccepted :exec
UPDATE problem SET accepted = 0 WHERE problem_id = sqlc.arg(problem_id) AND problem_id > 0;

-- name: DeleteSimByProblem :exec
DELETE FROM sim WHERE s_id IN (SELECT solution_id FROM solution WHERE problem_id = sqlc.arg(problem_id));

-- name: DeleteSimBySolution :exec
DELETE FROM sim WHERE s_id = sqlc.arg(solution_id);

-- name: RejudgeSolution :exec
UPDATE solution SET result = 1, pass_rate = 0 WHERE solution_id = sqlc.arg(solution_id) AND problem_id > 0;

-- name: GetSolutionContestID :one
SELECT contest_id FROM solution WHERE solution_id = sqlc.arg(solution_id) LIMIT 1;

-- name: RejudgeByResult :exec
UPDATE solution SET result = sqlc.arg(to_result), pass_rate = 0 WHERE result = sqlc.arg(from_result) AND problem_id >= 0;

-- name: RejudgeContest :exec
UPDATE solution SET result = 1, pass_rate = 0 WHERE contest_id = sqlc.arg(contest_id) AND problem_id > 0;

-- name: RejudgeContestProblem :exec
UPDATE solution SET result = 1, pass_rate = 0 WHERE contest_id = sqlc.arg(contest_id) AND num = sqlc.arg(num);

-- ── 解题统计（solution_statistics.php）──────────────────────────────

-- name: SolutionMinByContest :many
SELECT user_id, nick, problem_id, CAST(MIN(result) AS SIGNED) AS r
FROM solution WHERE contest_id = sqlc.arg(contest_id)
GROUP BY user_id, nick, problem_id;

-- name: SolutionMinByUsers :many
SELECT user_id, nick, problem_id, CAST(MIN(result) AS SIGNED) AS r
FROM solution WHERE user_id IN (sqlc.slice('user_ids'))
GROUP BY user_id, nick, problem_id;

-- name: ListRecentContests50 :many
SELECT contest_id, title FROM contest ORDER BY contest_id DESC LIMIT 50;
