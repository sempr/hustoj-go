-- 00002 存量库 → schema/schema.sql 结构对齐
--
-- 适用：老版 HUSTOJ 库（MyISAM 引擎、utf8mb4_uca1400_ai_ci 等排序规则、
-- 缺索引、若干列的类型/可空/默认/顺序偏差）。
--
-- 生成：extra/schema_diff.py（对比线上信息架构与 schema/schema.sql）。
--   DB_HOST=... DB_USER=... DB_PASS=... DB_NAME=... python3 extra/schema_diff.py
-- 复盘：应用后 `python3 extra/check_schema.py --ignore-comments` 应退 0。
--   （默认不含列注释对齐——注释是文档、不参与结构语义；要连注释一起对齐，
--    用 schema_diff.py --comments 重新生成并按新编号落入。）
--
-- 约定：全部 MariaDB 幂等写法，可中断、可重跑；只做 ALTER，不删数据、不删表。
--   * 引擎/排序规则、索引均为“无条件对齐 + IF NOT EXISTS”——已是目标值即 no-op，
--     不依赖起始状态恰好是某个已知旧版本。
--   * 注意：`solution` / `source_code` 是大表，若需真正改引擎会重建并加锁，
--     请在低峰执行；已是 InnoDB 的库建议改用 `migrate.sh --baseline`。

SET NAMES utf8mb4;

ALTER TABLE `balloon` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `balloon` ADD PRIMARY KEY IF NOT EXISTS (`balloon_id`);

ALTER TABLE `compileinfo` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `compileinfo` ADD PRIMARY KEY IF NOT EXISTS (`solution_id`);

ALTER TABLE `contest` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `contest` MODIFY COLUMN `langmask` int(10) unsigned NOT NULL DEFAULT 0 COMMENT 'bits for LANG to mask';
ALTER TABLE `contest` MODIFY COLUMN `contest_type` smallint(5) unsigned DEFAULT 0;
ALTER TABLE `contest` ADD INDEX IF NOT EXISTS `key_c_def` (`defunct`);
ALTER TABLE `contest` ADD INDEX IF NOT EXISTS `key_c_dend` (`defunct`,`end_time`);
ALTER TABLE `contest` ADD INDEX IF NOT EXISTS `key_c_end` (`end_time`);
ALTER TABLE `contest` ADD PRIMARY KEY IF NOT EXISTS (`contest_id`);

ALTER TABLE `contest_problem` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `contest_problem` ADD INDEX IF NOT EXISTS `Index_contest_id` (`contest_id`);

ALTER TABLE `custominput` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `custominput` ADD PRIMARY KEY IF NOT EXISTS (`solution_id`);

ALTER TABLE `loginlog` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `loginlog` ADD INDEX IF NOT EXISTS `user_log_index` (`user_id`,`time`);
ALTER TABLE `loginlog` ADD INDEX IF NOT EXISTS `user_time_index` (`user_id`,`time`);
ALTER TABLE `loginlog` ADD PRIMARY KEY IF NOT EXISTS (`log_id`);

ALTER TABLE `mail` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `mail` ADD INDEX IF NOT EXISTS `uid` (`to_user`);
ALTER TABLE `mail` ADD PRIMARY KEY IF NOT EXISTS (`mail_id`);

ALTER TABLE `news` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `news` ADD PRIMARY KEY IF NOT EXISTS (`news_id`);

ALTER TABLE `online` ENGINE=MEMORY, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `online` ADD PRIMARY KEY IF NOT EXISTS (`hash`);
ALTER TABLE `online` ADD UNIQUE KEY IF NOT EXISTS `hash` (`hash`);

ALTER TABLE `openai_task_queue` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `openai_task_queue` ADD INDEX IF NOT EXISTS `idx_status_create` (`status`,`create_date`);
ALTER TABLE `openai_task_queue` ADD INDEX IF NOT EXISTS `idx_user_status` (`user_id`,`status`);
ALTER TABLE `openai_task_queue` ADD PRIMARY KEY IF NOT EXISTS (`id`);

