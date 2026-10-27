-- 题目导出（admin/problem_export_xml.php）。
--
-- 按题号区间取题（后端最常用的一种）。按 id 列表导出用 GetProblemByID 循环，
-- 按比赛导出用 ListContestProblemIDs（按 num 排序）。

-- name: ListProblemsByRange :many
SELECT problem_id, title, time_limit, memory_limit, description, input, output, sample_input, sample_output, hint, source, remote_oj, remote_id, spj
FROM problem
WHERE problem_id >= sqlc.arg(start_id) AND problem_id <= sqlc.arg(end_id)
ORDER BY problem_id;

-- name: GetContestTitle :one
SELECT title FROM contest WHERE contest_id = sqlc.arg(contest_id) LIMIT 1;
