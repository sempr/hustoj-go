-- P2 用户功能查询（mail.php、modify.php、lostpassword.php、lostpassword2.php）

-- ── 邮件（mail.php）────────────────────────────────────────────────

-- name: GetMailList :many
SELECT mail_id, from_user, to_user, title, content, in_date, new_mail
FROM mail
WHERE to_user = sqlc.arg(user_id) OR from_user = sqlc.arg(user_id)
ORDER BY mail_id DESC;

-- name: GetMailByID :many
SELECT mail_id, from_user, to_user, title, content, in_date, new_mail
FROM mail
WHERE mail_id = sqlc.arg(mail_id)
  AND (to_user = sqlc.arg(user_id) OR from_user = sqlc.arg(user_id));

-- name: MarkMailRead :exec
UPDATE mail SET new_mail = 0 WHERE mail_id = sqlc.arg(mail_id);

-- name: SendMail :exec
INSERT INTO mail (to_user, from_user, title, content, in_date)
VALUES (sqlc.arg(to_user), sqlc.arg(from_user), sqlc.arg(title), sqlc.arg(content), NOW());

-- name: CheckMailPrivilege :many
SELECT 1 FROM privilege
WHERE (rightstr = 'source_browser' OR rightstr = 'administrator')
  AND (user_id = sqlc.arg(from_user) OR user_id = sqlc.arg(to_user));

-- ── 用户信息修改（modify.php）──────────────────────────────────────

-- name: GetUserByID :one
SELECT user_id, password, nick, school, email FROM users WHERE user_id = sqlc.arg(user_id);

-- name: UpdateUserPassword :exec
UPDATE users SET password = sqlc.arg(password) WHERE user_id = sqlc.arg(user_id);

-- name: UpdateUserProfile :exec
UPDATE users SET password = sqlc.arg(password), nick = sqlc.arg(nick),
       school = sqlc.arg(school), email = sqlc.arg(email)
WHERE user_id = sqlc.arg(user_id);

-- name: UpdateUserSolutionNick :exec
UPDATE solution SET nick = sqlc.arg(nick) WHERE user_id = sqlc.arg(user_id);

-- ── 密码重置（lostpassword.php、lostpassword2.php）──────────────────

-- name: GetUserEmail :one
SELECT email FROM users WHERE user_id = sqlc.arg(user_id) AND defunct = 'N';

-- name: UpdateUserPasswordByKey :exec
UPDATE users SET password = sqlc.arg(password) WHERE user_id = sqlc.arg(user_id);

-- ── 账户激活（active.php）────────────────────────────────────────────

-- name: ActivateUserByCode :exec
UPDATE users SET defunct = 'N', activecode = ''
WHERE activecode = sqlc.arg(code) AND activecode != '' AND defunct = 'Y';

-- ── 批量删除题目（admin/problem_batch_delete.php）───────────────────

-- name: DeleteContestProblemByPID :exec
DELETE FROM contest_problem WHERE problem_id = sqlc.arg(pid);

-- name: DeleteProblemByID :exec
DELETE FROM problem WHERE problem_id = sqlc.arg(pid);

-- name: DeleteProblemPrivilege :exec
DELETE FROM privilege WHERE rightstr = sqlc.arg(rightstr);

-- name: UpdateSolutionProblemToZero :exec
UPDATE solution SET problem_id = 0, result = 13 WHERE problem_id = sqlc.arg(pid);

-- name: GetMaxProblemID :one
SELECT MAX(problem_id) AS max_id FROM problem;
