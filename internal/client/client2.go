package client

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sempr/hustoj-go/pkg/config"
	"github.com/sempr/hustoj-go/pkg/constants"
	"github.com/sempr/hustoj-go/pkg/language"
	"github.com/sempr/hustoj-go/pkg/models"
	"github.com/sempr/hustoj-go/pkg/repository"
)

// client2Reporter 把 judge 流水线的进度与最终结果写回数据库，
// 覆盖范围与 Run()（handleCompilation*/processTestResults/handleRawTextJudge）一致。
type client2Reporter struct {
	db         *repository.Database
	solutionID int
	userID     string
	problemID  int
	contestID  int
}

func (r *client2Reporter) OnPhase(status int) error {
	return r.db.UpdateSolution(r.solutionID, status, 0, 0, 0.0)
}

func (r *client2Reporter) OnCompileError(res *StandaloneResult) error {
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

func (r *client2Reporter) OnFinished(res *StandaloneResult) error {
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

func (r *client2Reporter) updateStats() {
	if err := r.db.UpdateUserStats(r.userID); err != nil {
		slog.Warn("Failed to update user stats", "user_id", r.userID, "error", err)
	}
	if err := r.db.UpdateProblemStats(r.problemID, r.contestID); err != nil {
		slog.Warn("Failed to update problem stats", "problem_id", r.problemID, "contest_id", r.contestID, "error", err)
	}
}

// NewClient2Client 创建复用 judge 流水线（RunStandalone）的评测客户端。
// 与 NewJudgeClient 参数一致：通过数据库读取题目/源码，组装 JudgeTask，
// 并把评测进度通过 client2Reporter 写回数据库。
func NewClient2Client(solutionID int, runnerID, homeDir string, debug bool) (*JudgeClient, error) {
	cfg, err := config.LoadJudgeConf(homeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	db, err := repository.NewDatabase(&cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	langManager, err := language.NewLanguageManager(homeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize language manager: %w", err)
	}

	solution, err := db.GetSolution(solutionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get solution info: %w", err)
	}

	problem, err := db.GetProblem(solution.ProblemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get problem info: %w", err)
	}

	source, err := db.GetSolutionSource(solutionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get solution source: %w", err)
	}

	task := &models.JudgeTask{
		Source:     source,
		Language:   solution.Language,
		DataDir:    filepath.Join(cfg.OJHome, "data", strconv.Itoa(problem.ID)),
		TimeLimit:  int(problem.TimeLimit * 1000),
		MemLimitKB: problem.MemLimit * 1024,
		Spj:        problem.SPJ,
		OJHome:     homeDir,
		WorkBase:   filepath.Join(homeDir, "run"),
	}

	reporter := &client2Reporter{
		db:         db,
		solutionID: solutionID,
		userID:     solution.UserID,
		problemID:  solution.ProblemID,
		contestID:  solution.ContestID,
	}

	client := &JudgeClient{
		config:      cfg,
		db:          db,
		langManager: langManager,
		solutionID:  solutionID,
		runnerID:    runnerID,
		debug:       debug,
		task:        task,
		workBase:    task.WorkBase,
		reporter:    reporter,
	}

	slog.SetDefault(slog.Default().With("solution_id", solutionID))

	return client, nil
}

// Client2Main 是 client2 子命令的入口，参数格式与 client（Main）保持一致：
//
//	hustoj-go client2 <solution_id> <runner_id> [oj_home_path] [-debug]
//
// 评测本体复用 judge 的 RunStandalone 流水线，进度与结果上报到数据库。
// 退出码与 client 一致：编译/系统错误时退出 1，其余情况退出 0。
func Client2Main() {
	args := os.Args[1:]

	if len(args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s client2 <solution_id> <runner_id> [oj_home_path] [-debug]\n", os.Args[0])
		os.Exit(1)
	}

	solutionID, err := strconv.Atoi(args[1])
	if err != nil {
		slog.Error("Invalid solution ID", "input", args[1], "error", err)
		os.Exit(1)
	}

	runnerID := args[2]
	homePath := "/home/judge"
	debug := false

	for _, arg := range args[3:] {
		if arg == "-debug" || arg == "DEBUG" {
			debug = true
		} else if !strings.HasPrefix(arg, "-") {
			homePath = arg
		}
	}

	client, err := NewClient2Client(solutionID, runnerID, homePath, debug)
	if err != nil {
		slog.Error("Failed to create judge client", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	slog.Info("Starting judge process (client2)", "solution_id", solutionID, "runner_id", runnerID)

	result, err := client.RunStandalone()
	if err != nil {
		slog.Error("Judge process failed", "error", err)
		os.Exit(1)
	}
	if result.FinalResult == constants.OJ_CE || result.FinalResult == constants.OJ_SE {
		slog.Info("Judge process finished with compile/system error", "final_result", result.FinalResult)
		os.Exit(1)
	}

	slog.Info("Judge process completed successfully", "final_result", result.FinalResult, "time_ms", result.TimeMs, "memory_kb", result.MemoryKB)
}
