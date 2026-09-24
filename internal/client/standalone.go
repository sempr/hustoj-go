package client

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/sempr/hustoj-go/pkg/constants"
	"github.com/sempr/hustoj-go/pkg/language"
	"github.com/sempr/hustoj-go/pkg/rawtext"
	"github.com/sempr/hustoj-go/pkg/repository"
	"github.com/sempr/hustoj-go/pkg/subtask"
)

// StandaloneCase 是单个测试点的结果。
type StandaloneCase struct {
	Datafile string `json:"datafile"`
	Result   int    `json:"result"`
	Time     int    `json:"time"`
	Mem      int    `json:"mem"`
}

// StandaloneResult 是独立评测的完整输出数据。
// 当前仅输出数据内容，展示格式留待后续调整。
type StandaloneResult struct {
	ProblemID    int              `json:"problem_id"`
	Language     int              `json:"language"`
	FinalResult  int              `json:"final_result"`
	PassRate     float64          `json:"pass_rate"`
	Score        float64          `json:"score,omitempty"`
	TotalScore   float64          `json:"total_score,omitempty"`
	TimeMs       int              `json:"time_ms"`
	MemoryKB     int              `json:"memory_kb"`
	Results      []StandaloneCase `json:"results,omitempty"`
	CompileError string           `json:"compile_error,omitempty"`
	RuntimeInfo  string           `json:"runtime_info,omitempty"`
	SysError     string           `json:"sys_error,omitempty"`
}

// RunStandalone 执行一次不依赖数据库的评测任务，返回评测结果数据（不负责输出）。
func (jc *JudgeClient) RunStandalone() (*StandaloneResult, error) {
	task := jc.task
	if task == nil {
		return nil, errors.New("RunStandalone requires a JudgeTask")
	}
	if task.DataDir == "" {
		return nil, errors.New("no test data directory provided, use --data")
	}

	result := &StandaloneResult{
		Language: task.Language,
	}

	solution := &repository.Solution{
		ID:        jc.solutionID,
		ProblemID: 0,
		UserID:    "standalone",
		Language:  task.Language,
		ContestID: 0,
	}
	problem := &repository.Problem{
		ID:        0,
		TimeLimit: float64(task.TimeLimit) / 1000.0,
		MemLimit:  task.MemLimitKB / 1024,
		SPJ:       task.Spj,
	}

	langConfig, err := jc.langManager.GetLanguageConfig(task.Language)
	if err != nil {
		return nil, fmt.Errorf("failed to get language config: %w", err)
	}

	if err := jc.ensureRootfsReady(langConfig); err != nil {
		return nil, err
	}

	workDir, cleanupFunc, err := jc.setupStandaloneEnvironment(solution, langConfig)
	if err != nil {
		return nil, err
	}
	defer cleanupFunc()

	// rawtext 模式：直接以源码文本评分
	if task.Spj == constants.OJ_SPJ_MODE_RAWTEXT {
		return jc.runRawTextStandalone(solution, workDir, result)
	}

	// 编译
	compileResult := jc.compile(solution.Language, workDir, langConfig)
	if compileResult.SystemError {
		result.FinalResult = constants.OJ_SE
		result.SysError = compileResult.CombinedOutput
		return result, nil
	}
	if compileResult.ExitStatus != 0 {
		result.FinalResult = constants.OJ_CE
		result.CompileError = compileResult.CombinedOutput
		return result, nil
	}
	slog.Info("compile ok result", "result", compileResult)

	// SPJ 程序类型探测
	spjProgram := 0
	if task.Spj == constants.OJ_SPJ_MODE_SPJ {
		spjProgram = jc.detectSpjType(task.DataDir)
	}

	// 执行测试用例
	ctx, err := jc.prepareTestContext(solution, problem, workDir, spjProgram)
	if err != nil {
		return nil, err
	}

	testResults, totalResults, stats, err := jc.executeAllTestCases(ctx)
	if err != nil {
		return nil, err
	}

	subtaskScore := subtask.Judge(testResults, jc.determineOIModeFromFiles(testResults))
	totalResults.FinalResult = subtaskScore.FinalResult
	passRate := subtaskScore.PassRate

	result.FinalResult = totalResults.FinalResult
	result.PassRate = passRate
	result.Score = subtaskScore.GetMark
	result.TotalScore = subtaskScore.TotalMark
	result.TimeMs = stats.TotalTime
	result.MemoryKB = stats.PeakMemory
	result.Results = make([]StandaloneCase, 0, len(testResults))
	for _, tr := range testResults {
		result.Results = append(result.Results, StandaloneCase{
			Datafile: tr.Filename,
			Result:   tr.Result,
			Time:     tr.Time,
			Mem:      tr.Mem,
		})
	}

	if details, err := jc.renderResults(totalResults); err == nil {
		result.RuntimeInfo = details
	}

	slog.Info("Standalone judge completed",
		"final_result", totalResults.FinalResult,
		"total_time_ms", stats.TotalTime,
		"peak_memory_kb", stats.PeakMemory,
		"pass_rate", passRate,
	)

	return result, nil
}

