-- admin/ajax.php 的内联编辑端点查询。
--
-- 表格内联编辑（news_list / user_list / problem_list）把改动 POST 到这个端点。
-- 迁移期改用 nosurf（非消费式双提交 Cookie），因此单页连续多次编辑不会像
-- PHP 的消费式 token 那样第二次 403。
--
-- 列更新一律走显式查询（不做动态拼接列名），既安全又便于测试替身覆盖。

-- name: GetProblemTitleSource :one
SELECT title, source FROM problem WHERE problem_id = sqlc.arg(problem_id) LIMIT 1;

-- name: SetProblemSource :exec
UPDATE problem SET source = sqlc.arg(source) WHERE problem_id = sqlc.arg(problem_id);

-- name: SetProblemTimeLimit :exec
UPDATE problem SET time_limit = sqlc.arg(time_limit) WHERE problem_id = sqlc.arg(problem_id);

-- name: SetProblemMemoryLimit :exec
UPDATE problem SET memory_limit = sqlc.arg(memory_limit) WHERE problem_id = sqlc.arg(problem_id);

-- name: SetProblemCoin :exec
UPDATE problem SET coin = sqlc.arg(coin) WHERE problem_id = sqlc.arg(problem_id);

-- name: SetUserNick :exec
UPDATE users SET nick = sqlc.arg(nick) WHERE user_id = sqlc.arg(user_id);

-- name: SetUserExpiryDate :exec
UPDATE users SET expiry_date = sqlc.arg(expiry_date) WHERE user_id = sqlc.arg(user_id);

-- name: SetUserSchool :exec
UPDATE users SET school = sqlc.arg(school) WHERE user_id = sqlc.arg(user_id);

-- name: SetUserGroupName :exec
UPDATE users SET group_name = sqlc.arg(group_name) WHERE user_id = sqlc.arg(user_id);

-- name: SetUserParentPhone :exec
UPDATE users SET parent_phone = sqlc.arg(parent_phone) WHERE user_id = sqlc.arg(user_id);

-- name: SetUserCoinBonus :exec
UPDATE users SET coin_bonus = sqlc.arg(coin_bonus) WHERE user_id = sqlc.arg(user_id);

-- name: SetUserCoinSpent :exec
UPDATE users SET coin_spent = sqlc.arg(coin_spent) WHERE user_id = sqlc.arg(user_id);

-- name: SetNewsImportance :exec
UPDATE news SET importance = sqlc.arg(importance) WHERE news_id = sqlc.arg(news_id);

-- name: ListContestPrivilegeUsers :many
SELECT DISTINCT user_id FROM privilege WHERE rightstr = sqlc.arg(rightstr);
