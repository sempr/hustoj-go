-- 状态页 AJAX（status-ajax.php）的查询。

-- 按 solution_id 取整行（status-ajax.php:24-25 的 SELECT *）。
-- :one 配 LIMIT 1，与 PHP 一致：查不到即无行。
-- name: GetSolutionStatusByID :one
SELECT * FROM solution WHERE solution_id = sqlc.arg(solution_id) LIMIT 1;

-- 比赛标题与类型，用于 NOIP 屏蔽（status-ajax.php:66-69）。
-- name: GetContestTitleType :one
SELECT title, contest_type FROM contest WHERE contest_id = sqlc.arg(contest_id);

-- 编译错误详情（status-ajax.php:34）：result=11 走 compileinfo。
-- name: GetCompileError :one
SELECT error FROM compileinfo WHERE solution_id = sqlc.arg(solution_id);

-- 运行错误详情（status-ajax.php:43）：其余走 runtimeinfo。
-- name: GetRuntimeError :one
SELECT error FROM runtimeinfo WHERE solution_id = sqlc.arg(solution_id);

-- 判题信息被查看后清掉自定义输入（status-ajax.php:54-55）。
-- name: DeleteCustomInput :exec
DELETE FROM custominput WHERE solution_id = sqlc.arg(solution_id);
