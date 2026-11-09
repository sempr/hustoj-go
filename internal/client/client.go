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
	Lang      int
	Rootdir   string
	Workdir   string
	InFile    string
	OutFile   string
	InName    string
	OutName   string
	Timelimit int // 毫秒
	// MemoryLimit 的单位是 **MB**（与 problem.mem_limit 一致）。
	//
	// 传给沙箱时会 <<10 换成 KB 再写进 cgroup 的 memory.max；这里若填成 KB
	// 会被再放大一次，cgroup 的上限就成了几百 TB —— 内存限制等于没有
	// （测试运行曾经就栽在这上面：探针一口气申请了 2.4GB 才被宿主 OOM 收走）。
	MemoryLimit int
	Spj         int
	SpjProgram  int
	Interactive *InteractorInfo
	// TestRun 表示这是一次测试运行：只跑自定义输入、不与标准输出比对，
	// 结果一律按程序自身状态返回（AC/RE/TL...）。
	TestRun bool
}

// sandboxMemoryKB 把 RunConfig.MemoryLimit（MB）换成沙箱 --memory 用的 KB。
//
// 单位只在这一处转换：沙箱侧拿到 KB 后再换算成 cgroup 的 memory.max（字节）。
// 混一次单位就会差 1024 倍 —— 内存限制要么形同虚设，要么连普通程序都跑不动。
func sandboxMemoryKB(mb int) int { return mb << 10 }

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
	cfg.ApplyEnv()

	langManager, err := language.NewLanguageManager(homeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize language manager: %w", err)
	}

	workBase := task.WorkBase
	if workBase == "" {
		workBase = filepath.Join(homeDir, "run")
	}
	// 提前创建并验证可写：否则会在挂载 tmpfs 时才报一个难懂的 EACCES。
	if err := os.MkdirAll(workBase, 0o755); err != nil {
		return nil, fmt.Errorf("无法创建工作目录 %s: %w", workBase, err)
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
	return copyFileWithMode(src, dst, 0)
}

func (jc *JudgeClient) findInputName(dataDir string) string {
	inNameFile := filepath.Join(dataDir, "input.name")
	// 数据目录对 Web 用户可写，不能跟随 input.name 符号链接去读宿主机文件。
	raw := strings.TrimSpace(readRegularFile(inNameFile, 4096))
	name := sanitizeTestDataName(raw)
	if raw != "" && name == "" {
		slog.Warn("忽略非法的 input.name（只允许单个文件名）", "data_dir", dataDir, "value", raw)
	}
	return name
}

func (jc *JudgeClient) findOutputName(dataDir string) string {
	outNameFile := filepath.Join(dataDir, "output.name")
	raw := strings.TrimSpace(readRegularFile(outNameFile, 4096))
	name := sanitizeTestDataName(raw)
	if raw != "" && name == "" {
		slog.Warn("忽略非法的 output.name（只允许单个文件名）", "data_dir", dataDir, "value", raw)
	}
	return name
}
