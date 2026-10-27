package client

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/sempr/hustoj-go/pkg/constants"
	"github.com/sempr/hustoj-go/pkg/models"
	"github.com/sempr/hustoj-go/pkg/repository"
	"github.com/sempr/hustoj-go/pkg/subtask"
)

// determineOIMode determines whether to use OI scoring mode
// OI mode is used when there are multiple test files that might be grouped into subtasks
func (jc *JudgeClient) determineOIMode(dataFiles [][]string) bool {
	// If there are multiple test files, use OI mode
	// This allows subtask-based scoring where all tests in a subtask must pass
	return len(dataFiles) > 1
}

func (jc *JudgeClient) problemDataDir(problem *repository.Problem) string {
	if jc.task != nil {
		return jc.task.DataDir
	}
	return filepath.Join(jc.config.OJHome, "data", strconv.Itoa(problem.ID))
}

func (jc *JudgeClient) detectSpjType(dataDir string) int {
	tpjPath := filepath.Join(dataDir, "tpj")
	upjPath := filepath.Join(dataDir, "upj")
	spjPath := filepath.Join(dataDir, "spj")

	if _, err := os.Stat(upjPath); err == nil {
		slog.Info("Detected UPJ special judge", "data_dir", dataDir)
		return constants.OJ_SPJ_PROGRAM_UPJ
	}

	if _, err := os.Stat(tpjPath); err == nil {
		slog.Info("Detected TPJ special judge", "data_dir", dataDir)
		return constants.OJ_SPJ_PROGRAM_TPJ
	}

	if _, err := os.Stat(spjPath); err == nil {
		slog.Info("Detected SPJ special judge", "data_dir", dataDir)
		return constants.OJ_SPJ_PROGRAM_SPJ
	}

	return 0
}

func (jc *JudgeClient) findDataFiles(dataDir string) ([][]string, error) {
	slog.Info("Scanning data files", "directory", dataDir)

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			slog.Warn("Data directory not found", "directory", dataDir)
			return [][]string{}, nil
		}
		return nil, fmt.Errorf("failed to read data directory %s: %w", dataDir, err)
	}

	var inFiles []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		fileName := entry.Name()
		if filepath.Ext(fileName) == ".in" {
			inFiles = append(inFiles, fileName)
		}
	}

	sort.Strings(inFiles)
	slog.Info("Found .in files", "count", len(inFiles))

	var result [][]string
	for _, inFileName := range inFiles {
		inFullPath := filepath.Join(dataDir, inFileName)
		baseName := strings.TrimSuffix(inFileName, ".in")
		outFileName := baseName + ".out"
		outFullPath := filepath.Join(dataDir, outFileName)

		outPath := ""
		if _, err := os.Stat(outFullPath); err == nil {
			outPath = outFullPath
		} else if !os.IsNotExist(err) {
			slog.Warn("Cannot access .out file", "path", outFullPath, "error", err)
		}

		result = append(result, []string{inFullPath, outPath})
	}

	slog.Info("Data file pairing completed", "pairs", len(result))
	return result, nil
}

func (jc *JudgeClient) writeSourceCode(source string, langID int, workDir string) error {
	langBasic, err := jc.langManager.GetLanguageBasic(langID)
	if err != nil {
		return fmt.Errorf("failed to get language basic info: %w", err)
	}

	fileName := fmt.Sprintf("Main%s", langBasic.Suffix)
	filePath := filepath.Join(workDir, "code", fileName)

	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return fmt.Errorf("failed to create code directory: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(source), 0o644); err != nil {
		return fmt.Errorf("failed to write source code: %w", err)
	}

	slog.Info("Source code written", "path", filePath)
	return nil
}

// TestContext holds all necessary data for test case execution
type TestContext struct {
	Solution   *repository.Solution
	Problem    *repository.Problem
	RunConfig  RunConfig
	DataFiles  [][]string
	OIMode     bool
	SpjProgram int
	InName     string
	OutName    string
	// Communication 非 nil 表示这是通讯题（spj=4）。
	Communication *CommunicationInfo
	// CommMessages 累积通讯题校验器的诊断信息，最终写入 runtimeinfo。
	CommMessages []string
}

// ExecutionStats holds statistics from test execution
type ExecutionStats struct {
	PeakMemory int
	TotalTime  int
}

