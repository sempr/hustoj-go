-- 提交入库相关查询
--
-- 对应 phpweb/submit.php。这些写入必须与 PHP 逐条等价，原因有三：
--
--   1. 提交记录会被 judged-go 直接读取判题（fetcher 按 result<2 取件），
--      任何字段差异都会改变判题结果或让记录卡住不再被取走。
--   2. 迁移期 PHP 与 Go 同时在写同一批 solution 行，status 页、榜单、
--      题目 AC/提交计数都直接读这些列。两端语义不一致 = 用户看到
--      "在 PHP 提交的题在 Go 侧消失"，或者提交数不增长。
--   3. 插入顺序（先 result=14，源码落库后才置 0）是有竞态保护的，
--      见 InsertSolution 的说明，不能合并成一步。

-- ── 提交前的准入查询 ────────────────────────────────────────────────

-- 待判题队列长度。submit.php:26 用它决定是否对提交也启用图形验证码：
-- `select count(1) cnt FROM solution WHERE result<4`，超过 50 就开验证码。
-- 这是队列堆积时的自保护，Go 侧等价实现。
-- name: CountPendingJudges :one
SELECT count(1) FROM solution WHERE result < 4;

-- 练习模式取题。submit.php:69-72：
--   select problem_id, defunct from problem where problem_id=?
-- 管理员/题目编辑/题目审核可提交已删除题目（不加 defunct 条件），
-- 所以 defunct 交给 Go 侧判定，不在此处过滤。
-- name: GetSubmitProblem :one
SELECT problem_id, defunct FROM problem WHERE problem_id = sqlc.arg(problem_id);

-- 比赛模式取题。submit.php:65-67 按 (cid, num) 查，defunct 恒为 'N'
-- （原 SQL 字面量写死），因此这里不返回 defunct。
-- name: GetContestSubmitProblem :one
SELECT problem_id
FROM contest_problem
WHERE contest_id = sqlc.arg(contest_id)
  AND num = sqlc.arg(num);

-- 比赛信息，用于时间窗、私有性与语言掩码。submit.php:99 要求比赛正在进行
-- （start_time<=now AND end_time>now），查不到即"不在比赛中"。
-- name: GetContestForSubmit :one
SELECT private, langmask, title
FROM contest
WHERE contest_id = sqlc.arg(contest_id)
  AND start_time <= sqlc.arg(now)
  AND end_time > sqlc.arg(now);

-- 私有比赛的受邀判定。submit.php:120：
--   select count(*) from privilege where user_id=? and (rightstr=? or rightstr=?)
-- 两个 rightstr 分别是 "c<cid>"（参赛邀请）与 "m<cid>"（管理邀请）。
-- 用参数绑定而非拼字符串，cid 来自用户输入，拼字符串等于把 rightstr
-- 的内容交给用户构造。
-- name: CountContestInvites :one
SELECT count(*)
FROM privilege
WHERE user_id = sqlc.arg(user_id)
  AND (rightstr = sqlc.arg(rightstr_contest) OR rightstr = sqlc.arg(rightstr_manage));

-- SPJ 类型。submit.php:245 用于给 PHP 6（PHP 语言）补 coding 头。
-- name: GetProblemSPJ :one
SELECT spj FROM problem WHERE problem_id = sqlc.arg(problem_id);

-- 取用户昵称，写入 solution.nick 以便列表页免 JOIN users。
-- name: GetUserNick :one
SELECT nick FROM users WHERE user_id = sqlc.arg(user_id);

-- 提交冷却。submit.php:286：
--   select in_date, solution_id from solution
--    where user_id=? and in_date>? order by in_date desc limit 1
-- 命中时 PHP 不报错，直接把用户重定向到自己的 status 页
-- （ajax 模式则回 -1）。这是防刷屏与防 WAF 抽风的机制。
-- name: GetLastRecentSubmit :one
SELECT solution_id
FROM solution
WHERE user_id = sqlc.arg(user_id)
  AND in_date > sqlc.arg(since)
ORDER BY in_date DESC
LIMIT 1;

