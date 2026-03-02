package repository

import (
	"database/sql"
	"fmt"
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

// NewDatabase creates a new database connection
func NewDatabase(cfg *config.DatabaseConfig) (*Database, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8",
		cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Name)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	if err = db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if _, err = db.Exec("SET NAMES utf8"); err != nil {
		return nil, fmt.Errorf("failed to set UTF8: %w", err)
	}

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
