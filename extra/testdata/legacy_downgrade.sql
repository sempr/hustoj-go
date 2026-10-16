-- 测试专用：把 schema/schema.sql 建出的“新结构”回退成老版 HUSTOJ 结构。
--
-- 仅用于 CI 的升级路径用例：
--   schema.sql → 本文件 → extra/migrate.sh → check_schema 必须退 0。
-- 用来替代体积大、且属第三方的上游 install/db.sql，**不要在生产库执行**。
--
-- 覆盖 migrations/00002 会修复的每一类差异：引擎、排序规则、缺索引、
-- 列的类型/可空/默认/顺序，以及 00003 会重建的触发器。

SET NAMES utf8mb4;

DROP TRIGGER IF EXISTS firstAC;
DROP TRIGGER IF EXISTS simfilter;

-- ── 引擎：MyISAM（online 本就是 MEMORY，不动）──
ALTER TABLE `balloon`            ENGINE=MyISAM;
ALTER TABLE `compileinfo`        ENGINE=MyISAM;
ALTER TABLE `contest`            ENGINE=MyISAM;
ALTER TABLE `contest_problem`    ENGINE=MyISAM;
ALTER TABLE `custominput`        ENGINE=MyISAM;
ALTER TABLE `loginlog`           ENGINE=MyISAM;
ALTER TABLE `mail`               ENGINE=MyISAM;
ALTER TABLE `news`               ENGINE=MyISAM;
ALTER TABLE `openai_task_queue`  ENGINE=MyISAM;
ALTER TABLE `printer`            ENGINE=MyISAM;
ALTER TABLE `privilege`          ENGINE=MyISAM;
ALTER TABLE `problem`            ENGINE=MyISAM;
ALTER TABLE `reply`              ENGINE=MyISAM;
ALTER TABLE `runtimeinfo`        ENGINE=MyISAM;
ALTER TABLE `share_code`         ENGINE=MyISAM;
ALTER TABLE `sim`                ENGINE=MyISAM;
ALTER TABLE `solution`           ENGINE=MyISAM;
ALTER TABLE `solution_ai_answer` ENGINE=MyISAM;
ALTER TABLE `source_code`        ENGINE=MyISAM;
ALTER TABLE `source_code_user`   ENGINE=MyISAM;
ALTER TABLE `topic`              ENGINE=MyISAM;
ALTER TABLE `users`              ENGINE=MyISAM;

-- ── 排序规则：换成另一种（00002 会统一回 utf8mb4_general_ci）──
ALTER TABLE `balloon`            DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `compileinfo`        DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `contest`            DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `contest_problem`    DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `custominput`        DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `loginlog`           DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `mail`               DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `news`               DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `online`             DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `openai_task_queue`  DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `printer`            DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `privilege`          DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `problem`            DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `reply`              DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `runtimeinfo`        DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `share_code`         DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `sim`                DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `solution`           DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `solution_ai_answer` DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `source_code`        DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `source_code_user`   DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `topic`              DEFAULT COLLATE=utf8mb4_unicode_ci;
ALTER TABLE `users`              DEFAULT COLLATE=utf8mb4_unicode_ci;

-- ── 列的旧定义 ──
ALTER TABLE `contest` MODIFY COLUMN `langmask` int(11) NOT NULL DEFAULT 0;
ALTER TABLE `contest` MODIFY COLUMN `contest_type` smallint(5) unsigned NOT NULL DEFAULT 0;
ALTER TABLE `problem` MODIFY COLUMN `description` mediumtext DEFAULT NULL;
ALTER TABLE `problem` MODIFY COLUMN `input` mediumtext DEFAULT NULL;
ALTER TABLE `problem` MODIFY COLUMN `output` mediumtext DEFAULT NULL;
ALTER TABLE `problem` MODIFY COLUMN `hint` mediumtext DEFAULT NULL;
-- coin 在老版里排在 solved 之后、remote_oj 之前
ALTER TABLE `problem` MODIFY COLUMN `coin` int(11) NOT NULL DEFAULT 1 AFTER `solved`;
ALTER TABLE `reply` MODIFY COLUMN `author_id` varchar(48) NOT NULL;
ALTER TABLE `solution` MODIFY COLUMN `ip` char(46) NOT NULL;
ALTER TABLE `solution` MODIFY COLUMN `pass_rate` decimal(4,3) unsigned NOT NULL DEFAULT 0.000;
ALTER TABLE `topic` MODIFY COLUMN `author_id` varchar(48) NOT NULL;
ALTER TABLE `users` MODIFY COLUMN `coin_earned` int(11) NOT NULL DEFAULT 0;
ALTER TABLE `users` MODIFY COLUMN `coin_bonus` int(11) NOT NULL DEFAULT 0;
ALTER TABLE `users` MODIFY COLUMN `coin_spent` int(11) NOT NULL DEFAULT 0;

-- ── 去掉 00002 会补的索引 ──
ALTER TABLE `contest` DROP INDEX `key_c_def`;
ALTER TABLE `contest` DROP INDEX `key_c_end`;
ALTER TABLE `contest` DROP INDEX `key_c_dend`;
ALTER TABLE `problem` DROP INDEX `key_p_def`;
ALTER TABLE `sim` DROP INDEX `Index_sim_id`;
ALTER TABLE `users` DROP INDEX `idx_parent_phone`;