func (jc *JudgeClient) prepareTestContext(solution *repository.Solution, problem *repository.Problem, rootfs string, spjProgram int) (*TestContext, error) {
	dataDir := jc.problemDataDir(problem)

	dataFiles, err := jc.findDataFiles(dataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to find data files: %w", err)
	}
	// 交互评测（spj=3）：每个 .in 文件一个交互会话；没有 .in 时注入合成用例。
	if problem.SPJ == constants.OJ_SPJ_MODE_INTERACTIVE {
		dataFiles = jc.interactiveDataFiles(dataDir, dataFiles)
	}

	// 通讯题（spj=4）：答案文件是 X.ans，校验器来自数据目录。
	var comm *CommunicationInfo
	if problem.SPJ == constants.OJ_SPJ_MODE_COMMUNICATION {
		comm, dataFiles = jc.prepareCommunication(dataDir, dataFiles)
	}

	inName := jc.findInputName(dataDir)
	outName := jc.findOutputName(dataDir)

	runConfig := RunConfig{
		Lang:        solution.Language,
		Rootdir:     rootfs,
		Workdir:     filepath.Join(rootfs, "code"),
		Timelimit:   int(1000 * problem.TimeLimit),
		MemoryLimit: problem.MemLimit,
		InName:      inName,
		OutName:     outName,
		Spj:         problem.SPJ,
		SpjProgram:  spjProgram,
	}

	// 交互评测：准备 interactor 二进制与 FIFO 通道。
	if problem.SPJ == constants.OJ_SPJ_MODE_INTERACTIVE {
		info, err := jc.prepareInteractor(rootfs, dataDir)
		if err != nil {
			return nil, fmt.Errorf("failed to prepare interactor: %w", err)
		}
		runConfig.Interactive = info
	}

	return &TestContext{
		Solution:      solution,
		Problem:       problem,
		RunConfig:     runConfig,
		DataFiles:     dataFiles,
		OIMode:        jc.determineOIMode(dataFiles),
		SpjProgram:    spjProgram,
		InName:        inName,
		OutName:       outName,
		Communication: comm,
	}, nil
}

func (jc *JudgeClient) executeAllTestCases(ctx *TestContext) ([]subtask.TestResult, models.TotalResults, ExecutionStats, error) {
	var (
		testResults  []subtask.TestResult
		totalResults models.TotalResults
		stats        ExecutionStats
	)

	for _, dataFile := range ctx.DataFiles {
		testResult, oneResult, err := jc.executeSingleTestCase(ctx, dataFile)
		if err != nil {
			return nil, models.TotalResults{}, ExecutionStats{}, err
		}

		// Update statistics
		if testResult.Time > stats.TotalTime {
			stats.TotalTime = testResult.Time
		}
		if testResult.Mem > stats.PeakMemory {
			stats.PeakMemory = testResult.Mem
		}

		testResults = append(testResults, testResult)
		totalResults.Results = append(totalResults.Results, oneResult)
	}

	return testResults, totalResults, stats, nil
}

func (jc *JudgeClient) executeSingleTestCase(ctx *TestContext, dataFile []string) (subtask.TestResult, models.OneResult, error) {
	var result, timeUsed, memUsed int
	if ctx.Communication != nil {
		result, timeUsed, memUsed = jc.runCommunicationCase(ctx, dataFile)
	} else {
		ctx.RunConfig.InFile = dataFile[0]
		ctx.RunConfig.OutFile = dataFile[1]
		result, timeUsed, memUsed = jc.runAndCompare(ctx.RunConfig)
	}

	filename := filepath.Base(dataFile[0])

	testResult := subtask.TestResult{
		Filename: filename,
		Score:    subtask.ExtractScoreFromFilename(filename),
		Result:   result,
		SpjMark:  jc.calculateSpjMark(result, ctx),
		Time:     timeUsed,
		Mem:      memUsed,
	}

	oneResult := models.OneResult{
		Result:   result,
		Datafile: filename,
		Time:     timeUsed,
		Mem:      memUsed,
	}

	jc.logTestResult(filename, result)

	return testResult, oneResult, nil
}

func (jc *JudgeClient) calculateSpjMark(result int, ctx *TestContext) float64 {
	if ctx.Problem.SPJ != constants.OJ_SPJ_MODE_NONE && ctx.SpjProgram == constants.OJ_SPJ_PROGRAM_UPJ {
		switch result {
		case constants.OJ_AC, constants.OJ_PE:
			return 1.0
		case constants.OJ_WA:
			return 0.0
		}
	}
	return 0.0
}

func (jc *JudgeClient) logTestResult(filename string, result int) {
	if result != constants.OJ_AC {
		slog.Warn("Test case failed", "data_file", filename, "result", result)
	} else {
		slog.Info("Test case passed", "data_file", filename)
	}
}

func (jc *JudgeClient) determineOIModeFromFiles(testResults []subtask.TestResult) bool {
	return len(testResults) > 1
}
