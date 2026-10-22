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
	// TestRun 表示这是一次测试运行：只跑自定义输入、不与标准输出比对，
	// 结果一律按程序自身状态返回（AC/RE/TL...）。
	TestRun bool
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

	// source/language 是数据库驱动评测的源码与语言。
	// 普通提交会组装成 task；测试运行没有测试数据目录，只用到这两个值。
	source   string
	language int

	// customInput 非 nil 表示这是一次测试运行（solution 有 custominput 行）：
	// 只跑自定义输入、不比对标准输出，也不计入任何统计。
	customInput *string
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
		source:      task.Source,
		language:    task.Language,
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