ALTER TABLE `printer` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `printer` ADD PRIMARY KEY IF NOT EXISTS (`printer_id`);

ALTER TABLE `privilege` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `privilege` ADD INDEX IF NOT EXISTS `user_id_index` (`user_id`);

ALTER TABLE `problem` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `problem` MODIFY COLUMN `problem_id` int(11) NOT NULL AUTO_INCREMENT FIRST;
ALTER TABLE `problem` MODIFY COLUMN `title` varchar(200) NOT NULL DEFAULT '' AFTER `problem_id`;
ALTER TABLE `problem` MODIFY COLUMN `description` mediumtext NOT NULL AFTER `title`;
ALTER TABLE `problem` MODIFY COLUMN `input` mediumtext NOT NULL AFTER `description`;
ALTER TABLE `problem` MODIFY COLUMN `output` mediumtext NOT NULL AFTER `input`;
ALTER TABLE `problem` MODIFY COLUMN `sample_input` text DEFAULT NULL AFTER `output`;
ALTER TABLE `problem` MODIFY COLUMN `sample_output` text DEFAULT NULL AFTER `sample_input`;
ALTER TABLE `problem` MODIFY COLUMN `spj` char(1) NOT NULL DEFAULT '0' AFTER `sample_output`;
ALTER TABLE `problem` MODIFY COLUMN `hint` mediumtext NOT NULL AFTER `spj`;
ALTER TABLE `problem` MODIFY COLUMN `source` varchar(100) DEFAULT NULL AFTER `hint`;
ALTER TABLE `problem` MODIFY COLUMN `in_date` datetime DEFAULT NULL AFTER `source`;
ALTER TABLE `problem` MODIFY COLUMN `time_limit` decimal(10,3) NOT NULL DEFAULT 0.000 AFTER `in_date`;
ALTER TABLE `problem` MODIFY COLUMN `memory_limit` int(11) NOT NULL DEFAULT 0 AFTER `time_limit`;
ALTER TABLE `problem` MODIFY COLUMN `defunct` char(1) NOT NULL DEFAULT 'N' AFTER `memory_limit`;
ALTER TABLE `problem` MODIFY COLUMN `coin` int(10) unsigned NOT NULL DEFAULT 1 COMMENT 'AC此题可得的金币数' AFTER `defunct`;
ALTER TABLE `problem` MODIFY COLUMN `accepted` int(11) DEFAULT 0 AFTER `coin`;
ALTER TABLE `problem` MODIFY COLUMN `submit` int(11) DEFAULT 0 AFTER `accepted`;
ALTER TABLE `problem` MODIFY COLUMN `solved` int(11) DEFAULT 0 AFTER `submit`;
ALTER TABLE `problem` MODIFY COLUMN `remote_oj` varchar(16) DEFAULT NULL AFTER `solved`;
ALTER TABLE `problem` MODIFY COLUMN `remote_id` varchar(32) DEFAULT NULL AFTER `remote_oj`;
ALTER TABLE `problem` ADD INDEX IF NOT EXISTS `key_p_def` (`defunct`);
ALTER TABLE `problem` ADD PRIMARY KEY IF NOT EXISTS (`problem_id`);

ALTER TABLE `reply` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `reply` MODIFY COLUMN `author_id` varchar(48) NOT NULL DEFAULT '' COMMENT 'user_id';
ALTER TABLE `reply` ADD INDEX IF NOT EXISTS `author_id` (`author_id`);
ALTER TABLE `reply` ADD PRIMARY KEY IF NOT EXISTS (`rid`);

ALTER TABLE `runtimeinfo` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `runtimeinfo` ADD PRIMARY KEY IF NOT EXISTS (`solution_id`);

