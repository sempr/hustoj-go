-- 后台批量建号（admin/user_add.php）与 CSV 导入（admin/user_import.php）。
--
-- user_add：每行 "userid password"，逐行建号（已存在则报错），并写一条 loginlog。
-- user_import：CSV 行 → upsert（存在则覆盖 email/ip/password/nick/school/group/expiry）。
-- 到期日由 Go 按 OJ_EXPIRY_DAYS（默认 365）算好后作为日期传入，不在 SQL 里算 INTERVAL。

-- name: InsertBulkUser :exec
INSERT INTO users (user_id, password, reg_time, expiry_date, nick)
VALUES (sqlc.arg(user_id), sqlc.arg(password), NOW(), sqlc.arg(expiry_date), sqlc.arg(nick));

-- name: UpsertImportUser :exec
INSERT INTO users (user_id, email, ip, accesstime, password, reg_time, nick, school, group_name, expiry_date)
VALUES (sqlc.arg(user_id), sqlc.arg(email), sqlc.arg(ip), NOW(), sqlc.arg(password), NOW(), sqlc.arg(nick), sqlc.arg(school), sqlc.arg(group_name), sqlc.arg(expiry_date))
ON DUPLICATE KEY UPDATE email=sqlc.arg(email), ip=sqlc.arg(ip), accesstime=NOW(), password=sqlc.arg(password), reg_time=NOW(), nick=sqlc.arg(nick), school=sqlc.arg(school), group_name=sqlc.arg(group_name), expiry_date=sqlc.arg(expiry_date);
