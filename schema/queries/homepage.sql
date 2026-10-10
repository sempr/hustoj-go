-- 首页/门户/新闻详情查询
--
-- 这些查询对应 phpweb/index.php、phpweb/portal.php、phpweb/viewnews.php
-- 的 SQL 逻辑，绞杀期两者必须给出完全相同的结果。

-- ── 首页（index.php）────────────────────────────────────────────────

-- name: GetNoipContestIDs :many
-- 获取进行中的 NOIP 比赛 ID 列表（用于排除统计）
SELECT contest_id FROM contest
WHERE start_time < NOW() AND end_time > NOW()
  AND (title LIKE sqlc.arg(noip_keyword) OR (contest_type & 20) > 0);

-- name: CountNews :one
-- 新闻计数（分页用）
SELECT COUNT(news_id) AS ids FROM news
WHERE defunct != 'Y' AND title != sqlc.arg(faqs_title);

-- name: GetNewsList :many
-- 新闻列表（首页）
SELECT news_id, title, time FROM news
WHERE defunct != 'Y' AND title != 'faqs.cn'
ORDER BY importance DESC, time DESC
LIMIT 10;

-- name: GetMonthStarBoundary :one
-- 本月之星边界 solution_id（本月第一条提交之前）
SELECT solution_id FROM solution
WHERE in_date < DATE_ADD(CURDATE(), INTERVAL -DAY(CURDATE())+1 DAY)
ORDER BY solution_id DESC LIMIT 1;

-- name: GetMonthStarRank :many
-- 本月之星排名（排除隐藏用户）
SELECT user_id, nick, COUNT(DISTINCT problem_id) AS ac
FROM solution
WHERE solution_id > sqlc.arg(boundary_id)
  AND problem_id > 0
  AND result = 4
  AND first_time = 1
  AND user_id NOT IN (sqlc.arg(hidden_users))
GROUP BY user_id, nick
ORDER BY ac DESC
LIMIT 10;

-- name: GetRecentProblems :many
-- 最近题目（用户未通过的题目）
SELECT p.problem_id, p.title, s.max_in_date
FROM (
  SELECT problem_id, MIN(result) AS best, MAX(in_date) AS max_in_date
  FROM solution
  WHERE user_id = sqlc.arg(user_id) AND result >= 4 AND problem_id > 0
  GROUP BY problem_id
) s
INNER JOIN problem p ON s.problem_id = p.problem_id
WHERE s.best > 4
ORDER BY s.max_in_date DESC
LIMIT 5;

-- name: GetRecentContests :many
-- 最近比赛
SELECT contest_id, title, start_time FROM contest
WHERE defunct = 'N'
ORDER BY contest_id DESC
LIMIT 5;

-- name: GetChartDataAll :many
-- 图表数据（全部提交）
SELECT DATE(in_date) AS md, COUNT(1) AS c
FROM (
  SELECT in_date FROM solution
  WHERE solution_id > sqlc.arg(last_id)
    AND result < 13 AND problem_id > 0 AND result >= 4
) solution
GROUP BY md
ORDER BY md DESC
LIMIT 1000;

-- name: GetChartDataAC :many
-- 图表数据（AC 提交）
SELECT DATE(in_date) AS md, COUNT(1) AS c
FROM (
  SELECT in_date FROM solution
  WHERE solution_id > sqlc.arg(last_id)
    AND result = 4 AND problem_id > 0
) solution
GROUP BY md
ORDER BY md DESC
LIMIT 1000;

-- name: GetSubmitSpeed :one
-- 提交速度（管理员）
SELECT AVG(sp) AS sp FROM (
  SELECT COUNT(*) AS sp, judgetime DIV 3600 AS hour_slot
  FROM solution
  WHERE result > 3 AND solution_id > sqlc.arg(last_id)
  GROUP BY hour_slot
  ORDER BY sp
) tt;

-- name: GetLastSolutionID :one
-- 最近提交 ID（用于图表范围）
SELECT MIN(solution_id) AS id FROM solution
WHERE in_date >= NOW() - INTERVAL 8 DAY
UNION
SELECT MAX(solution_id) AS id FROM solution
ORDER BY id DESC LIMIT 1;

-- ── 门户（portal.php）────────────────────────────────────────────────

-- name: GetUserSubmittedProblems :many
-- 用户已提交/已通过的题目
SELECT problem_id, MIN(result) AS result
FROM solution
WHERE user_id = sqlc.arg(user_id) AND result >= 4
GROUP BY problem_id;

-- name: GetUserUnsolvedProblems :many
-- 用户未通过的题目
SELECT p.problem_id, p.title, p.source, p.accepted, p.submit
FROM problem p
INNER JOIN (
  SELECT DISTINCT s1.problem_id, s1.contest_id
  FROM solution s1
  WHERE s1.result != 4 AND s1.user_id = sqlc.arg(user_id)
    AND s1.problem_id NOT IN (
      SELECT DISTINCT s2.problem_id FROM solution s2
      WHERE s2.result = 4 AND s2.user_id = sqlc.arg(user_id)
    )
) f ON p.problem_id = f.problem_id;

-- name: GetUserContests :many
-- 用户参加的比赛
SELECT DISTINCT contest_id FROM solution
WHERE contest_id > 0 AND user_id = sqlc.arg(user_id);

-- name: GetActiveContests :many
-- 运行中的比赛
SELECT contest_id, title, start_time, end_time, private, user_id
FROM contest
WHERE defunct = 'N'
  AND start_time < NOW() AND end_time > NOW()
ORDER BY contest_id DESC;

-- ── 新闻详情（viewnews.php）─────────────────────────────────────────

-- name: GetNewsByID :many
-- 新闻详情
SELECT news_id, title, content, user_id, time, defunct, menu
FROM news
WHERE news_id = sqlc.arg(news_id)
  AND (defunct != 'Y' OR menu = 1)
ORDER BY importance ASC, time DESC
LIMIT 50;
