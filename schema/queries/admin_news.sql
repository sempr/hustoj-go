-- 后台公告管理查询（admin/news_list.php、news_add_page.php、news_add.php、
-- news_edit.php、news_df_change.php）。
--
-- 与 PHP 等价要点：
--   - news_list 的计数是 COUNT('news_id')（字符串常量等值于 COUNT(*)）。
--   - 列表固定 25 条/页，按 news_id DESC；带关键词时 PHP **不加 LIMIT**（全量返回），
--     这里保持一致，否则搜索结果会被截断。
--   - 搜索是裸 LIKE：用户输入的 % 与 _ 就是通配符（PHP 的 `like ?` 未转义）。
--   - 详情查询**不能在 SQL 层过滤 defunct**：管理员要能编辑已停用(Off)的公告，
--     因此不使用 homepage.sql 的 GetNewsByID（那条带 defunct!='Y' OR menu=1 过滤）。

-- name: CountNewsAdmin :one
SELECT COUNT(*) AS ids FROM news;

-- name: ListNewsAdmin :many
SELECT news_id, user_id, title, time, defunct, importance, menu
FROM news
ORDER BY news_id DESC
LIMIT ? OFFSET ?;

-- name: SearchNewsAdmin :many
SELECT news_id, user_id, title, time, defunct, importance, menu
FROM news
WHERE (title LIKE sqlc.arg(title_like) OR content LIKE sqlc.arg(content_like))
ORDER BY news_id DESC;

-- name: GetNewsAdmin :one
SELECT news_id, user_id, title, content, time, defunct, importance, menu
FROM news
WHERE news_id = sqlc.arg(news_id)
LIMIT 1;

-- name: InsertNews :exec
INSERT INTO news(user_id, title, content, time, menu) VALUES(?, ?, ?, now(), ?);

-- name: UpdateNews :exec
UPDATE news SET title=?, time=now(), content=?, user_id=?, menu=? WHERE news_id=?;

-- name: SetNewsDefunct :exec
UPDATE news SET defunct=? WHERE news_id=?;
