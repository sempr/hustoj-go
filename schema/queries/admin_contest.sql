-- 后台比赛管理（admin/contest_list.php、contest_df_change.php、contest_pr_change.php、
-- contest_add.php、contest_edit.php）。
--
-- langmask 由 Go 侧按 `((1<<len(languageExt))-1) & ^selected` 算好后传入；
-- contest_type 是各复选框 1<<i 的按位或，同样在 Go 侧算好。

-- name: CountContestsAdmin :one
SELECT COUNT(*) AS ids FROM contest;

-- name: ListContestsAdmin :many
SELECT contest_id, title, start_time, end_time, private, defunct
FROM contest ORDER BY contest_id DESC LIMIT ? OFFSET ?;

-- name: SearchContestsAdmin :many
SELECT contest_id, title, start_time, end_time, private, defunct
FROM contest
WHERE (title LIKE sqlc.arg(kw_title) OR description LIKE sqlc.arg(kw_desc))
ORDER BY contest_id DESC;

-- name: GetContestDefunct :one
SELECT defunct FROM contest WHERE contest_id = sqlc.arg(contest_id) LIMIT 1;

-- name: GetContestPrivate :one
SELECT private FROM contest WHERE contest_id = sqlc.arg(contest_id) LIMIT 1;

-- name: GetContestPassword :one
SELECT password FROM contest WHERE contest_id = sqlc.arg(contest_id) LIMIT 1;

-- name: SetContestDefunct :exec
UPDATE contest SET defunct = sqlc.arg(defunct) WHERE contest_id = sqlc.arg(contest_id);

-- name: SetContestPrivate :exec
UPDATE contest SET private = sqlc.arg(private) WHERE contest_id = sqlc.arg(contest_id);

-- name: InsertContest :execresult
INSERT INTO contest (title, start_time, end_time, private, langmask, description, password, subnet, contest_type, user_id)
VALUES (sqlc.arg(title), sqlc.arg(start_time), sqlc.arg(end_time), sqlc.arg(private), sqlc.arg(langmask),
        sqlc.arg(description), sqlc.arg(password), sqlc.arg(subnet), sqlc.arg(contest_type), sqlc.arg(user_id));

-- name: UpdateContest :exec
UPDATE contest SET title=sqlc.arg(title), description=sqlc.arg(description), start_time=sqlc.arg(start_time),
  end_time=sqlc.arg(end_time), private=sqlc.arg(private), langmask=sqlc.arg(langmask),
  password=sqlc.arg(password), subnet=sqlc.arg(subnet), contest_type=sqlc.arg(contest_type)
WHERE contest_id=sqlc.arg(contest_id);

-- name: DeleteContestProblems :exec
DELETE FROM contest_problem WHERE contest_id = sqlc.arg(contest_id);

-- name: ListContestProblemIDsOrdered :many
SELECT problem_id FROM contest_problem WHERE contest_id = sqlc.arg(contest_id) ORDER BY num;

-- name: ListContestUserIDs :many
SELECT user_id FROM privilege WHERE rightstr = sqlc.arg(rightstr) ORDER BY user_id;

-- name: ResetSolutionNum :exec
UPDATE solution SET num = -1 WHERE contest_id = sqlc.arg(contest_id);

-- name: SetSolutionNum :exec
UPDATE solution SET num = sqlc.arg(num) WHERE contest_id = sqlc.arg(contest_id) AND problem_id = sqlc.arg(problem_id);

-- name: RecomputeContestProblemAccepted :exec
UPDATE contest_problem SET c_accepted = (SELECT count(1) FROM solution WHERE solution.problem_id=sqlc.arg(problem_id) AND solution.contest_id=sqlc.arg(contest_id) AND solution.result=4)
WHERE contest_problem.problem_id=sqlc.arg(problem_id) AND contest_problem.contest_id=sqlc.arg(contest_id);

-- name: RecomputeContestProblemSubmit :exec
UPDATE contest_problem SET c_submit = (SELECT count(1) FROM solution WHERE solution.problem_id=sqlc.arg(problem_id) AND solution.contest_id=sqlc.arg(contest_id))
WHERE contest_problem.problem_id=sqlc.arg(problem_id) AND contest_problem.contest_id=sqlc.arg(contest_id);
