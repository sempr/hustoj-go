package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	_ "github.com/go-sql-driver/mysql"
)

const prefetchMultiplier = 80

// JobFetcher defines the interface for fetching judge jobs.
type JobFetcher interface {
	GetJobs(maxJobs int) ([]int, error)
	CheckOut(solutionID int, result int) (bool, error)
	Close() error
}

// NewFetcher is a factory for creating the appropriate JobFetcher based on the config.
func NewFetcher(cfg *Config) (JobFetcher, error) {
	if cfg.HTTPJudge {
		// HTTP implementation would go here
		return nil, fmt.Errorf("HTTP fetcher is not implemented")
	}
	if cfg.RedisEnable {
		return NewRedisFetcher(cfg)
	}
	return NewMySQLFetcher(cfg)
}

// --- MySQL Fetcher ---
type MySQLFetcher struct {
	db        *sql.DB
	selectQuery string
}

func NewMySQLFetcher(cfg *Config) (*MySQLFetcher, error) {
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?parseTime=true",
		cfg.UserName, cfg.Password, cfg.HostName, cfg.PortNumber, cfg.DBName)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetConnMaxLifetime(time.Minute * 3)
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(10)

	if err := db.Ping(); err != nil {
		return nil, err
	}

	_, err = db.Exec("SET NAMES utf8")
	if err != nil {
		return nil, err
	}

	prefetchLimit := prefetchMultiplier * cfg.MaxRunning
	var query string
	if cfg.TotalJudges <= 1 {
		query = fmt.Sprintf(
			"SELECT solution_id FROM solution WHERE language in (%s) and result<2 ORDER BY result, solution_id limit %d",
			cfg.LangSet, prefetchLimit)
	} else {
		query = fmt.Sprintf(
			"SELECT solution_id FROM solution WHERE language in (%s) and result<2 and MOD(solution_id,%d)=%d ORDER BY result, solution_id ASC limit %d",
			cfg.LangSet, cfg.TotalJudges, cfg.JudgeMod, prefetchLimit)
	}

	return &MySQLFetcher{db: db, selectQuery: query}, nil
}

func (f *MySQLFetcher) GetJobs(maxJobs int) ([]int, error) {
	rows, err := f.db.Query(f.selectQuery)
	if err != nil {
		return nil, fmt.Errorf("error querying for jobs: %w", err)
	}
	defer rows.Close()

	var jobs []int
	for rows.Next() {
		var solutionID int
		if err := rows.Scan(&solutionID); err != nil {
			return nil, err
		}
		jobs = append(jobs, solutionID)
	}
	return jobs, nil
}

func (f *MySQLFetcher) CheckOut(solutionID int, result int) (bool, error) {
	// For Redis and some distributed modes, checkout is not needed.
	if f.db == nil {
		return true, nil
	}
	query := `UPDATE solution SET result=?, time=0, memory=0, judgetime=NOW() 
              WHERE solution_id=? and result<2 LIMIT 1`
	res, err := f.db.Exec(query, result, solutionID)
	if err != nil {
		return false, err
	}
	rowsAffected, err := res.RowsAffected()
	return rowsAffected > 0, err
}

func (f *MySQLFetcher) Close() error {
	return f.db.Close()
}


// --- Redis Fetcher ---
type RedisFetcher struct {
	client *redis.Client
	qname string
}

func NewRedisFetcher(cfg *Config) (*RedisFetcher, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.RedisServer, cfg.RedisPort),
		Password: cfg.RedisAuth,
		DB:       0,
	})

	if _, err := rdb.Ping(context.Background()).Result(); err != nil {
		return nil, fmt.Errorf("could not connect to Redis: %w", err)
	}

	return &RedisFetcher{client: rdb, qname: cfg.RedisQName}, nil
}


func (f *RedisFetcher) GetJobs(maxJobs int) ([]int, error) {
    var jobs []int
    for i := 0; i < maxJobs; i++ {
        val, err := f.client.RPop(context.Background(), f.qname).Int()
        if err == redis.Nil {
            break // Queue is empty
        }
        if err != nil {
            return nil, fmt.Errorf("error getting job from Redis: %w", err)
        }
        jobs = append(jobs, val)
    }
    return jobs, nil
}


func (f *RedisFetcher) CheckOut(solutionID int, result int) (bool, error) {
	// RPop is atomic, so no explicit checkout is needed.
	return true, nil
}

func (f *RedisFetcher) Close() error {
	return f.client.Close()
}