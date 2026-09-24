package client

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/sempr/hustoj-go/pkg/config"
	"github.com/sempr/hustoj-go/pkg/language"
	"github.com/sempr/hustoj-go/pkg/models"
	"github.com/sempr/hustoj-go/pkg/repository"
)

type RunConfig struct {
	Lang        int
	Rootdir     string
	Workdir     string
	InFile      string
	OutFile     string
	InName      string
	OutName     string
	Timelimit   int
	MemoryLimit int
	Spj         int
	SpjProgram  int
	Interactive *InteractorInfo
}

type JudgeClient struct {
	config      *config.JudgeConfig
	db          *repository.Database
	langManager *language.Manager
	solutionID  int
	runnerID    string
	debug       bool
	task        *models.JudgeTask
	workBase    string
	reporter    JudgeReporter
}

func NewJudgeClient(solutionID int, runnerID, homeDir string, debug bool) (*JudgeClient, error) {
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
		return nil, fmt.Errorf("failed to initialize language manager: %w", err)
	}

	client := &JudgeClient{
		config:      cfg,
		db:          db,
		langManager: langManager,
		solutionID:  solutionID,
		runnerID:    runnerID,
		debug:       debug,
	}

	slog.SetDefault(slog.Default().With("solution_id", solutionID))

	return client, nil
}

// NewStandaloneClient 创建一个不依赖数据库的评测客户端。
// 所有评测参数通过 task 传入，结果由调用方（RunStandalone）输出到 stdout。
func NewStandaloneClient(task *models.JudgeTask) (*JudgeClient, error) {
	homeDir := task.OJHome
	if homeDir == "" {
		homeDir = "/home/judge"
	}

	cfg, err := config.LoadJudgeConf(homeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	langManager, err := language.NewLanguageManager(homeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize language manager: %w", err)
	}

	workBase := task.WorkBase
	if workBase == "" {
		workBase = "/tmp"
	}

	client := &JudgeClient{
		config:      cfg,
		db:          nil,
		langManager: langManager,
		solutionID:  os.Getpid(),
		runnerID:    "standalone",
		debug:       false,
		task:        task,
		workBase:    workBase,
	}

	slog.SetDefault(slog.Default().With("standalone", true))

	return client, nil
}

func (jc *JudgeClient) Close() error {
	if jc.db != nil {
		return jc.db.Close()
	}
	return nil
}

func (jc *JudgeClient) updateSolutionStatus(status int) error {
	return jc.db.UpdateSolution(jc.solutionID, status, 0, 0, 0.0)
}

func (jc *JudgeClient) updateUserStats(userID string) {
	if err := jc.db.UpdateUserStats(userID); err != nil {
		slog.Warn("Failed to update user stats", "user_id", userID, "error", err)
	}
}

func (jc *JudgeClient) updateProblemStats(problemID, contestID int) {
	if err := jc.db.UpdateProblemStats(problemID, contestID); err != nil {
		slog.Warn("Failed to update problem stats", "problem_id", problemID, "contest_id", contestID, "error", err)
	}
}

func (jc *JudgeClient) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	destinationFile, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destinationFile.Close()

	if _, err := destinationFile.ReadFrom(sourceFile); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}

	return nil
}

func (jc *JudgeClient) findInputName(dataDir string) string {
	inNameFile := filepath.Join(dataDir, "input.name")
	data, err := os.ReadFile(inNameFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func (jc *JudgeClient) findOutputName(dataDir string) string {
	outNameFile := filepath.Join(dataDir, "output.name")
	data, err := os.ReadFile(outNameFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
