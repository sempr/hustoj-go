-- Migration: {{ description }}
-- Created: {{ timestamp }}
-- Author: {{ author }}
-- Issue: {{ issue_ref }}

-- +goose Up
-- 在此编写向前迁移 SQL（只增不删数据）
-- 推荐使用幂等写法：ADD ... IF NOT EXISTS / MODIFY COLUMN

-- 示例：
-- ALTER TABLE `users` ADD COLUMN IF NOT EXISTS `avatar` varchar(255) DEFAULT NULL COMMENT '头像URL' AFTER `nick`;
-- ALTER TABLE `problem` ADD INDEX IF NOT EXISTS `idx_title` (`title`);

-- 大表（solution/source_code 等）加索引/改字段建议低峰执行，必要时分批
-- ALTER TABLE `solution` ADD INDEX IF NOT EXISTS `idx_xxx` (`col`);

-- +goose Down
-- 在此编写回滚 SQL（对应 Up 的逆操作）
-- 若无法安全回滚（如 DROP COLUMN 会丢数据），请写明原因并留空或 SELECT 1;

-- 示例：
-- ALTER TABLE `users` DROP COLUMN IF EXISTS `avatar`;
-- ALTER TABLE `problem` DROP INDEX IF EXISTS `idx_title`;

-- 无法回滚时：
-- SELECT 1; -- 此迁移不可回滚，回滚需从备份恢复