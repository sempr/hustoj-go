package client

import (
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"

	"github.com/sempr/hustoj-go/pkg/config"
	"github.com/sempr/hustoj-go/pkg/constants"
	"github.com/sempr/hustoj-go/pkg/language"
	"github.com/sempr/hustoj-go/pkg/models"
	"github.com/sempr/hustoj-go/pkg/repository"
)

// dbReporter 把 judge 流水线的进度与最终结果写回数据库，
// 覆盖编译/系统错误、最终结果、runtimeinfo 与用户/题目统计。
type dbReporter struct {
	db         *repository.Database
	solutionID int
	userID     string
	problemID  int
	contestID  int
}

func (r *dbReporter) OnPhase(status int) error {
	return r.db.UpdateSolution(r.solutionID, status, 0, 0, 0.0)
}

func (r *dbReporter) OnCompileError(res *StandaloneResult) error {
	if res.FinalResult == constants.OJ_SE {
		if err := r.db.AddCompileError(r.solutionID, res.SysError); err != nil {
			return err
		}
		if err := r.db.UpdateSolution(r.solutionID, constants.OJ_SE, 0, 0, 0.0); err != nil {
			return err
		}
	} else {
		if err := r.db.AddCompileError(r.solutionID, res.CompileError); err != nil {
			return err
		}
		if err := r.db.UpdateSolution(r.solutionID, constants.OJ_CE, 0, 0, 0.0); err != nil {
			return err
		}
	}
	r.updateStats()
	return nil
}

func (r *dbReporter) OnFinished(res *StandaloneResult) error {
	if res.RuntimeInfo != "" {
		if err := r.db.AddRuntimeInfo(r.solutionID, res.RuntimeInfo); err != nil {
			slog.Warn("Failed to add runtime info", "solution_id", r.solutionID, "error", err)
		}
	}
	if err := r.db.UpdateSolution(r.solutionID, res.FinalResult, res.TimeMs, res.MemoryKB, res.PassRate); err != nil {
		return err
	}
	r.updateStats()
	return nil
}

func (r *dbReporter) updateStats() {
	if err := r.db.UpdateUserStats(r.userID); err != nil {
		slog.Warn("Failed to update user stats", "user_id", r.userID, "error", err)
	}
	if err := r.db.UpdateProblemStats(r.problemID, r.contestID); err != nil {
		slog.Warn("Failed to update problem stats", "problem_id", r.problemID, "contest_id", r.contestID, "error", err)
	}
}

// NewClient 创建数据库驱动的评测客户端。
//
// 普通提交：读取题目与源码，组装 JudgeTask，评测走 RunStandalone。
// 测试运行：solution 有 custominput 行且 problem_id=0，此时**不能**读题目
// （problem_id=0 查不到），只保留源码与语言，评测走 runCustomInput。
func NewClient(solutionID int, runnerID, homeDir string, debug bool) (*JudgeClient, error) {
	cfg, err := config.LoadJudgeConf(homeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	cfg.Debug = debug

	db, err := repository.NewDatabase(&cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	langManager, err := language.NewLanguageManager(homeDir)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize language manager: %w", err)
	}

	solution, err := db.GetSolution(solutionID)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to get solution info: %w", err)
	}

	source, err := db.GetSolutionSource(solutionID)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to get solution source: %w", err)
	}

	client := &JudgeClient{
		config:      cfg,
		db:          db,
		langManager: langManager,
		solutionID:  solutionID,
		runnerID:    runnerID,
		debug:       debug,
		source:      source,
		language:    solution.Language,
		reporter: &dbReporter{
			db:         db,
			solutionID: solutionID,
			userID:     solution.UserID,
			problemID:  solution.ProblemID,
			contestID:  solution.ContestID,
		},
	}

	// 测试运行必须在读题目之前分流：problem_id 为 0，GetProblem 必然失败。
	// 查询失败时按普通提交继续，与旧实现（错误被忽略）保持一致。
	input, isTestRun, err := db.GetCustomInput(solutionID)
	if err != nil {
		slog.Warn("Failed to check custom input, treating as normal submission",
			"solution_id", solutionID, "error", err)
	} else if isTestRun {
		client.customInput = &input
		slog.SetDefault(slog.Default().With("solution_id", solutionID))
		return client, nil
	}

	problem, err := db.GetProblem(solution.ProblemID)
	if err != nil {
		client.Close()
		return nil, fmt.Errorf("failed to get problem info: %w", err)
	}

	client.task = &models.JudgeTask{
		Source:     source,
		Language:   solution.Language,
		DataDir:    filepath.Join(cfg.OJHome, "data", strconv.Itoa(problem.ID)),
		TimeLimit:  int(problem.TimeLimit * 1000),
		MemLimitKB: problem.MemLimit * 1024,
		Spj:        problem.SPJ,
		OJHome:     homeDir,
		WorkBase:   filepath.Join(homeDir, "run"),
	}
	client.workBase = client.task.WorkBase

	slog.SetDefault(slog.Default().With("solution_id", solutionID))

	return client, nil
}
