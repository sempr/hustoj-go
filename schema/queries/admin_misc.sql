-- 后台零散页（admin/help.php、suspect_list.php）。

-- name: AvgJudgeDelay :one
-- 最近 10 条已判提交的平均判题耗时（help.php）。
SELECT avg(usedtime) AS delay
FROM (SELECT judgetime - in_date AS usedtime FROM solution WHERE result >= 4 ORDER BY solution_id DESC LIMIT 10) c;

-- name: CountSolutions :one
SELECT count(*) FROM solution;

-- name: SuspectByIP :many
-- 同一 IP 被多个不同用户使用（contest 内），并按 IP 展开明细。
SELECT u.ip, u.user_id, u.in_date, suspect.c
FROM (SELECT count(DISTINCT solution.user_id) AS c, solution.ip FROM solution WHERE solution.contest_id = sqlc.arg(contest_id) GROUP BY solution.ip) suspect
INNER JOIN (SELECT DISTINCT solution.ip, solution.user_id, solution.in_date FROM solution WHERE solution.contest_id = sqlc.arg(contest_id)) u
  ON suspect.ip = u.ip AND suspect.c > 1
ORDER BY suspect.c DESC, u.ip, u.in_date, u.user_id;

-- name: SuspectUsersByIP :many
-- 同一用户在比赛时段内用多个不同 IP 登录。
SELECT u.user_id, ips.ip, ips.time, suspect.c
FROM (SELECT count(DISTINCT loginlog.ip) AS c, loginlog.user_id FROM loginlog WHERE loginlog.time >= sqlc.arg(start_time) AND loginlog.time <= sqlc.arg(end_time) GROUP BY loginlog.user_id) suspect
INNER JOIN (SELECT DISTINCT solution.user_id FROM solution WHERE solution.contest_id = sqlc.arg(contest_id)) u
  ON suspect.user_id = u.user_id AND suspect.c > 1
INNER JOIN (SELECT DISTINCT loginlog.ip, loginlog.user_id, loginlog.time FROM loginlog WHERE loginlog.time >= sqlc.arg(start_time) AND loginlog.time <= sqlc.arg(end_time)) ips
  ON ips.user_id = u.user_id
ORDER BY suspect.c DESC, u.user_id, ips.time, ips.ip;
