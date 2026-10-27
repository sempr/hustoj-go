-- Status page queries
-- name: GetStatusList :many
SELECT s.solution_id, s.problem_id, s.user_id, s.language, s.result, s.in_date,
       s.time, s.memory, s.code_length, u.nick, u.group_name, u.starred,
       c.contest_id, c.title as contest_title, c.contest_type,
       p.title as problem_title
FROM solution s
JOIN users u ON s.user_id = u.user_id
LEFT JOIN contest c ON s.contest_id = c.contest_id
LEFT JOIN problem p ON s.problem_id = p.problem_id
WHERE s.problem_id > 0
  AND (s.contest_id = 0 OR s.contest_id IS NULL)
ORDER BY s.solution_id DESC
LIMIT ? OFFSET ?;

-- name: GetStatusListContest :many
SELECT s.solution_id, s.problem_id, s.user_id, s.num, s.language, s.result, s.in_date,
       s.time, s.memory, s.code_length, u.nick, u.group_name, u.starred,
       c.contest_id, c.title as contest_title, c.contest_type,
       p.title as problem_title
FROM solution s
JOIN users u ON s.user_id = u.user_id
LEFT JOIN contest c ON s.contest_id = c.contest_id
LEFT JOIN problem p ON s.problem_id = p.problem_id
WHERE s.contest_id = ?
ORDER BY s.solution_id DESC
LIMIT ? OFFSET ?;

-- name: GetStatusCount :one
SELECT COUNT(*) FROM solution s
JOIN users u ON s.user_id = u.user_id
WHERE s.problem_id > 0
  AND (s.contest_id = 0 OR s.contest_id IS NULL);

-- name: GetStatusCountContest :one
SELECT COUNT(*) FROM solution WHERE contest_id = ?;

-- name: GetContestForRank :one
SELECT contest_id, title, start_time, end_time, contest_type, private, langmask
FROM contest WHERE contest_id = ?;

-- name: GetContestProblemsForRank :many
SELECT num, problem_id, title
FROM contest_problem
WHERE contest_id = ?
ORDER BY num;

-- name: GetRankList :many
SELECT s.user_id, u.nick, u.group_name,
       COUNT(CASE WHEN s.result = 4 THEN 1 END) as solved,
       SUM(CASE WHEN s.result = 4 THEN 1 ELSE 0 END) as ac_count,
       MIN(CASE WHEN s.result = 4 THEN s.in_date END) as first_ac_time,
       SUM(CASE WHEN s.result = 4 THEN 1 ELSE 0 END) as total_ac
FROM solution s
JOIN users u ON s.user_id = u.user_id
WHERE s.contest_id = ?
GROUP BY s.user_id, u.nick, u.group_name
ORDER BY solved DESC, first_ac_time ASC;
