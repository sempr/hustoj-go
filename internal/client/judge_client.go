package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"

	"github.com/sempr/hustoj-go/pkg/config"
	"github.com/sempr/hustoj-go/pkg/constants"
	"github.com/sempr/hustoj-go/pkg/language"
	"github.com/sempr/hustoj-go/pkg/models"
	"github.com/sempr/hustoj-go/pkg/rawtext"
	"github.com/sempr/hustoj-go/pkg/repository"
	"golang.org/x/sys/unix"
)

// RunConfig holds configuration for running a test case
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
}

// JudgeClient handles the judging process
type JudgeClient struct {
	config      *config.JudgeConfig
	db          *repository.Database
	langManager *language.Manager
	solutionID  int
	runnerID    string
	debug       bool
}

// NewJudgeClient creates a new judge client
func NewJudgeClient(solutionID int, runnerID, homeDir string, debug bool) (*JudgeClient, error) {
	// Load configuration
	cfg, err := config.LoadJudgeConf(homeDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	cfg.Debug = debug

	// Initialize database
	db, err := repository.NewDatabase(&cfg.Database)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize database: %w", err)
	}

	// Initialize language manager
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

	// Set up structured logging
	slog.SetDefault(slog.Default().With("solution_id", solutionID))

	return client, nil
}

// Close cleans up resources
func (jc *JudgeClient) Close() error {
	if jc.db != nil {
		return jc.db.Close()
	}
	return nil
}

// Run executes the judging process
func (jc *JudgeClient) Run() error {
	slog.Info("Starting judge process", "runner_id", jc.runnerID)

	// Get solution information
	solution, err := jc.db.GetSolution(jc.solutionID)
	if err != nil {
		return fmt.Errorf("failed to get solution info: %w", err)
	}

	// Get problem information
	problem, err := jc.db.GetProblem(solution.ProblemID)
	if err != nil {
		return fmt.Errorf("failed to get problem info: %w", err)
	}

	// Get language configuration
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

	// Set up working environment
	workDir, err := jc.setupWorkEnvironment(langConfig)
	if err != nil {
		return fmt.Errorf("failed to setup work environment: %w", err)
	}

	if !jc.debug {
		defer jc.cleanupWorkEnvironment(workDir)
	}

	// Get and write source code
	source, err := jc.db.GetSolutionSource(jc.solutionID)
	if err != nil {
		return fmt.Errorf("failed to get solution source: %w", err)
	}

	if err := jc.writeSourceCode(source, solution.Language, workDir); err != nil {
		return fmt.Errorf("failed to write source code: %w", err)
	}

	// Handle rawtext special judge mode
	if problem.SPJ == constants.OJ_SPJ_MODE_RAWTEXT {
		return jc.handleRawTextJudge(solution, problem, workDir)
	}

	// Compile the solution
	if err := jc.updateSolutionStatus(constants.OJ_CI); err != nil {
		slog.Warn("Failed to update to compiling status", "error", err)
	}

	compileResult := jc.compile(solution.Language, workDir, langConfig)
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

	// Update to running status
	if err := jc.updateSolutionStatus(constants.OJ_RI); err != nil {
		slog.Warn("Failed to update to running status", "error", err)
	}

	// Run test cases
	return jc.runTestCases(solution, problem, workDir, langConfig)
}

