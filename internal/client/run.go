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
	"github.com/sempr/hustoj-go/pkg/language"
	"github.com/sempr/hustoj-go/pkg/models"
	"github.com/sempr/hustoj-go/pkg/rawtext"
	"github.com/sempr/hustoj-go/pkg/repository"
	"github.com/sempr/hustoj-go/pkg/subtask"
)

func (jc *JudgeClient) Run() error {
	slog.Info("Starting judge process", "runner_id", jc.runnerID)

	ctx, err := jc.prepareJudgeContext()
	if err != nil {
		return err
	}

	workDir, cleanupFunc, err := jc.setupEnvironment(ctx)
	if err != nil {
		return err
	}
	if cleanupFunc != nil {
		defer cleanupFunc()
	}

	if ctx.Problem.SPJ == constants.OJ_SPJ_MODE_RAWTEXT {
		return jc.handleRawTextJudge(ctx.Solution, ctx.Problem, workDir)
	}

	if err := jc.handleCompilation(ctx, workDir); err != nil {
		return err
	}

	if err := jc.handleExecution(ctx, workDir); err != nil {
		return err
	}

	return nil
}

// JudgeContext holds all necessary context for judge execution
type JudgeContext struct {
	Solution   *repository.Solution
	Problem    *repository.Problem
	LangConfig *language.LangConfig
	SpjProgram int
}

func (jc *JudgeClient) prepareJudgeContext() (*JudgeContext, error) {
	solution, err := jc.db.GetSolution(jc.solutionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get solution info: %w", err)
	}

	problem, err := jc.db.GetProblem(solution.ProblemID)
	if err != nil {
		return nil, fmt.Errorf("failed to get problem info: %w", err)
	}

	spjProgram := jc.detectSpjType(problem)

	langConfig, err := jc.langManager.GetLanguageConfig(solution.Language)
	if err != nil {
		return nil, fmt.Errorf("failed to get language config: %w", err)
	}

	slog.Info("Retrieved judge information",
		"problem_id", solution.ProblemID,
		"user_id", solution.UserID,
		"language", solution.Language,
		"contest_id", solution.ContestID,
		"time_limit", problem.TimeLimit,
		"memory_limit", problem.MemLimit,
		"spj", problem.SPJ,
	)

	return &JudgeContext{
		Solution:   solution,
		Problem:    problem,
		LangConfig: langConfig,
		SpjProgram: spjProgram,
	}, nil
}

func (jc *JudgeClient) setupEnvironment(ctx *JudgeContext) (string, func(), error) {
	workDir, err := jc.setupWorkEnvironment(ctx.LangConfig)
	if err != nil {
		return "", nil, fmt.Errorf("failed to setup work environment: %w", err)
	}

	source, err := jc.db.GetSolutionSource(jc.solutionID)
	if err != nil {
		jc.cleanupWorkEnvironment(workDir)
		return "", nil, fmt.Errorf("failed to get solution source: %w", err)
	}

	if err := jc.writeSourceCode(source, ctx.Solution.Language, workDir); err != nil {
		jc.cleanupWorkEnvironment(workDir)
		return "", nil, fmt.Errorf("failed to write source code: %w", err)
	}

	cleanupFunc := func() {
		if !jc.debug {
			jc.cleanupWorkEnvironment(workDir)
		}
	}

	return workDir, cleanupFunc, nil
}

func (jc *JudgeClient) handleCompilation(ctx *JudgeContext, workDir string) error {
	if err := jc.updateSolutionStatus(constants.OJ_CI); err != nil {
		slog.Warn("Failed to update to compiling status", "error", err)
	}

	compileResult := jc.compile(ctx.Solution.Language, workDir, ctx.LangConfig)
	if compileResult.SystemError {
		return jc.handleCompilationSystemError(ctx, compileResult)
	}
	if compileResult.ExitStatus != 0 {
		return jc.handleCompilationFailure(ctx, compileResult)
	}

	return nil
}