// setupStandaloneEnvironment 为独立评测准备 overlay 工作环境并写入源码。
func (jc *JudgeClient) setupStandaloneEnvironment(solution *repository.Solution, langConfig *language.LangConfig) (string, func(), error) {
	workDir, err := jc.setupWorkEnvironment(langConfig)
	if err != nil {
		return "", nil, fmt.Errorf("failed to setup work environment: %w", err)
	}

	if err := jc.writeSourceCode(jc.task.Source, solution.Language, workDir); err != nil {
		jc.cleanupWorkEnvironment(workDir)
		return "", nil, fmt.Errorf("failed to write source code: %w", err)
	}

	cleanupFunc := func() {
		jc.cleanupWorkEnvironment(workDir)
	}

	return workDir, cleanupFunc, nil
}

// runRawTextStandalone 处理 rawtext 类型的独立评测。
func (jc *JudgeClient) runRawTextStandalone(solution *repository.Solution, workDir string, result *StandaloneResult) (*StandaloneResult, error) {
	langBasic, err := jc.langManager.GetLanguageBasic(solution.Language)
	if err != nil {
		return nil, err
	}

	mainFile := filepath.Join(workDir, "code", fmt.Sprintf("Main%s", langBasic.Suffix))
	details, userScore, totalScore, err := rawtext.RawTextJudge(
		filepath.Join(jc.task.DataDir, "data.in"),
		filepath.Join(jc.task.DataDir, "data.out"),
		mainFile,
	)
	if err != nil {
		slog.Error("Rawtext judge error", "error", err)
		result.FinalResult = constants.OJ_RE
		result.RuntimeInfo = fmt.Sprintf("rawtext judge error: %v", err)
		return result, nil
	}

	result.FinalResult = constants.OJ_AC
	result.PassRate = 1.0
	if userScore < totalScore {
		result.FinalResult = constants.OJ_WA
		result.PassRate = float64(userScore) / float64(totalScore)
	}
	result.Score = userScore
	result.TotalScore = totalScore
	result.RuntimeInfo = details

	return result, nil
}

// ensureRootfsReady 校验语言 rootfs（overlay 的 lowerdir）是否已构建。
func (jc *JudgeClient) ensureRootfsReady(langConfig *language.LangConfig) error {
	base := langConfig.Fs.Base
	if base == "" {
		return fmt.Errorf("language config has empty fs.base")
	}
	for _, layer := range filepath.SplitList(base) {
		if _, err := os.Stat(layer); err != nil {
			return fmt.Errorf("language rootfs not found (base=%s): %w\nplease build it first, e.g. run: extra/build_rootfs.sh <lang_id>", layer, err)
		}
	}
	return nil
}
