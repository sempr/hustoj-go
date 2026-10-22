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

	workDir, cleanupFunc, err := jc.setupStandaloneEnvironment(task.Source, task.Language, langConfig)
	if err != nil {
		return nil, err
	}
	defer cleanupFunc()

	// rawtext 模式：直接以源码文本评分
	if task.Spj == constants.OJ_SPJ_MODE_RAWTEXT {
		result, err := jc.runRawTextStandalone(solution, workDir, result)
		if err != nil {
			return nil, err
		}
		if err := jc.notifyFinished(result); err != nil {
			return nil, err
		}
		return result, nil
	}

	// 编译
	if err := jc.notifyPhase(constants.OJ_CI); err != nil {
		return nil, err
	}
	compileResult := jc.compile(solution.Language, workDir, langConfig)
	if compileResult.SystemError {
		result.FinalResult = constants.OJ_SE
		result.SysError = compileResult.CombinedOutput
		if err := jc.notifyCompileError(result); err != nil {
			return nil, err
		}
		return result, nil
	}
	if compileResult.ExitStatus != 0 {
		result.FinalResult = constants.OJ_CE
		result.CompileError = compileResult.CombinedOutput
		if err := jc.notifyCompileError(result); err != nil {
			return nil, err
		}
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

	if err := jc.notifyPhase(constants.OJ_RI); err != nil {
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

	if err := jc.notifyFinished(result); err != nil {
		return nil, err
	}

	return result, nil
}

// 测试运行的默认资源限制（problem_id=0 时读不到题目，用固定值）。
//
// 注意 MemoryLimit 的单位是 MB（见 RunConfig 的注释）：沙箱侧会 <<10 换成
// KB 再写进 cgroup 的 memory.max。这里若写成 KB，cgroup 的上限会被放大
// 1000 倍，内存限制形同虚设。
const (
	testRunTimeLimitMS = 10000
	testRunMemoryMB    = 256
)

// Run 执行一次数据库驱动的评测。
//
// 普通提交走 judge 流水线（RunStandalone）；测试运行只跑自定义输入，
// 不比对标准输出，也不计入题目/比赛统计。
func (jc *JudgeClient) Run() (*StandaloneResult, error) {
	if jc.customInput != nil {
		return jc.runCustomInput(*jc.customInput)
	}
	return jc.RunStandalone()
}

// runCustomInput 执行一次测试运行：编译 → 用 custominput 作 stdin 跑一次 →
// 把程序 stdout 写入 runtimeinfo（供 status-ajax 的 tr 分支展示）。
//
// 结果通过 reporter 写回数据库，与普通评测共用同一套状态流转。
func (jc *JudgeClient) runCustomInput(input string) (*StandaloneResult, error) {
	result := &StandaloneResult{Language: jc.language}

	langConfig, err := jc.langManager.GetLanguageConfig(jc.language)
	if err != nil {
		return nil, fmt.Errorf("failed to get language config: %w", err)
	}
	if err := jc.ensureRootfsReady(langConfig); err != nil {
		return nil, err
	}

	workDir, cleanupFunc, err := jc.setupStandaloneEnvironment(jc.source, jc.language, langConfig)
	if err != nil {
		return nil, err
	}
	defer cleanupFunc()

	if err := jc.notifyPhase(constants.OJ_CI); err != nil {
		return nil, err
	}
	compileResult := jc.compile(jc.language, workDir, langConfig)
	if compileResult.SystemError {
		result.FinalResult = constants.OJ_SE
		result.SysError = compileResult.CombinedOutput
		if err := jc.notifyCompileError(result); err != nil {
			return nil, err
		}
		return result, nil
	}
	if compileResult.ExitStatus != 0 {
		result.FinalResult = constants.OJ_CE
		result.CompileError = compileResult.CombinedOutput
		if err := jc.notifyCompileError(result); err != nil {
			return nil, err
		}
		return result, nil
	}

	// 自定义输入落成临时文件，交给 runAndCompare 复制为 data.in。
	tmp, err := os.CreateTemp("", "hustoj-testrun-*.in")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp input: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(input); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("failed to write temp input: %w", err)
	}
	tmp.Close()

	rc := RunConfig{
		Lang:        jc.language,
		Rootdir:     workDir,
		Workdir:     filepath.Join(workDir, "code"),
		InFile:      tmp.Name(),
		Timelimit:   testRunTimeLimitMS,
		MemoryLimit: testRunMemoryMB,
		TestRun:     true,
	}
	res, timeUsed, memUsed := jc.runAndCompare(rc)

	// 程序 stdout 被写到 rootfs 内 /code/data.usr（见 runAndCompare 的 stdoutName）。
	out, _ := os.ReadFile(filepath.Join(workDir, "code", "data.usr"))
	result.FinalResult = res
	result.TimeMs = timeUsed
	result.MemoryKB = memUsed
	result.PassRate = 1.0
	result.RuntimeInfo = string(out)

	if err := jc.notifyFinished(result); err != nil {
		return nil, err
	}
	return result, nil
}

// setupStandaloneEnvironment 为独立评测准备 overlay 工作环境并写入源码。
func (jc *JudgeClient) setupStandaloneEnvironment(source string, languageID int, langConfig *language.LangConfig) (string, func(), error) {
	workDir, err := jc.setupWorkEnvironment(langConfig)
	if err != nil {
		return "", nil, fmt.Errorf("failed to setup work environment: %w", err)
	}

	if err := jc.writeSourceCode(source, languageID, workDir); err != nil {
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