-- contest_locked($cid, $level)（my_func.inc.php:210）。
-- 命中条件：比赛进行中，且（contest_type 含 level 位 或 标题含关键字）。
-- submit.php 用它判断 128（延时）与 16（NOIP 只留最后一次提交）两种模式。
-- name: IsContestLocked :one
SELECT count(1) > 0
FROM contest
WHERE contest_id = sqlc.arg(contest_id)
  AND (contest_type & sqlc.arg(level) > 0 OR title LIKE sqlc.arg(keyword))
  AND start_time < sqlc.arg(now)
  AND end_time > sqlc.arg(now);

-- 远程评测题目。submit.php:372-379 命中则把 result 置 16（远程等待），
-- 不进本地判题队列。
-- name: GetProblemRemoteOJ :one
SELECT remote_oj FROM problem WHERE problem_id = sqlc.arg(problem_id);

-- 投毒计数（$OJ_POISON_BOT_COUNT）。submit.php:392 统计该用户该题的 AC 数，
-- 达到阈值后判题机开始返回随机结果，把算力让给真人。
-- name: CountUserAccepted :one
SELECT count(1)
FROM solution
WHERE user_id = sqlc.arg(user_id)
  AND result = 4
  AND problem_id = sqlc.arg(problem_id);

-- ── 写入 ────────────────────────────────────────────────────────────
--
-- 注意：solution / source_code_user / source_code / custominput 四张表的
-- 写入，以及把 result 从 14 推进到 0 的那一步，**刻意不放这里**。
--
-- 它们必须在同一个事务里，且 insert 之后要拿自增主键。sqlc 的 :one 要求
-- 查询带 RETURNING，而 MySQL 的 INSERT 没有 RETURNING（codegen 会直接
-- 报 "specifies parameter :one without containing a RETURNING clause"），
-- 所以这组写入手写在 pkg/repository/submit.go，SQL 语义在此说明。
--
-- 把顺序拆到两处实现的诱惑很大（"就一条 UPDATE 而已"），但 result=14→0
-- 这一步是"可以判了"的开关，必须和源码落库原子生效。判题机按 result<2
-- 取件，拆开就会出现"行已可判、源码还没写"的窗口，编译出空程序。

-- 题目提交计数。PHP 只在非 test_run 时 +1。
-- name: IncrementProblemSubmit :exec
UPDATE problem SET submit = submit + 1 WHERE problem_id = sqlc.arg(problem_id);

-- 比赛题目提交计数。
-- name: IncrementContestProblemSubmit :exec
UPDATE contest_problem SET c_submit = c_submit + 1
WHERE contest_id = sqlc.arg(contest_id) AND num = sqlc.arg(num);


-- 被降级后需要重算题目计数。submit.php:337-341 用一条
-- `UPDATE problem p INNER JOIN (SELECT count(1) ...) s` 同时算和写。
--
-- 这里拆成"先 SELECT 计数，再 UPDATE 单表"两步，原因有二：
--   1. sqlc 的 MySQL 解析器（dolphin）在"多表 UPDATE + 派生表"上会直接
--      panic（expected range var），codegen 过不去。schema/queries/problem.sql
--      里也记录了同类解析缺陷的绕法。
--   2. 语义上更安全：降级通常不命中任何行（PHP 用 $delete > 0 判断），
--      拆开后可以直接跳过写库，避免为一次无变化的提交多改一行 problem。
--
-- 顺序有语义：accepted 只数 result=4，submit 数全部提交，两次都要重算。
-- name: CountAcceptedForProblem :one
SELECT count(1) FROM solution WHERE problem_id = sqlc.arg(problem_id) AND result = 4;

-- name: CountSubmitsForProblem :one
SELECT count(1) FROM solution WHERE problem_id = sqlc.arg(problem_id);

-- name: SetProblemAccepted :exec
UPDATE problem SET accepted = sqlc.arg(accepted) WHERE problem_id = sqlc.arg(problem_id);

-- name: SetProblemSubmit :exec
UPDATE problem SET submit = sqlc.arg(submit) WHERE problem_id = sqlc.arg(problem_id);
