-- 题目提交状态页（problemstatus.php）的查询。

-- NOIP 拦截（problemstatus.php:34-44）。
-- 与 ranklist.php 的版本不同：这里只看标题关键字，不看 contest_type。
-- name: CountRunningNOIPProblemContests :one
SELECT count(contest_id) FROM contest
WHERE start_time < sqlc.arg(now) AND end_time > sqlc.arg(now)
  AND title LIKE sqlc.arg(keyword);

-- 提交过的不同用户数（problemstatus.php:63-67）。
-- name: CountProblemUsers :one
SELECT count(DISTINCT user_id) FROM solution WHERE problem_id = sqlc.arg(problem_id);

-- AC 过的不同用户数（problemstatus.php:70-75）。
-- name: CountProblemACUsers :one
SELECT count(DISTINCT user_id) FROM solution
WHERE problem_id = sqlc.arg(problem_id) AND result = 4;

-- 各评测结果分布（problemstatus.php:82-96）。jresult 下标从 result 取，
-- 因此升序；只统计 result>=4。
-- name: GetProblemResultStats :many
SELECT result, count(1) AS cnt FROM solution
WHERE problem_id = sqlc.arg(problem_id) AND result >= 4
GROUP BY result
ORDER BY result;

-- 题目是否被进行中的比赛占用（problemstatus.php:122-124）。
-- 命中时内存/耗时/代码长度列显示 ------。
-- name: ProblemInRunningContest :one
SELECT count(*) > 0 FROM contest_problem
WHERE problem_id = sqlc.arg(problem_id)
  AND contest_id IN (
    SELECT contest_id FROM contest
    WHERE start_time < sqlc.arg(now) AND end_time > sqlc.arg(now)
  );

-- $OJ_AUTO_SHARE 开启时，某个用户是否已 AC 该题（problemstatus.php:131-133）。
-- name: UserSolvedProblem :one
SELECT count(*) > 0 FROM solution
WHERE result = 4 AND problem_id = sqlc.arg(problem_id) AND user_id = sqlc.arg(user_id);

-- 推荐题目：先取本题 source，再按 source LIKE 找相关题（problemstatus.php:243-248）。
-- name: GetProblemSource :one
SELECT source FROM problem WHERE problem_id = sqlc.arg(problem_id);

-- name: ListProblemIDsBySource :many
SELECT problem_id FROM problem
WHERE source LIKE sqlc.arg(source) AND problem_id != sqlc.arg(problem_id)
LIMIT 10;