func (jc *JudgeClient) handleCompilationSystemError(ctx *JudgeContext, compileResult *models.SandboxOutput) error {
	slog.Error("Compilation system error", "output", compileResult.CombinedOutput)

	if err := jc.db.AddCompileError(jc.solutionID, compileResult.CombinedOutput); err != nil {
		slog.Warn("Failed to add compile error info", "error", err)
	}
	if err := jc.updateSolutionStatus(constants.OJ_SE); err != nil {
		return fmt.Errorf("failed to update solution status: %w", err)
	}
	jc.updateUserStats(ctx.Solution.UserID)
	jc.updateProblemStats(ctx.Solution.ProblemID, ctx.Solution.ContestID)

	return fmt.Errorf("system error during compilation: %s", compileResult.CombinedOutput)
}

func (jc *JudgeClient) handleCompilationFailure(ctx *JudgeContext, compileResult *models.SandboxOutput) error {
	slog.Info("Compilation failed", "output", compileResult.CombinedOutput)

	if err := jc.db.AddCompileError(jc.solutionID, compileResult.CombinedOutput); err != nil {
		slog.Warn("Failed to add compile error info", "error", err)
	}
	if err := jc.updateSolutionStatus(constants.OJ_CE); err != nil {
		return fmt.Errorf("failed to update solution status: %w", err)
	}
	jc.updateUserStats(ctx.Solution.UserID)
	jc.updateProblemStats(ctx.Solution.ProblemID, ctx.Solution.ContestID)

	return nil
}

func (jc *JudgeClient) handleExecution(ctx *JudgeContext, workDir string) error {
	if err := jc.updateSolutionStatus(constants.OJ_RI); err != nil {
		slog.Warn("Failed to update to running status", "error", err)
	}

	return jc.runTestCases(ctx.Solution, ctx.Problem, workDir, ctx.LangConfig, ctx.SpjProgram)
}

// determineOIMode determines whether to use OI scoring mode
// OI mode is used when there are multiple test files that might be grouped into subtasks
func (jc *JudgeClient) determineOIMode(dataFiles [][]string) bool {
	// If there are multiple test files, use OI mode
	// This allows subtask-based scoring where all tests in a subtask must pass
	return len(dataFiles) > 1
}

func (jc *JudgeClient) detectSpjType(problem *repository.Problem) int {
	if problem.SPJ != constants.OJ_SPJ_MODE_SPJ {
		return 0
	}

	dataDir := filepath.Join(jc.config.OJHome, "data", strconv.Itoa(problem.ID))
	tpjPath := filepath.Join(dataDir, "tpj")
	upjPath := filepath.Join(dataDir, "upj")
	spjPath := filepath.Join(dataDir, "spj")

	if _, err := os.Stat(upjPath); err == nil {
		slog.Info("Detected UPJ special judge", "problem_id", problem.ID)
		return constants.OJ_SPJ_PROGRAM_UPJ
	}

	if _, err := os.Stat(tpjPath); err == nil {
		slog.Info("Detected TPJ special judge", "problem_id", problem.ID)
		return constants.OJ_SPJ_PROGRAM_TPJ
	}

	if _, err := os.Stat(spjPath); err == nil {
		slog.Info("Detected SPJ special judge", "problem_id", problem.ID)
		return constants.OJ_SPJ_PROGRAM_SPJ
	}

	return 0
}

func (jc *JudgeClient) findDataFiles(problemID int) ([][]string, error) {
	dataDir := filepath.Join(jc.config.OJHome, "data", strconv.Itoa(problemID))
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

	if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
		return fmt.Errorf("failed to create code directory: %w", err)
	}

	if err := os.WriteFile(filePath, []byte(source), 0644); err != nil {
		return fmt.Errorf("failed to write source code: %w", err)
	}

	slog.Info("Source code written", "path", filePath)
	return nil
}

