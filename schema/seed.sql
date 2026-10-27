-- 全新库的最小种子数据：管理员账号 + administrator 权限。
-- 执行前需先导入 schema/schema.sql（建表）与 schema/triggers.sql。
--
-- 账号/口令可用 MySQL 会话变量覆盖，例如：
--   mysql <db> --init-command="SET @admin_user='root', @admin_password='s3cret'" < seed.sql
-- 未设置时默认为 admin / admin。
--
-- 口令按 HUSTOJ 旧版格式存 MD5(password)（32 位十六进制）。column 为
-- varchar(32)，恰好容纳；Go（internal/web/password.go 的 isOldPW 分支）与
-- PHP 两端都能校验。首次登录后应在后台改密（会转成新版加盐格式）。

SET @admin_user     = IFNULL(@admin_user, 'admin');
SET @admin_password = IFNULL(@admin_password, 'admin');

INSERT INTO users (user_id, password, reg_time, nick, email, defunct)
VALUES (@admin_user, MD5(@admin_password), NOW(), @admin_user, '', 'N')
ON DUPLICATE KEY UPDATE password = VALUES(password), defunct = 'N';

-- privilege 表无唯一键，用 NOT EXISTS 保证可重复执行不产生重复行。
INSERT INTO privilege (user_id, rightstr, valuestr, defunct)
SELECT @admin_user, 'administrator', 'true', 'N'
WHERE NOT EXISTS (
  SELECT 1 FROM privilege WHERE user_id = @admin_user AND rightstr = 'administrator'
);
