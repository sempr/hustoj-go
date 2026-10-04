-- 题目列表/详情查询
--
-- 这些查询刻意保持与 phpweb/problemset.php、phpweb/problem.php 的 SQL 等价，
-- 绞杀期两者必须给出完全相同的题目集合与顺序，否则用户会看到列表在切换
-- PHP/Go 后"少题""变顺序""AC 标记消失"。
--
-- 对应关系（problemset.php 的三种模式）：
--   管理员                → ListProblemsForAdmin / SearchProblemsForAdmin
--   $OJ_FREE_PRACTICE=1   → ListProblemsFreePractice / SearchProblemsFreePractice
--   普通用户              → ListProblemsForMember / SearchProblemsForMember
--
-- 不做成一个带 narg 开关的查询：三种模式的过滤条件不同，混在一起会让
-- EXPLAIN 的执行计划随参数漂移，且 review 时难以确认与 PHP 等价。
--
-- 分页固定 50 条（problemset.php 的 $page_cnt）。
--
-- 一条极易漏掉的等价性细节：problemset.php 的 `problem_id > 0` 只出现在
-- "无搜索且无 list"分支（第 91-93 行的 else）；一旦带 search，$filter_sql 被
-- 整体替换成 `( title like ? or source like ?)`，不再含该条件。所以无搜索的
-- 列表/计数查询要有它，搜索查询不能有 —— 加错或漏掉都不会报错，只会安静地
-- 多列或少列题目，只有对着真实库做等价性比对才能发现。

-- ── 管理员：可见全部题目，不加 defunct 过滤 ────────────────────────────
-- name: ListProblemsForAdmin :many
SELECT problem_id, title, source, submit, accepted, defunct, coin
FROM problem
WHERE problem_id > 0
ORDER BY problem_id
LIMIT ? OFFSET ?;

-- name: CountProblemsForAdmin :one
SELECT count(1) FROM problem WHERE problem_id > 0;

-- name: SearchProblemsForAdmin :many
SELECT problem_id, title, source, submit, accepted, defunct, coin
FROM problem
WHERE (title LIKE sqlc.arg(title_like) OR source LIKE sqlc.arg(source_like))
ORDER BY problem_id
LIMIT ? OFFSET ?;

-- name: CountSearchProblemsForAdmin :one
SELECT count(1) FROM problem
WHERE (title LIKE sqlc.arg(title_like) OR source LIKE sqlc.arg(source_like));

-- ── 自由练习：只看 defunct='N'，不排除比赛题 ────────────────────────────
-- name: ListProblemsFreePractice :many
SELECT problem_id, title, source, submit, accepted, defunct, coin
FROM problem
WHERE problem_id > 0
  AND defunct = 'N'
ORDER BY problem_id
LIMIT ? OFFSET ?;

-- name: CountProblemsFreePractice :one
SELECT count(1) FROM problem WHERE problem_id > 0 AND defunct = 'N';

-- name: SearchProblemsFreePractice :many
SELECT problem_id, title, source, submit, accepted, defunct, coin
FROM problem
WHERE defunct = 'N'
  AND (title LIKE sqlc.arg(title_like) OR source LIKE sqlc.arg(source_like))
ORDER BY problem_id
LIMIT ? OFFSET ?;

-- name: CountSearchProblemsFreePractice :one
SELECT count(1) FROM problem
WHERE defunct = 'N'
  AND (title LIKE sqlc.arg(title_like) OR source LIKE sqlc.arg(source_like));

-- ── 普通用户：排除正在进行比赛中的题目 ──────────────────────────────────
-- 与 PHP 的子查询逐字对应；PHP 把 now 字符串插值进 SQL，这里改为绑定参数，
-- 语义相同但不会因时间格式化失误产生注入或比较错误。
-- name: ListProblemsForMember :many
SELECT problem_id, title, source, submit, accepted, defunct, coin
FROM problem
WHERE problem_id > 0
  AND defunct = 'N'
  AND problem_id NOT IN (
    SELECT cp.problem_id
    FROM contest c
    INNER JOIN contest_problem cp ON c.contest_id = cp.contest_id
    WHERE c.defunct = 'N' AND sqlc.arg(now) < c.end_time
  )
ORDER BY problem_id
LIMIT ? OFFSET ?;