func (jc *JudgeClient) handleRawTextJudge(solution *repository.Solution, problem *repository.Problem, rootfs string) error {
	_ = problem
	langBasic, err := jc.langManager.GetLanguageBasic(solution.Language)
	if err != nil {
		return fmt.Errorf("failed to get language basic info: %w", err)
	}

	details, userScore, totalScore, err := rawtext.RawTextJudge(
		filepath.Join(jc.config.OJHome, "data", fmt.Sprint(solution.ProblemID), "data.in"),
		filepath.Join(jc.config.OJHome, "data", fmt.Sprint(solution.ProblemID), "data.out"),
		filepath.Join(rootfs, "code", fmt.Sprintf("Main%s", langBasic.Suffix)),
	)

	if err != nil {
		slog.Error("Rawtext judge error", "error", err)
		if err := jc.updateSolutionStatus(constants.OJ_RE); err != nil {
			return fmt.Errorf("failed to update solution status: %w", err)
		}
		return nil
	}

	result := constants.OJ_AC
	rate := 1.0
	if userScore < totalScore {
		result = constants.OJ_WA
		rate = float64(userScore) / float64(totalScore)
	}

	if err := jc.db.UpdateSolution(jc.solutionID, result, 0, 0, rate); err != nil {
		return fmt.Errorf("failed to update solution: %w", err)
	}

	if err := jc.db.AddRuntimeInfo(jc.solutionID, details); err != nil {
		slog.Warn("Failed to add runtime info", "error", err)
	}

	return nil
}

func (jc *JudgeClient) runTestCases(solution *repository.Solution, problem *repository.Problem, rootfs string, langConfig *language.LangConfig, spjProgram int) error {
	_ = langConfig

	ctx, err := jc.prepareTestContext(solution, problem, rootfs, spjProgram)
	if err != nil {
		return err
	}

	testResults, totalResults, stats, err := jc.executeAllTestCases(ctx)
	if err != nil {
		return err
	}

	return jc.processTestResults(solution, testResults, totalResults, stats)
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
}

// ExecutionStats holds statistics from test execution
type ExecutionStats struct {
	PeakMemory int
	TotalTime  int
}

func (jc *JudgeClient) prepareTestContext(solution *repository.Solution, problem *repository.Problem, rootfs string, spjProgram int) (*TestContext, error) {
	dataFiles, err := jc.findDataFiles(problem.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to find data files: %w", err)
	}

	inName := jc.findInputName(problem.ID)
	outName := jc.findOutputName(problem.ID)

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

	return &TestContext{
		Solution:   solution,
		Problem:    problem,
		RunConfig:  runConfig,
		DataFiles:  dataFiles,
		OIMode:     jc.determineOIMode(dataFiles),
		SpjProgram: spjProgram,
		InName:     inName,
		OutName:    outName,
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
	ctx.RunConfig.InFile = dataFile[0]
	ctx.RunConfig.OutFile = dataFile[1]

	result, timeUsed, memUsed := jc.runAndCompare(ctx.RunConfig)

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

func (jc *JudgeClient) processTestResults(solution *repository.Solution, testResults []subtask.TestResult, totalResults models.TotalResults, stats ExecutionStats) error {
	subtaskScore := subtask.Judge(testResults, jc.determineOIModeFromFiles(testResults))

	totalResults.FinalResult = subtaskScore.FinalResult
	passRate := subtaskScore.PassRate

	if err := jc.addRuntimeInfo(jc.solutionID, totalResults); err != nil {
		slog.Warn("Failed to add runtime info", "error", err)
	}

	slog.Info("Judge completed",
		"final_result", totalResults.FinalResult,
		"total_time_ms", stats.TotalTime,
		"peak_memory_kb", stats.PeakMemory,
		"pass_rate", passRate,
	)

	if err := jc.db.UpdateSolution(jc.solutionID, totalResults.FinalResult, stats.TotalTime, stats.PeakMemory, passRate); err != nil {
		return fmt.Errorf("failed to update final solution result: %w", err)
	}

	jc.updateUserStats(solution.UserID)
	jc.updateProblemStats(solution.ProblemID, solution.ContestID)

	return nil
}

func (jc *JudgeClient) determineOIModeFromFiles(testResults []subtask.TestResult) bool {
	return len(testResults) > 1
}
