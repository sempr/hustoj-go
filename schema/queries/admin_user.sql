-- 后台用户/权限管理查询（admin/privilege_list.php、privilege_add.php、
-- privilege_delete.php、changepass.php、user_df_change.php、user_set_ip.php）。
--
-- 可管理权限白名单与 PHP privilege_list.php 的 $privileges 逐字一致：
-- 列表只展示这些 rightstr，避免把 c<cid>/s<pid> 之类的海量授权也铺出来。

-- name: CountPrivileges :one
SELECT COUNT(*) AS ids FROM privilege
WHERE rightstr IN ('administrator','source_browser','contest_creator','user_adder','http_judge','problem_editor','tag_adder','problem_importer','problem_verifiter','password_setter','printer','balloon','vip','problem_start','problem_end','service_port');

-- name: ListPrivileges :many
SELECT user_id, rightstr, valuestr, defunct FROM privilege
WHERE rightstr IN ('administrator','source_browser','contest_creator','user_adder','http_judge','problem_editor','tag_adder','problem_importer','problem_verifiter','password_setter','printer','balloon','vip','problem_start','problem_end','service_port')
ORDER BY user_id, rightstr
LIMIT ? OFFSET ?;

-- name: SearchPrivileges :many
SELECT user_id, rightstr, valuestr, defunct FROM privilege
WHERE user_id LIKE ? OR rightstr LIKE ?
ORDER BY user_id, rightstr;

-- name: InsertPrivilege :exec
INSERT INTO privilege (user_id, rightstr, valuestr, defunct) VALUES (?, ?, ?, 'N');

-- name: DeletePrivilege :exec
DELETE FROM privilege WHERE user_id = ? AND rightstr = ?;

-- name: GetUserDefunct :one
SELECT defunct FROM users WHERE user_id = ? LIMIT 1;

-- name: SetUserDefunct :exec
UPDATE users SET defunct = ? WHERE user_id = ?;

-- name: UpdateUserPasswordAdmin :execrows
-- changepass.php：只能改非管理员账号，返回受影响行数供调用方判断是否命中。
UPDATE users SET password = sqlc.arg(password)
WHERE users.user_id = sqlc.arg(user_id)
  AND users.user_id NOT IN (SELECT p.user_id FROM privilege p WHERE p.rightstr = 'administrator');
