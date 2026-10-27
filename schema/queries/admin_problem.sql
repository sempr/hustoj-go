-- 后台题目管理：列表与变更（admin/problem_list.php、problem_df_change.php、
-- problem_del.php、problem_changeid.php）。
--
-- 列表计数是 COUNT('problem_id')（等值 COUNT(*)），**不过滤** problem_id>0 也不过滤
-- defunct —— 与 problem_list.php 一致（不同于 problemset.php 的管理员列表）。
-- 搜索命中 5 列（problem_id/title/description/source/hint），不加 LIMIT，按 problem_id ASC。

-- name: CountProblemsAll :one
SELECT COUNT(*) AS ids FROM problem;

-- name: ListProblemsAdminList :many
SELECT problem_id, title, accepted, in_date, defunct, source, remote_oj, remote_id, coin
FROM problem
ORDER BY problem_id DESC
LIMIT ? OFFSET ?;

-- name: SearchProblemsAdminList :many
SELECT problem_id, title, accepted, in_date, defunct, source, remote_oj, remote_id, coin
FROM problem
WHERE (CAST(problem_id AS CHAR) LIKE CONCAT(sqlc.arg(kw_pid), '') OR title LIKE sqlc.arg(kw_title) OR description LIKE sqlc.arg(kw_desc)
       OR source LIKE sqlc.arg(kw_src) OR hint LIKE sqlc.arg(kw_hint))
ORDER BY problem_id ASC;

-- name: GetProblemDefunct :one
SELECT defunct FROM problem WHERE problem_id = sqlc.arg(problem_id) LIMIT 1;

-- name: SetProblemDefunctByID :exec
UPDATE problem SET defunct = sqlc.arg(defunct) WHERE problem_id = sqlc.arg(problem_id);

-- name: ProblemIDExists :one
SELECT COUNT(*) AS n FROM problem WHERE problem_id = sqlc.arg(problem_id);

-- name: UpdateProblemID :exec
UPDATE problem SET problem_id = sqlc.arg(new_id) WHERE problem_id = sqlc.arg(old_id);

-- name: UpdateSolutionProblemID :exec
UPDATE solution SET problem_id = sqlc.arg(new_id) WHERE problem_id = sqlc.arg(old_id);

-- name: UpdateContestProblemProblemID :exec
UPDATE contest_problem SET problem_id = sqlc.arg(new_id) WHERE problem_id = sqlc.arg(old_id);

-- name: UpdateTopicPID :exec
UPDATE topic SET pid = sqlc.arg(new_id) WHERE pid = sqlc.arg(old_id);
