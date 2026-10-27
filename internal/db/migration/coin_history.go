package migration

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/pressly/goose/v3"
)

// 初始化时注册所有 Go 迁移
func init() {
	goose.AddMigrationContext(upAddUserCoinHistory, downAddUserCoinHistory)
	goose.AddMigrationContext(upBackfillProblemStats, downBackfillProblemStats)
}

// upAddUserCoinHistory 示例：为用户金币变更添加历史记录表并回填初始数据
// 场景：新增 coin_history 表，需从 users.coin_earned/coin_spent 推导历史
func upAddUserCoinHistory(ctx context.Context, tx *sql.Tx) error {
	// 1. 创建历史记录表
	_, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS coin_history (
			id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
			user_id VARCHAR(48) NOT NULL,
			delta INT NOT NULL DEFAULT 0 COMMENT '变更量（正为得、负为消耗）',
			reason VARCHAR(64) NOT NULL DEFAULT '' COMMENT '变更原因：ac_reward/daily_bonus/admin_adjust/spend',
			ref_id BIGINT UNSIGNED DEFAULT NULL COMMENT '关联业务ID：solution_id/contest_id 等',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id),
			KEY idx_user_created (user_id, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci COMMENT='用户金币变更历史'
	`)
	if err != nil {
		return fmt.Errorf("create coin_history: %w", err)
	}

	// 2. 回填现有用户的 earned/spent 为两条初始记录
	// 使用事务保证一致性
	_, err = tx.ExecContext(ctx, `
		INSERT INTO coin_history (user_id, delta, reason, created_at)
		SELECT user_id, coin_earned, 'ac_reward', reg_time
		FROM users
		WHERE coin_earned > 0
	`)
	if err != nil {
		return fmt.Errorf("backfill earned: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		INSERT INTO coin_history (user_id, delta, reason, created_at)
		SELECT user_id, -coin_spent, 'spend', reg_time
		FROM users
		WHERE coin_spent > 0
	`)
	if err != nil {
		return fmt.Errorf("backfill spent: %w", err)
	}

	log.Println("coin_history 表创建并回填完成")
	return nil
}

func downAddUserCoinHistory(ctx context.Context, tx *sql.Tx) error {
	// 回滚：删除表（数据丢失，生产慎用）
	_, err := tx.ExecContext(ctx, `DROP TABLE IF EXISTS coin_history`)
	if err != nil {
		return fmt.Errorf("drop coin_history: %w", err)
	}
	log.Println("coin_history 表已删除（回滚）")
	return nil
}

// upBackfillProblemStats 示例：重算 problem.accepted/submit/solved
// 场景：历史数据不一致、或新增统计字段需全量重算
func upBackfillProblemStats(ctx context.Context, tx *sql.Tx) error {
	// 使用临时表避免锁住 problem 表太久
	_, err := tx.ExecContext(ctx, `
		CREATE TEMPORARY TABLE tmp_problem_stats (
			problem_id INT NOT NULL PRIMARY KEY,
			accepted INT NOT NULL DEFAULT 0,
			submit INT NOT NULL DEFAULT 0,
			solved INT NOT NULL DEFAULT 0
		) ENGINE=InnoDB
	`)
	if err != nil {
		return fmt.Errorf("create temp table: %w", err)
	}

	// 分批聚合（大表建议分批，这里简化）
	_, err = tx.ExecContext(ctx, `
		INSERT INTO tmp_problem_stats (problem_id, accepted, submit, solved)
		SELECT
			problem_id,
			SUM(CASE WHEN result = 4 THEN 1 ELSE 0 END) AS accepted,
			COUNT(*) AS submit,
			COUNT(DISTINCT user_id) AS solved
		FROM solution
		WHERE valid = 1
		GROUP BY problem_id
	`)
	if err != nil {
		return fmt.Errorf("aggregate stats: %w", err)
	}

	// 更新 problem 表
	_, err = tx.ExecContext(ctx, `
		UPDATE problem p
		JOIN tmp_problem_stats t ON p.problem_id = t.problem_id
		SET p.accepted = t.accepted,
			p.submit = t.submit,
			p.solved = t.solved
	`)
	if err != nil {
		return fmt.Errorf("update problem: %w", err)
	}

	log.Println("problem 统计字段重算完成")
	return nil
}

func downBackfillProblemStats(ctx context.Context, tx *sql.Tx) error {
	// 统计重算无法精确回滚，标记不可回滚
	log.Println("统计重算不可回滚，需从备份恢复")
	return nil // 或返回错误阻止回滚
}