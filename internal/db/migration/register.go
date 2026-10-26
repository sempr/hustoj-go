package migration

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/pressly/goose/v3"
)

// RegisterGoMigrations 注册所有基于 Go 的迁移
// 在应用启动时或 main.go 中调用一次即可
func RegisterGoMigrations() {
	// goose 通过 init() 自动注册，此函数仅作显式导入触发
	_ = upAddUserCoinHistory
	_ = downAddUserCoinHistory
	_ = upBackfillProblemStats
	_ = downBackfillProblemStats
}

// RunGooseUp 程序化执行迁移（替代 CLI）
// 适用于：应用启动时自动迁移、集成测试、部署脚本
func RunGooseUp(db *sql.DB, migrationsDir string) error {
	// 设置迁移目录（包含 SQL 与 Go 迁移）
	goose.SetBaseFS(nil) // 使用文件系统
	if err := goose.SetDialect("mysql"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}

	// 可选：自定义版本表名（默认 schema_migrations，与现有一致）
	// goose.SetTableName("schema_migrations")

	log.Printf("开始执行数据库迁移 (dir: %s)", migrationsDir)
	if err := goose.Up(db, migrationsDir); err != nil {
		return fmt.Errorf("goose up: %w", err)
	}
	log.Println("数据库迁移完成")
	return nil
}

// RunGooseDown 回滚最后一条迁移
func RunGooseDown(db *sql.DB, migrationsDir string) error {
	if err := goose.SetDialect("mysql"); err != nil {
		return fmt.Errorf("set dialect: %w", err)
	}
	log.Println("回滚最后一条迁移")
	if err := goose.Down(db, migrationsDir); err != nil {
		return fmt.Errorf("goose down: %w", err)
	}
	return nil
}

// GetMigrationStatus 获取迁移状态（用于健康检查/监控）
func GetMigrationStatus(db *sql.DB, migrationsDir string) ([]*goose.Migration, error) {
	if err := goose.SetDialect("mysql"); err != nil {
		return nil, fmt.Errorf("set dialect: %w", err)
	}
	return goose.GetDBVersion(db)
}