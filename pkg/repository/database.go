package repository

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"github.com/sempr/hustoj-go/pkg/config"
)

// Solution represents a solution record
type Solution struct {
	ID        int
	ProblemID int
	UserID    string
	Language  int
	ContestID int
}

// Problem represents a problem record
type Problem struct {
	ID        int
	TimeLimit float64
	MemLimit  int
	SPJ       int
}

// Database handles all database operations
type Database struct {
	db *sql.DB
}

// DSN 构造 go-sql-driver 的连接串。
//
// 显式指定 collation，与 PHP 侧保持一致。
//
// 背景：23 张表与库默认均为 utf8mb4_general_ci，但 PHP 的
// "set names utf8mb4" 不带 COLLATE，取的是服务器默认的 utf8mb4_uca1400_ai_ci；
// Go 侧不指定 collation 时同样落到服务器默认值，于是两边"碰巧一致"。
// 这依赖服务器默认值，一旦换机器或改 MariaDB 配置就会漂移，故显式写死。
//
// 选 general_ci 而非 uca1400 的原因：表定义就是 general_ci。
// 比较结果两者一致（MariaDB 对 coercible 列做提升），但排序顺序不同，
// 而 ORDER BY 直接决定题目列表、用户昵称榜的输出顺序。
// 已用真实数据逐列比对（problem.title / users.nick / contest.title，
// 含正序与倒序）：当前数据集下两种 collation 结果完全一致，
// 因此统一到 general_ci 是行为平价的改动。
//
// 注意 MariaDB 的 UCA 14.0.0 系列（如 utf8mb4_uca1400_ai_ci）无法写进 DSN：
// go-sql-driver 内置的 collation→ID 映射只覆盖 MySQL，缺这批条目，
// 传入会报 unknown collation。general_ci 两侧都有，故可安全下发。
//
// parseTime / loc 供读取 datetime 列：loc=Local 让驱动按进程本地时区解析
// DATETIME，与写入侧同一时区语义。
//
// time_zone 是会话级设置，决定 SQL 函数 NOW()/CURDATE()/DATE() 用哪个时区。
// 它必须与进程的 TZ 一致，否则"同一次提交"在写入与展示之间会差一个时区。
// 取值不在代码里写死，按以下优先级解析（见 sessionTimeZone）：
//
//  1. 环境变量 HUSTOJ_DB_TIME_ZONE 显式指定（可给名字如 Asia/Shanghai，
//     也可给偏移如 +08:00），用于 DB 会话时区与进程 TZ 刻意不同的场景；
//  2. 否则取进程本地时区（由 TZ 环境变量 / /etc/localtime 决定）的当前偏移。
//
// PHP 侧对应 include/init.php:147-148 的 date_default_timezone_set 与
// SET time_zone='+8:00'，两者都是"部署配置"，因此 Go 侧也从环境取，
// 不把某个时区焊死在代码里。
func DSN(cfg config.DatabaseConfig) string {
	return dsn(cfg, sessionTimeZone())
}

// dsn 是 DSN 的可测内核：会话时区作为参数传入，便于断言各种输入。
func dsn(cfg config.DatabaseConfig, sessionTZ string) string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&collation=utf8mb4_general_ci&parseTime=true&loc=Local&time_zone=%s",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Name,
		url.QueryEscape("'"+strings.Trim(strings.TrimSpace(sessionTZ), "'")+"'"),
	)
}

// dbTimeZoneEnv 可显式指定会话时区。正常部署无需设置：TZ 已经够了。
//
// 设置它表示"SQL 会话时区与进程 TZ 刻意不同"，例如 TZ 用 Etc/UTC 跑容器、
// 而业务时区固定在别处。仅在确认 parseTime 的 loc=Local 仍符合预期时使用。
const dbTimeZoneEnv = "HUSTOJ_DB_TIME_ZONE"

// sessionTimeZone 解析要下发给 MySQL 的会话时区。
//
// 默认跟随进程本地时区，因此只要设置了 TZ（或系统 /etc/localtime 是目标
// 时区），SQL 侧与 Go 侧就自动一致，代码里不出现任何具体时区。
//
// 未显式给名字时下发固定偏移而不是时区名：MySQL/MariaDB 只有加载了
// mysql.time_zone* 系统表才认时区名，否则 SET time_zone 报错会让整条连接
// 不可用。偏移取当前时刻实际生效的值 —— 对无夏令时的地区（如 Asia/Shanghai）
// 恒定；若部署到有夏令时的地区，跨夏令时会偏移一小时，应改用
// HUSTOJ_DB_TIME_ZONE 显式指定固定偏移。
func sessionTimeZone() string {
	if v := strings.TrimSpace(os.Getenv(dbTimeZoneEnv)); v != "" {
		return v
	}
	_, offset := time.Now().In(time.Local).Zone()
	sign := "+"
	if offset < 0 {
		sign, offset = "-", -offset
	}
	return fmt.Sprintf("%s%02d:%02d", sign, offset/3600, (offset%3600)/60)
}

// NewDatabase creates a new database connection
func NewDatabase(cfg *config.DatabaseConfig) (*Database, error) {
	db, err := sql.Open("mysql", DSN(*cfg))
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	if err = db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	// 字符集由 DSN 的 charset 在握手时逐连接设定。
	// 此处不再执行 SET NAMES：连接池有 10 条连接，Exec 只会作用于其中任意一条，
	// 其余连接仍停留在默认字符集，会导致偶发乱码与"同样的 SQL 时好时坏"。
	return &Database{db: db}, nil
}

// Close closes the database connection
func (d *Database) Close() error {
	return d.db.Close()
}