ALTER TABLE `share_code` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `share_code` ADD PRIMARY KEY IF NOT EXISTS (`share_id`);

ALTER TABLE `sim` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `sim` ADD INDEX IF NOT EXISTS `Index_sim_id` (`sim_s_id`);
ALTER TABLE `sim` ADD PRIMARY KEY IF NOT EXISTS (`s_id`);

ALTER TABLE `solution` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `solution` MODIFY COLUMN `ip` char(46) NOT NULL DEFAULT '';
ALTER TABLE `solution` MODIFY COLUMN `pass_rate` decimal(4,3) NOT NULL DEFAULT 0.000;
ALTER TABLE `solution` ADD INDEX IF NOT EXISTS `cid` (`contest_id`);
ALTER TABLE `solution` ADD INDEX IF NOT EXISTS `fst` (`first_time`);
ALTER TABLE `solution` ADD INDEX IF NOT EXISTS `idx_cid_result_num_sid` (`contest_id`,`result`,`num`,`solution_id`);
ALTER TABLE `solution` ADD INDEX IF NOT EXISTS `idx_contest_num` (`contest_id`,`num`,`result`);
ALTER TABLE `solution` ADD INDEX IF NOT EXISTS `idx_contest_result` (`contest_id`,`result`);
ALTER TABLE `solution` ADD INDEX IF NOT EXISTS `idx_contest_user_id` (`contest_id`,`user_id`,`solution_id`);
ALTER TABLE `solution` ADD INDEX IF NOT EXISTS `idx_solution_in_date` (`in_date`);
ALTER TABLE `solution` ADD INDEX IF NOT EXISTS `idx_uid_pid_res` (`user_id`,`problem_id`,`result`);
ALTER TABLE `solution` ADD INDEX IF NOT EXISTS `idx_uid_pid` (`user_id`,`problem_id`);
ALTER TABLE `solution` ADD INDEX IF NOT EXISTS `pid` (`problem_id`);
ALTER TABLE `solution` ADD INDEX IF NOT EXISTS `res` (`result`);
ALTER TABLE `solution` ADD INDEX IF NOT EXISTS `uid` (`user_id`);
ALTER TABLE `solution` ADD PRIMARY KEY IF NOT EXISTS (`solution_id`);

ALTER TABLE `solution_ai_answer` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `solution_ai_answer` ADD PRIMARY KEY IF NOT EXISTS (`solution_id`);

ALTER TABLE `source_code` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `source_code` ADD PRIMARY KEY IF NOT EXISTS (`solution_id`);

ALTER TABLE `source_code_user` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `source_code_user` ADD PRIMARY KEY IF NOT EXISTS (`solution_id`);

ALTER TABLE `topic` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `topic` MODIFY COLUMN `author_id` varchar(48) NOT NULL DEFAULT '' COMMENT 'user_id';
ALTER TABLE `topic` ADD INDEX IF NOT EXISTS `cid` (`cid`,`pid`);
ALTER TABLE `topic` ADD PRIMARY KEY IF NOT EXISTS (`tid`);

ALTER TABLE `users` ENGINE=INNODB, DEFAULT CHARACTER SET utf8mb4 COLLATE=utf8mb4_general_ci;
ALTER TABLE `users` MODIFY COLUMN `coin_earned` int(10) unsigned NOT NULL DEFAULT 0 COMMENT '做题获得的积分';
ALTER TABLE `users` MODIFY COLUMN `coin_bonus` int(10) unsigned NOT NULL DEFAULT 0 COMMENT '老师奖励的积分';
ALTER TABLE `users` MODIFY COLUMN `coin_spent` int(10) unsigned NOT NULL DEFAULT 0 COMMENT '已消耗的积分';
ALTER TABLE `users` ADD INDEX IF NOT EXISTS `idx_parent_phone` (`parent_phone`);
ALTER TABLE `users` ADD PRIMARY KEY IF NOT EXISTS (`user_id`);