-- name: CountProblemsForMember :one
SELECT count(1)
FROM problem
WHERE problem_id > 0
  AND defunct = 'N'
  AND problem_id NOT IN (
    SELECT cp.problem_id
    FROM contest c
    INNER JOIN contest_problem cp ON c.contest_id = cp.contest_id
    WHERE c.defunct = 'N' AND sqlc.arg(now) < c.end_time
  );

-- 注意：下面两条查询的列必须写成 `p.xxx` 限定形式。
-- sqlc 的 MySQL 分析器在"title/source 的 OR 谓词"与"含子查询"同时出现时
-- 会误判 title 为歧义列（problem 与 contest_problem 都有 title 列），
-- 直接写 title 会报 `column reference "title" is ambiguous`。
-- 改成 NOT EXISTS 也绕不开（实测仍报），只能用表别名限定。
-- 这是 sqlc 的解析缺陷，SQL 本身在 MySQL 上完全合法。
-- 别图省事去掉别名限定，那样 codegen 会直接失败。

-- name: SearchProblemsForMember :many
SELECT p.problem_id, p.title, p.source, p.submit, p.accepted, p.defunct, p.coin
FROM problem p
WHERE p.defunct = 'N'
  AND (p.title LIKE sqlc.arg(title_like) OR p.source LIKE sqlc.arg(source_like))
  AND p.problem_id NOT IN (
    SELECT cp.problem_id
    FROM contest c
    INNER JOIN contest_problem cp ON c.contest_id = cp.contest_id
    WHERE c.defunct = 'N' AND sqlc.arg(now) < c.end_time
  )
ORDER BY p.problem_id
LIMIT ? OFFSET ?;

-- name: CountSearchProblemsForMember :one
SELECT count(1)
FROM problem p
WHERE p.defunct = 'N'
  AND (p.title LIKE sqlc.arg(title_like) OR p.source LIKE sqlc.arg(source_like))
  AND p.problem_id NOT IN (
    SELECT cp.problem_id
    FROM contest c
    INNER JOIN contest_problem cp ON c.contest_id = cp.contest_id
    WHERE c.defunct = 'N' AND sqlc.arg(now) < c.end_time
  );

-- ── 题目详情 ────────────────────────────────────────────────────────────
-- defunct 不在此处过滤：problem.php 对已删除题目会显示提示页，
-- 由路由层决定渲染什么，查询层保持中立。
-- name: GetProblemByID :one
SELECT p.problem_id, p.title, p.description, p.input, p.output,
       p.sample_input, p.sample_output, p.spj, p.hint, p.source, p.in_date,
       p.time_limit, p.memory_limit, p.defunct, p.coin,
       p.accepted, p.submit, p.solved, p.remote_oj, p.remote_id
FROM problem p
WHERE p.problem_id = sqlc.arg(problem_id);

-- ── 当前用户的完成状态标记 ──────────────────────────────────────────────
-- PHP：select problem_id, MIN(result) AS result FROM solution
--       WHERE user_id=? and result>=4 GROUP BY problem_id
-- result>=4 才是"已提交过"（含部分分）；result=4 才是 AC。
-- MIN() 很关键：同一题多次提交取最好成绩，Go 侧不能改成取最新一次。
-- MIN(result) 必须 CAST 成 SIGNED：直接写 MIN(result) 时 sqlc 无法确定
-- smallint 的 MIN 返回类型，会生成 interface{}，调用方没法和判罚码常量比较。
--
-- source LIKE ? 的参数被 sqlc 推成 sql.NullString（它沿用 source 列的可空性，
-- 加 COALESCE 也不改变推断）。SQL 保持与 PHP 的 `source like ?` 逐字一致，
-- 由调用方用 web.LikeArg() 包一层，不为了顺眼而改动查询语义。
-- name: GetUserBestResults :many
SELECT problem_id, CAST(MIN(result) AS SIGNED) AS result
FROM solution
WHERE user_id = sqlc.arg(user_id) AND result >= 4
GROUP BY problem_id;

-- ── 详情页的三模式可见性 ──────────────────────────────────────────────
--
-- 详情页的过滤比列表页更严，多一条 private='1'（problem.php:44）：
-- 成员模式下题目被「进行中的比赛」**或**「私有比赛」引用时都不能看，
-- 否则学生改 URL 就能提前看到考题。
--
-- 与列表页的另一处差异：详情页对 problem_id 没有 problem_id > 0 条件
-- （走的是 WHERE problem_id=? 而非 problemset.php 的分页分支），也不需要。
--
-- 关键：private 列是 tinyint，而 PHP 写的是 c.private='1'。
-- MariaDB 会做数值比较，'1' == 1 成立，所以这里也用 = 1，
-- 不能"顺手"改成 private = '1' 然后指望类型转换 —— 索引选择会变。