// GetSolution retrieves solution information
func (d *Database) GetSolution(solutionID int) (*Solution, error) {
	query := "SELECT problem_id, user_id, language, contest_id FROM solution WHERE solution_id = ?"
	var nullCID sql.NullInt64

	solution := &Solution{ID: solutionID}
	err := d.db.QueryRow(query, solutionID).Scan(&solution.ProblemID, &solution.UserID, &solution.Language, &nullCID)
	if err != nil {
		return nil, fmt.Errorf("failed to get solution info: %w", err)
	}

	if nullCID.Valid {
		solution.ContestID = int(nullCID.Int64)
	}

	return solution, nil
}

// GetProblem retrieves problem information
func (d *Database) GetProblem(problemID int) (*Problem, error) {
	query := "SELECT time_limit, memory_limit, spj FROM problem WHERE problem_id = ?"

	problem := &Problem{ID: problemID}
	err := d.db.QueryRow(query, problemID).Scan(&problem.TimeLimit, &problem.MemLimit, &problem.SPJ)
	if err != nil {
		return nil, fmt.Errorf("failed to get problem info: %w", err)
	}

	return problem, nil
}

// GetSolutionSource retrieves the source code for a solution
func (d *Database) GetSolutionSource(solutionID int) (string, error) {
	query := "SELECT source FROM source_code WHERE solution_id = ?"

	var source string
	err := d.db.QueryRow(query, solutionID).Scan(&source)
	if err != nil {
		return "", fmt.Errorf("failed to get solution source: %w", err)
	}

	return source, nil
}

// UpdateSolution updates solution status and results
func (d *Database) UpdateSolution(solutionID, result, timeUsed, memoryUsed int, passRate float64) error {
	query := "UPDATE solution SET result=?, time=?, memory=?, pass_rate=?, judger=?, judgetime=now() WHERE solution_id=?"
	_, err := d.db.Exec(query, result, timeUsed, memoryUsed, passRate, "go_judger", solutionID)
	if err != nil {
		return fmt.Errorf("failed to update solution: %w", err)
	}
	return nil
}

// UpdateUserStats updates user solve and submit statistics
func (d *Database) UpdateUserStats(userID string) error {
	queries := []string{
		"UPDATE `users` SET `solved`=(SELECT count(DISTINCT `problem_id`) FROM `solution` s WHERE s.`user_id`=? AND s.`result`=4 AND problem_id>0 AND problem_id NOT IN (SELECT problem_id FROM contest_problem WHERE contest_id IN (SELECT contest_id FROM contest WHERE contest_type & 16 > 0 AND end_time>now()))) WHERE `user_id`=?",
		"UPDATE `users` SET `submit`=(SELECT count(DISTINCT `problem_id`) FROM `solution` s WHERE s.`user_id`=? AND problem_id>0 AND problem_id NOT IN (SELECT problem_id FROM contest_problem WHERE contest_id IN (SELECT contest_id FROM contest WHERE contest_type & 16 > 0 AND end_time>now()))) WHERE `user_id`=?",
	}

	for _, query := range queries {
		if _, err := d.db.Exec(query, userID, userID); err != nil {
			return fmt.Errorf("failed to update user stats: %w", err)
		}
	}

	return nil
}

// UpdateProblemStats updates problem statistics
func (d *Database) UpdateProblemStats(problemID, contestID int) error {
	if contestID > 0 {
		queries := []string{
			"UPDATE `contest_problem` SET `c_accepted`=(SELECT count(*) FROM `solution` WHERE `problem_id`=? AND `result`=4 AND contest_id=?) WHERE `problem_id`=? AND contest_id=?",
			"UPDATE `contest_problem` SET `c_submit`=(SELECT count(*) FROM `solution` WHERE `problem_id`=? AND contest_id=?) WHERE `problem_id`=? AND contest_id=?",
		}

		for _, query := range queries {
			if _, err := d.db.Exec(query, problemID, contestID, problemID, contestID); err != nil {
				return fmt.Errorf("failed to update contest problem stats: %w", err)
			}
		}
	}

	query := "UPDATE `problem` SET `accepted`=(SELECT count(*) FROM `solution` s WHERE s.`problem_id`=? AND s.`result`=4 AND problem_id NOT IN (SELECT problem_id FROM contest_problem WHERE contest_id IN (SELECT contest_id FROM contest WHERE contest_type & 16 > 0 AND end_time>now()))) WHERE `problem_id`=?"
	if _, err := d.db.Exec(query, problemID, problemID); err != nil {
		return fmt.Errorf("failed to update problem stats: %w", err)
	}

	return nil
}

// AddCompileError adds compilation error information
func (d *Database) AddCompileError(solutionID int, message string) error {
	if _, err := d.db.Exec("DELETE FROM compileinfo WHERE solution_id=?", solutionID); err != nil {
		return fmt.Errorf("failed to delete old compile info: %w", err)
	}

	if _, err := d.db.Exec("INSERT INTO compileinfo VALUES(?, ?)", solutionID, message); err != nil {
		return fmt.Errorf("failed to insert compile info: %w", err)
	}

	return nil
}

// AddRuntimeInfo adds runtime error information
func (d *Database) AddRuntimeInfo(solutionID int, details string) error {
	if _, err := d.db.Exec("DELETE FROM runtimeinfo WHERE solution_id=?", solutionID); err != nil {
		return fmt.Errorf("failed to delete old runtime info: %w", err)
	}

	if _, err := d.db.Exec("INSERT INTO runtimeinfo VALUES(?, ?)", solutionID, details); err != nil {
		return fmt.Errorf("failed to insert runtime info: %w", err)
	}

	return nil
}
