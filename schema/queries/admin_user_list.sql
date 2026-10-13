-- 后台用户列表查询（admin/user_list.php）。
--
-- 与 PHP 等价要点：
--   - 计数是 COUNT('user_id')（字符串常量等值于 COUNT(*)），且不区分 defunct。
--   - 默认只看 defunct='N'（?trash 则只看 'Y'），按 accesstime DESC，25 条/页。
--   - 搜索命中 6 列（user_id/nick/school/group_name/parent_phone/ip），**不加 LIMIT**，
--     按 user_id DESC。搜索是裸 LIKE（% 与 _ 通配）。

-- name: CountUsers :one
SELECT COUNT(*) AS ids FROM users;

-- name: ListUsersActive :many
SELECT user_id, nick, email, accesstime, reg_time, expiry_date, ip, school, group_name, parent_phone, defunct, coin_earned, coin_bonus, coin_spent
FROM users
WHERE defunct = 'N'
ORDER BY accesstime DESC
LIMIT ? OFFSET ?;

-- name: ListUsersTrash :many
SELECT user_id, nick, email, accesstime, reg_time, expiry_date, ip, school, group_name, parent_phone, defunct, coin_earned, coin_bonus, coin_spent
FROM users
WHERE defunct = 'Y'
ORDER BY accesstime DESC
LIMIT ? OFFSET ?;

-- name: SearchUsers :many
SELECT user_id, nick, email, accesstime, reg_time, expiry_date, ip, school, group_name, parent_phone, defunct, coin_earned, coin_bonus, coin_spent
FROM users
WHERE (user_id LIKE sqlc.arg(kw_user) OR nick LIKE sqlc.arg(kw_nick) OR school LIKE sqlc.arg(kw_school)
       OR group_name LIKE sqlc.arg(kw_group) OR parent_phone LIKE sqlc.arg(kw_phone) OR ip LIKE sqlc.arg(kw_ip))
ORDER BY user_id DESC;