-- name: GetProblemForMember :one
SELECT p.problem_id, p.title, p.description, p.input, p.output,
       p.sample_input, p.sample_output, p.spj, p.hint, p.source, p.in_date,
       p.time_limit, p.memory_limit, p.defunct, p.coin,
       p.accepted, p.submit, p.solved, p.remote_oj, p.remote_id
FROM problem p
WHERE p.problem_id = sqlc.arg(problem_id)
  AND p.defunct = 'N'
  AND NOT EXISTS (
    SELECT 1
    FROM contest_problem cp
    INNER JOIN contest c ON cp.contest_id = c.contest_id
    WHERE cp.problem_id = sqlc.arg(problem_id)
      AND (c.end_time > sqlc.arg(now) AND c.defunct = 'N' OR c.private = 1)
  );

-- name: GetProblemFreePractice :one
SELECT p.problem_id, p.title, p.description, p.input, p.output,
       p.sample_input, p.sample_output, p.spj, p.hint, p.source, p.in_date,
       p.time_limit, p.memory_limit, p.defunct, p.coin,
       p.accepted, p.submit, p.solved, p.remote_oj, p.remote_id
FROM problem p
WHERE p.problem_id = sqlc.arg(problem_id)
  AND p.defunct = 'N';

-- ── 比赛模式的题目详情（/problem.php?cid=X&pid=Y）────────────────────
--
-- /problem.php 是练习与比赛两种模式的公共入口（problem.php:25 与 :53）。
-- 比赛模式的取题路径与练习模式完全不同：
--
--   1. contest-check.php 先做**不加过滤**的 SELECT *，再在 PHP 里逐条判断
--      （defunct / private / subnet / start_time）。所以这里也先无过滤取回，
--      把判断交给 Go 侧的 EvaluateContestAccess —— 复刻 PHP 的判断顺序比
--      把条件塞进 SQL 更好读，也更容易逐条写测试。
--
--   2. 题目按 contest_problem.num 取，而不是 problem_id（problem.php:95-97）。
--      这是外部可见的编号（1-based 展示顺序），与 problem_id 无关。
--
--   3. 练习模式过滤 defunct，但比赛模式**不过滤**：已删除题目仍要能在比赛里
--      复现，所以这里复用 GetProblemByID 的无过滤语义。

-- name: GetContestByID :one
SELECT contest_id, title, start_time, end_time, defunct,
       description, private, langmask, subnet, contest_type, user_id
FROM contest
WHERE contest_id = sqlc.arg(contest_id);

-- name: GetContestProblemID :one
SELECT problem_id
FROM contest_problem
WHERE contest_id = sqlc.arg(contest_id)
  AND num = sqlc.arg(num);

-- 私有比赛的受邀者。PHP 走 session _c<cid>，session 桥落地后
-- 改用这张表；contest-check.php:62-70 就是这个查询。
--
-- name: HasContestPrivilege :one
SELECT count(*) > 0
FROM privilege
WHERE user_id = sqlc.arg(user_id)
  AND rightstr = sqlc.arg(rightstr);

-- problem_locked($id, 28)：题目是否被某个"锁榜"比赛占用。
-- 命中时隐藏 AC 数与提交数（problem.php:158），NOIP 考试期间防止泄题。
--
-- name: IsProblemLocked :one
SELECT count(*) > 0
FROM contest c
INNER JOIN contest_problem cp ON c.contest_id = cp.contest_id AND cp.problem_id = sqlc.arg(problem_id)
WHERE (c.contest_type & sqlc.arg(level) > 0 OR c.title LIKE sqlc.arg(keyword))
  AND c.start_time < sqlc.arg(now)
  AND c.end_time > sqlc.arg(now);

-- ── 比赛列表与题目 ────────────────────────────────────────────────

-- name: ListContests :many
SELECT contest_id, title, start_time, end_time, defunct, description, private, langmask, password, contest_type, user_id, subnet
FROM contest
ORDER BY contest_id DESC;

-- name: GetContestProblems :many
SELECT cp.num, p.problem_id, p.title, p.source, p.coin
FROM contest_problem cp
INNER JOIN problem p ON cp.problem_id = p.problem_id
WHERE cp.contest_id = sqlc.arg(contest_id)
ORDER BY cp.num;