// setupWorkEnvironment creates the working directory and mounts filesystems
func (jc *JudgeClient) setupWorkEnvironment(langConfig *language.LangConfig) (string, error) {
	workBaseDir := filepath.Join(jc.config.OJHome, "run"+jc.runnerID)

	// Create base directories
	for _, dir := range []string{"rootfs", "tmp"} {
		if err := os.MkdirAll(filepath.Join(workBaseDir, dir), 0755); err != nil {
			return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// Mount tmpfs
	tmpfsDir := filepath.Join(workBaseDir, "tmp")
	if err := unix.Mount("tmpfs", tmpfsDir, "tmpfs", uintptr(unix.MS_NOSUID|unix.MS_NODEV), "size=580M"); err != nil {
		return "", fmt.Errorf("failed to mount tmpfs: %w", err)
	}

	// Create overlay directories
	for _, dir := range []string{"upper", "work"} {
		if err := os.MkdirAll(filepath.Join(tmpfsDir, dir), 0755); err != nil {
			return "", fmt.Errorf("failed to create overlay directory %s: %w", dir, err)
		}
	}

	// Mount overlay filesystem
	options := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s",
		langConfig.Fs.Base,
		filepath.Join(workBaseDir, "tmp", "upper"),
		filepath.Join(workBaseDir, "tmp", "work"),
	)

	rootfs := filepath.Join(workBaseDir, "rootfs")
	if err := unix.Mount("overlay", rootfs, "overlay", 0, options); err != nil {
		return "", fmt.Errorf("failed to mount overlay: %w", err)
	}

	return rootfs, nil
}

// cleanupWorkEnvironment cleans up the working environment
func (jc *JudgeClient) cleanupWorkEnvironment(rootfs string) {
	// Unmount overlay
	if err := unix.Unmount(rootfs, 0); err != nil {
		slog.Warn("Failed to unmount overlay", "error", err)
	}

	// Unmount tmpfs
	tmpfsDir := filepath.Join(filepath.Dir(rootfs), "tmp")
	if err := unix.Unmount(tmpfsDir, 0); err != nil {
		slog.Warn("Failed to unmount tmpfs", "error", err)
	}

	// Remove work directory
	workBaseDir := filepath.Dir(rootfs)
	if err := os.RemoveAll(workBaseDir); err != nil {
		slog.Warn("Failed to remove work directory", "path", workBaseDir, "error", err)
	}
}

// writeSourceCode writes the source code to the working directory
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

// compile compiles the source code
func (jc *JudgeClient) compile(langID int, rootfs string, langConfig *language.LangConfig) *models.SandboxOutput {
	selfName, _ := os.Executable()
	cmd := exec.Command(selfName,
		"sandbox",
		fmt.Sprintf("--rootfs=%s", rootfs),
		fmt.Sprintf("--cmd=%s", langConfig.Cmd.Compile),
		fmt.Sprintf("--time=%d", 3000),
		fmt.Sprintf("--memory=%d", 256<<10),
		fmt.Sprintf("--sid=%d", jc.solutionID),
		"--cwd=/code",
	)

	if len(langConfig.Cmd.Env) > 0 {
		cmd.Env = append(cmd.Env, langConfig.Cmd.Env...)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout

	r, w, err := os.Pipe()
	if err != nil {
		return &models.SandboxOutput{
			UserStatus:     constants.OJ_SE,
			CombinedOutput: "failed to create pipe for compile",
		}
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, w)
	slog.Info("Starting compilation", "language", langID, "work_dir", rootfs)

	if err := cmd.Start(); err != nil {
		return &models.SandboxOutput{
			UserStatus:     constants.OJ_SE,
			CombinedOutput: "failed to start compile command",
		}
	}

	w.Close()
	defer cmd.Wait()

	var output models.SandboxOutput
	if err := json.NewDecoder(r).Decode(&output); err != nil {
		return &models.SandboxOutput{
			UserStatus:     constants.OJ_SE,
			CombinedOutput: fmt.Sprintf("failed to decode compile output: %v", err),
		}
	}

	slog.Debug("Compilation output", "output", output)
	return &output
}

// handleRawTextJudge handles rawtext special judge mode
func (jc *JudgeClient) handleRawTextJudge(solution *repository.Solution, problem *repository.Problem, rootfs string) error {
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

// updateSolutionStatus updates the solution status in the database
func (jc *JudgeClient) updateSolutionStatus(status int) error {
	return jc.db.UpdateSolution(jc.solutionID, status, 0, 0, 0.0)
}

// updateUserStats updates user statistics
func (jc *JudgeClient) updateUserStats(userID string) {
	if err := jc.db.UpdateUserStats(userID); err != nil {
		slog.Warn("Failed to update user stats", "user_id", userID, "error", err)
	}
}

// updateProblemStats updates problem statistics
func (jc *JudgeClient) updateProblemStats(problemID, contestID int) {
	if err := jc.db.UpdateProblemStats(problemID, contestID); err != nil {
		slog.Warn("Failed to update problem stats", "problem_id", problemID, "contest_id", contestID, "error", err)
	}
}

// runTestCases runs all test cases for the solution
func (jc *JudgeClient) runTestCases(solution *repository.Solution, problem *repository.Problem, rootfs string, langConfig *language.LangConfig) error {
	// Find test data files
	dataFiles, err := jc.findDataFiles(problem.ID)
	if err != nil {
		return fmt.Errorf("failed to find data files: %w", err)
	}

	// Get input/output file names
	inName := jc.findInputName(problem.ID)
	outName := jc.findOutputName(problem.ID)

	// Prepare run configuration
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

	// Run each test case
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

	// Calculate final pass rate
	if testCases > 0 {
		passRate = passRate / testCases
	} else if totalResults.FinalResult == constants.OJ_AC {
		passRate = 1.0
	}

	// Add runtime info
	if err := jc.addRuntimeInfo(jc.solutionID, totalResults); err != nil {
		slog.Warn("Failed to add runtime info", "error", err)
	}

	// Update solution with final results
	slog.Info("Judge completed",
		"final_result", totalResults.FinalResult,
		"total_time_ms", totalTime,
		"peak_memory_kb", peakMemory,
		"pass_rate", passRate,
	)

	if err := jc.db.UpdateSolution(jc.solutionID, totalResults.FinalResult, totalTime, peakMemory, passRate); err != nil {
		return fmt.Errorf("failed to update final solution result: %w", err)
	}

	// Update statistics
	jc.updateUserStats(solution.UserID)
	jc.updateProblemStats(solution.ProblemID, solution.ContestID)

	return nil
}

// findDataFiles finds all test data files for a problem
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
	// Find all .in files
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		fileName := entry.Name()
		if filepath.Ext(fileName) == ".in" {
			inFiles = append(inFiles, fileName)
		}
	}

	// Sort .in files to ensure consistent order
	sort.Strings(inFiles)
	slog.Info("Found .in files", "count", len(inFiles))

	// Build file pairs
	var result [][]string
	for _, inFileName := range inFiles {
		inFullPath := filepath.Join(dataDir, inFileName)
		baseName := strings.TrimSuffix(inFileName, ".in")
		outFileName := baseName + ".out"
		outFullPath := filepath.Join(dataDir, outFileName)

		// Check if .out file exists
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

// findInputName gets the custom input filename
func (jc *JudgeClient) findInputName(problemID int) string {
	inNameFile := filepath.Join(jc.config.OJHome, "data", strconv.Itoa(problemID), "input.name")
	data, err := os.ReadFile(inNameFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// findOutputName gets the custom output filename
func (jc *JudgeClient) findOutputName(problemID int) string {
	outNameFile := filepath.Join(jc.config.OJHome, "data", strconv.Itoa(problemID), "output.name")
	data, err := os.ReadFile(outNameFile)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// runAndCompare runs a test case and compares the output
func (jc *JudgeClient) runAndCompare(config RunConfig) (int, int, int) {
	// Prepare stdin/stdout names
	stdinName := "/code/data.in"
	stdoutName := "/code/data.usr"

	if config.InName != "" {
		jc.copyFile(config.InFile, filepath.Join(config.Workdir, config.InName))
		stdinName = ""
	} else {
		jc.copyFile(config.InFile, filepath.Join(config.Workdir, "data.in"))
	}

	if config.OutName != "" {
		stdoutName = ""
	}

	// Get language configuration
	langConfig, err := jc.langManager.GetLanguageConfig(config.Lang)
	if err != nil {
		slog.Error("Failed to get language config", "error", err)
		return constants.OJ_SE, 0, 0
	}

	// Build run arguments
	runArgs := []string{
		"sandbox",
		fmt.Sprintf("--rootfs=%s", config.Rootdir),
		fmt.Sprintf("--cmd=%s", langConfig.Cmd.Run),
		fmt.Sprintf("--time=%d", config.Timelimit),
		fmt.Sprintf("--memory=%d", config.MemoryLimit<<10),
		fmt.Sprintf("--sid=%d", jc.solutionID),
		"--cwd=/code",
	}

	if stdinName != "" {
		runArgs = append(runArgs, fmt.Sprintf("--stdin=%s", stdinName))
	}
	if stdoutName != "" {
		runArgs = append(runArgs, fmt.Sprintf("--stdout=%s", stdoutName))
	}

	// Execute the run command
	selfName, _ := os.Executable()
	cmd := exec.Command(selfName, runArgs...)
	if len(langConfig.Cmd.Env) > 0 {
		cmd.Env = append(cmd.Env, langConfig.Cmd.Env...)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout

	r, w, err := os.Pipe()
	if err != nil {
		return constants.OJ_SE, 0, 0
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, w)
	slog.Info("Starting execution", "language", config.Lang, "work_dir", config.Rootdir)

	if err := cmd.Start(); err != nil {
		return constants.OJ_SE, 0, 0
	}

	w.Close()
	cmd.Wait()

	var output models.SandboxOutput
	if err := json.NewDecoder(r).Decode(&output); err != nil {
		slog.Error("Failed to decode run output", "error", err)
		return constants.OJ_SE, 0, 0
	}

	result := output.UserStatus
	timeUsed := output.Time
	memUsed := output.Memory

	if result != constants.OJ_AC {
		return result, timeUsed, memUsed
	}

	// Handle special judge
	if config.Spj == 1 {
		return jc.handleSpecialJudge(config)
	}

	// Compare output with expected output
	targetOutputName := "data.usr"
	if config.OutName != "" {
		targetOutputName = config.OutName
	}

	targetInputName := "data.in"
	if config.InName != "" {
		targetInputName = config.InName
	}
	_ = targetInputName // Use the variable to avoid unused error

	res, err := jc.compareFiles(config.OutFile, filepath.Join(config.Rootdir, "code", targetOutputName))
	switch res {
	case 1:
		result = constants.OJ_PE
	case 2:
		result = constants.OJ_WA
	case 0:
		result = constants.OJ_AC
	}

	if err != nil {
		result = constants.OJ_RE
	}

	return result, timeUsed, memUsed
}

// handleSpecialJudge handles special judge logic
func (jc *JudgeClient) handleSpecialJudge(config RunConfig) (int, int, int) {
	// Copy expected output
	sysDataFile := filepath.Join(config.Rootdir, "code/sysdata.out")
	jc.copyFile(config.OutFile, sysDataFile)
	defer os.Remove(sysDataFile)

	// Copy special judge program
	spjFile := filepath.Join(config.Rootdir, "code/spj")
	jc.copyFile(filepath.Join(filepath.Dir(config.OutFile), "spj"), spjFile)
	defer os.Remove(spjFile)

	// Prepare special judge command
	runArgs := []string{
		"sandbox",
		fmt.Sprintf("--rootfs=%s", filepath.Join(config.Rootdir, "code")),
		fmt.Sprintf("--cmd=/spj %s %s %s", "data.in", "data.usr", "sysdata.out"),
		fmt.Sprintf("--time=%d", config.Timelimit),
		fmt.Sprintf("--memory=%d", config.MemoryLimit<<10),
		fmt.Sprintf("--sid=%d", jc.solutionID),
		"--cwd=/",
	}

	r, w, err := os.Pipe()
	if err != nil {
		slog.Error("Failed to create pipe for special judge", "error", err)
		return constants.OJ_SE, 0, 0
	}
	defer r.Close()

	selfName, _ := os.Executable()
	cmd := exec.Command(selfName, runArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{w}

	if err := cmd.Start(); err != nil {
		slog.Error("Failed to start special judge", "error", err)
		return constants.OJ_SE, 0, 0
	}

	w.Close()
	cmd.Wait()

	var output models.SandboxOutput
	if err := json.NewDecoder(r).Decode(&output); err != nil {
		slog.Error("Failed to decode special judge output", "error", err)
		return constants.OJ_SE, 0, 0
	}

	if output.ExitStatus == 0 {
		return constants.OJ_AC, 0, 0
	}
	return constants.OJ_WA, 0, 0
}

// copyFile copies a file from src to dst
func (jc *JudgeClient) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
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

// compareFiles compares two files
func (jc *JudgeClient) compareFiles(file1, file2 string) (int, error) {
	// This is a stub implementation
	// In a real implementation, you would read both files and compare their contents
	// Return 0 for equal, 1 for presentation error, 2 for wrong answer
	return 0, nil
}

// addRuntimeInfo adds runtime information to the database
func (jc *JudgeClient) addRuntimeInfo(solutionID int, results models.TotalResults) error {
	details, err := jc.renderResults(results)
	if err != nil {
		return fmt.Errorf("failed to render results: %w", err)
	}

	return jc.db.AddRuntimeInfo(solutionID, details)
}

// renderResults renders the test results into a formatted string
func (jc *JudgeClient) renderResults(results models.TotalResults) (string, error) {
	const tpl = `
filename|size|result|memory|time
 --|--|--|--|--
 {{- range .Results }}
| {{ .Datafile }}|0|{{ getResult .Result }}/1.00|{{ .Mem }}KB|{{ .Time }}ms
 {{- end }}
`

	funcMap := template.FuncMap{
		"getResult": constants.GetOJResultName,
	}

	t, err := template.New("result").Funcs(funcMap).Parse(tpl)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, results); err != nil {
		return "", err
	}

	return buf.String(), nil
}
