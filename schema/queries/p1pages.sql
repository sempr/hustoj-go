-- P1 用户页查询（online.php、recent-contest.php、export_ac_code.php、export_contest_code.php、contestrank2/3.php）

-- ── 在线用户（online.php）────────────────────────────────────────────

-- name: GetOnlineUsers :many
SELECT user_id, password, ip, time FROM loginlog
ORDER BY log_id DESC LIMIT 50;

-- name: GetOnlineUsersByIP :many
SELECT user_id, password, ip, time FROM loginlog
WHERE ip LIKE sqlc.arg(ip_search)
ORDER BY log_id DESC LIMIT 50;

-- name: GetOnlineUsersExcludeSelf :many
SELECT user_id, password, ip, time FROM loginlog
WHERE user_id != sqlc.arg(uid)
ORDER BY log_id DESC LIMIT 50;

-- name: InsertLoginLog :exec
INSERT INTO loginlog (user_id, password, ip, time)
VALUES (?, ?, ?, NOW());

-- ── 最近比赛（recent-contest.php）────────────────────────────────────

-- name: GetRecentContestsList :many
SELECT contest_id, title, start_time, end_time, private, user_id
FROM contest
WHERE defunct = 'N'
ORDER BY contest_id DESC;

-- ── 导出 AC 代码（export_ac_code.php）───────────────────────────────

-- name: GetACCodeByUser :many
SELECT sc.source, s.problem_id
FROM source_code sc
INNER JOIN (
  SELECT solution_id, problem_id
  FROM solution
  WHERE solution.user_id = sqlc.arg(uid) AND solution.result = 4
) s ON sc.solution_id = s.solution_id
WHERE s.problem_id NOT IN (
  SELECT cp.problem_id FROM contest_problem cp
  WHERE cp.contest_id IN (
    SELECT c.contest_id FROM contest c
    WHERE c.start_time < NOW() AND c.end_time > NOW()
  )
)
ORDER BY s.problem_id;

-- ── 导出比赛代码（export_contest_code.php）──────────────────────────

-- name: GetContestCode :many
SELECT s.user_id, s.problem_id, s.result,
       (SELECT sc.source FROM source_code sc WHERE sc.solution_id = s.solution_id) AS source
FROM solution s
WHERE s.contest_id = sqlc.arg(cid)
ORDER BY s.solution_id;

-- ── 比赛排名（contestrank2.php、contestrank3.php）───────────────────

-- name: GetContestInfo :one
SELECT start_time, title, end_time FROM contest WHERE contest_id = sqlc.arg(cid);

-- name: GetContestProblemCount :one
SELECT COUNT(1) AS pbc FROM contest_problem WHERE contest_id = sqlc.arg(cid);

-- name: GetContestSubmissions :many
SELECT s.solution_id, s.user_id, s.problem_id, s.num, s.in_date, s.result
FROM solution s
WHERE s.contest_id = sqlc.arg(cid) AND s.num >= 0 AND s.problem_id > 0;

-- name: GetFirstBlood :many
SELECT s.num, s.user_id
FROM solution s
INNER JOIN (
  SELECT num, MIN(solution_id) AS minId
  FROM solution
  WHERE solution.contest_id = sqlc.arg(cid) AND solution.result = 4
  GROUP BY num
) c ON s.solution_id = c.minId;

-- name: GetContestTeams :many
SELECT DISTINCT user_id, nick FROM solution WHERE contest_id = sqlc.arg(cid);

-- name: GetContestProblemMap :many
SELECT problem_id, num FROM contest_problem WHERE contest_id = sqlc.arg(cid);
