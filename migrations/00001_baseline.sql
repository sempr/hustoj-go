-- +goose Up
-- 迁移基线（marker migration）
--
-- 本库在引入 goose 之前就已存在，且正在被 PHP 与 judged-go 并发使用。
-- 因此 00001 **不重建任何表**：重建会在生产库上执行 DROP/CREATE，
-- 而 MyISAM 的 ALTER/DROP 与其它 22 张表上的判题写入之间没有事务保护，
-- 一旦中途失败将留下半迁移状态且无法回滚。
--
-- 本迁移的唯一作用是给 goose 一个起点，使后续迁移能从 00002 起编号。
-- 数据库结构的权威描述在 schema/schema.sql，本文件与其保持一致。
--
-- ── 为什么不把 schema.sql 直接当 00001 ──────────────────────────────────
-- 1. schema.sql 含 CREATE TABLE。在已存在的库上重放要么报 "table exists"，
--    要么就得加 IF NOT EXISTS —— 后者会掩盖"库里有表但结构已偏离"的情况，
--    正是绞杀期最需要暴露的问题。
-- 2. schema.sql 是给 sqlc 读的（含 SET NAMES、注释、刻意保留的旧式显示宽度），
--    并不是可直接执行的迁移脚本。两者混用会让"结构定义"与"变更历史"纠缠。
-- 3. 引擎转换（MyISAM → InnoDB）必须是一次独立、可中断、可重跑的迁移，
--    不能与基线混在一个事务里。
--
-- ── 结构一致性由谁保证 ──────────────────────────────────────────────────
-- extra/check_schema.sh：把生产库的实际结构与 schema/schema.sql 逐项比对
-- （表名、列名、列序、类型、可空、默认值、主键、索引、引擎、字符集）。
-- 该检查进 CI 与部署前门禁；不一致即视为阻断性问题。
--
-- +goose StatementBegin

-- 迁移基线不做任何变更，此处仅留下可执行的痕迹，
-- 使 goose 能正确登记版本 1。校验逻辑见 extra/check_schema.sh。
SELECT 1;

-- +goose StatementEnd

-- +goose Down
-- 基线不可回滚：它未修改任何结构。
-- +goose StatementBegin
SELECT 1;
-- +goose StatementEnd
