-- 出题人 AJAX（problem-ajax.php）的查询。
--
-- problem-ajax.php:17 按 rightstr='p<pid>' 取 privilege 表里的第一个用户，
-- 作为题目命题人展示；无命中时 PHP 输出 $MSG_IMPORTED（"外部导入"）。
-- name: GetProblemCreator :one
SELECT user_id FROM privilege WHERE rightstr = sqlc.arg(rightstr) LIMIT 1;
