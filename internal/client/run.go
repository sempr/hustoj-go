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
)

func (jc *JudgeClient) Run() error {
	slog.Info("Starting judge process", "runner_id", jc.runnerID)

	solution, err := jc.db.GetSolution(jc.solutionID)
	if err != nil {
		return fmt.Errorf("failed to get solution info: %w", err)
	}

	problem, err := jc.db.GetProblem(solution.ProblemID)
	if err != nil {
		return fmt.Errorf("failed to get problem info: %w", err)
	}

	langConfig, err := jc.langManager.GetLanguageConfig(solution.Language)
	if err != nil {
		return fmt.Errorf("failed to get language config: %w", err)
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

	workDir, err := jc.setupWorkEnvironment(langConfig)
	if err != nil {
		return fmt.Errorf("failed to setup work environment: %w", err)
	}

	if !jc.debug {
		defer jc.cleanupWorkEnvironment(workDir)
	}

	source, err := jc.db.GetSolutionSource(jc.solutionID)
	if err != nil {
		return fmt.Errorf("failed to get solution source: %w", err)
	}

	if err := jc.writeSourceCode(source, solution.Language, workDir); err != nil {
		return fmt.Errorf("failed to write source code: %w", err)
	}

	if problem.SPJ == constants.OJ_SPJ_MODE_RAWTEXT {
		return jc.handleRawTextJudge(solution, problem, workDir)
	}

	if err := jc.updateSolutionStatus(constants.OJ_CI); err != nil {
		slog.Warn("Failed to update to compiling status", "error", err)
	}

	compileResult := jc.compile(solution.Language, workDir, langConfig)
	if compileResult.SystemError {
		slog.Error("Compilation system error", "output", compileResult.CombinedOutput)
		if err := jc.db.AddCompileError(jc.solutionID, compileResult.CombinedOutput); err != nil {
			slog.Warn("Failed to add compile error info", "error", err)
		}
		if err := jc.updateSolutionStatus(constants.OJ_SE); err != nil {
			return fmt.Errorf("failed to update solution status: %w", err)
		}
		jc.updateUserStats(solution.UserID)
		jc.updateProblemStats(solution.ProblemID, solution.ContestID)
		return fmt.Errorf("system error during compilation: %s", compileResult.CombinedOutput)
	}
	if compileResult.ExitStatus != 0 {
		slog.Info("Compilation failed", "output", compileResult.CombinedOutput)
		if err := jc.db.AddCompileError(jc.solutionID, compileResult.CombinedOutput); err != nil {
			slog.Warn("Failed to add compile error info", "error", err)
		}
		if err := jc.updateSolutionStatus(constants.OJ_CE); err != nil {
			return fmt.Errorf("failed to update solution status: %w", err)
		}
		jc.updateUserStats(solution.UserID)
		jc.updateProblemStats(solution.ProblemID, solution.ContestID)
		return nil
	}

	if err := jc.updateSolutionStatus(constants.OJ_RI); err != nil {
		slog.Warn("Failed to update to running status", "error", err)
	}

	return jc.runTestCases(solution, problem, workDir, langConfig)
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

func (jc *JudgeClient) runTestCases(solution *repository.Solution, problem *repository.Problem, rootfs string, langConfig *language.LangConfig) error {
	_ = langConfig
	dataFiles, err := jc.findDataFiles(problem.ID)
	if err != nil {
		return fmt.Errorf("failed to find data files: %w", err)
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
	}

	var (
		totalTime  = 0
		peakMemory = 0
		passRate   = 0.0
		testCases  = float64(len(dataFiles))
	)

	var totalResults models.TotalResults
	totalResults.FinalResult = constants.OJ_AC

	for _, dataFile := range dataFiles {
		runConfig.InFile = dataFile[0]
		runConfig.OutFile = dataFile[1]

		result, timeUsed, memUsed := jc.runAndCompare(runConfig)

		if timeUsed > totalTime {
			totalTime = timeUsed
		}
		if memUsed > peakMemory {
			peakMemory = memUsed
		}

		filename := filepath.Base(dataFile[0])
		testResult := models.OneResult{
			Result:   result,
			Datafile: filename,
			Time:     timeUsed,
			Mem:      memUsed,
		}

		if result != constants.OJ_AC {
			if totalResults.FinalResult == constants.OJ_AC {
				totalResults.FinalResult = result
			}
			slog.Warn("Test case failed", "data_file", filename, "result", result)
		} else {
			passRate += 1.0
			slog.Info("Test case passed", "data_file", filename)
		}

		totalResults.Results = append(totalResults.Results, testResult)
	}

	if testCases > 0 {
		passRate = passRate / testCases
	} else if totalResults.FinalResult == constants.OJ_AC {
		passRate = 1.0
	}

	if err := jc.addRuntimeInfo(jc.solutionID, totalResults); err != nil {
		slog.Warn("Failed to add runtime info", "error", err)
	}

	slog.Info("Judge completed",
		"final_result", totalResults.FinalResult,
		"total_time_ms", totalTime,
		"peak_memory_kb", peakMemory,
		"pass_rate", passRate,
	)

	if err := jc.db.UpdateSolution(jc.solutionID, totalResults.FinalResult, totalTime, peakMemory, passRate); err != nil {
		return fmt.Errorf("failed to update final solution result: %w", err)
	}

	jc.updateUserStats(solution.UserID)
	jc.updateProblemStats(solution.ProblemID, solution.ContestID)

	return nil
}
