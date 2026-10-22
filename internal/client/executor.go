package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/sempr/hustoj-go/pkg/constants"
	"github.com/sempr/hustoj-go/pkg/models"
)

func (jc *JudgeClient) runAndCompare(config RunConfig) (int, int, int) {
	// 交互评测（spj=3）：玩家与 interactor 通过 FIFO 双向通信，
	// verdict 以 interactor 退出码为准，任何一侧沙箱超限/崩溃优先。
	if config.Interactive != nil {
		return jc.runInteractiveCase(config)
	}

	stdinName := "/code/data.in"
	stdoutName := "/code/data.usr"

	if config.InName != "" {
		inPath := filepath.Join(config.Workdir, config.InName)
		jc.copyFile(config.InFile, inPath)
		os.Chmod(inPath, 0o666)
		stdinName = ""
	} else {
		jc.copyFile(config.InFile, filepath.Join(config.Workdir, "data.in"))
	}

	if config.OutName != "" {
		// File-I/O 模式（freopen 固定文件名读写）：
		// 编译后 /code 目录被恢复为 0755，nobody 用户无法自行创建输出文件，
		// 故预创建输出文件并以 0666 放开写权限，供程序 freopen("..."， "w") 直接写。
		outPath := filepath.Join(config.Workdir, config.OutName)
		os.Create(outPath)
		os.Chmod(outPath, 0o666)
		stdoutName = ""
	}

	langConfig, err := jc.langManager.GetLanguageConfig(config.Lang)
	if err != nil {
		slog.Error("Failed to get language config", "error", err)
		return constants.OJ_SE, 0, 0
	}

	runArgs := []string{
		"run",
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

	if output.SystemError {
		slog.Error("Execution system error", "output", output.CombinedOutput)
		return constants.OJ_SE, 0, 0
	}

	result := verdictFromExit(output.UserStatus, output.ExitStatus)
	timeUsed := output.Time
	memUsed := output.Memory

	slog.Info("Sandbox output", "solution_id", jc.solutionID,
		"user_status", output.UserStatus, "exit_status", output.ExitStatus,
		"time", output.Time, "memory", output.Memory, "process_cnt", output.ProcessCnt)

	if result != constants.OJ_AC {
		return result, timeUsed, memUsed
	}

	// 测试运行：不与标准输出比对，程序自身状态即结果。
	if config.TestRun {
		return result, timeUsed, memUsed
	}

	if config.Spj == 1 {
		return jc.handleSpecialJudge(config)
	}

	targetOutputName := "data.usr"
	if config.OutName != "" {
		targetOutputName = config.OutName
	}

	targetInputName := "data.in"
	if config.InName != "" {
		targetInputName = config.InName
	}
	_ = targetInputName

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
		slog.Info("re result? ", "err", err)
		result = constants.OJ_RE
	}

	return result, timeUsed, memUsed
}

// verdictFromExit 把沙箱的退出码折算成判罚码。
//
// 沙箱不把"程序非零退出/被信号杀死"反映到 UserStatus：tini 在子进程被信号
// 杀死时统一 _exit(128)（tini.c:165），于是崩溃的程序会一路走到输出比对，
// 空输出与标准答案不符 → 被判成 WA。PHP 判题器在这里给 RE，Go 侧补齐同样
// 的语义，否则"运行错误"这一类结果在 Go 判题器里永远出不来。
//
// 只在 UserStatus 仍是 AC 时看退出码：TL/ML 已经是更精确的结论，不能被
// 退出码覆盖（被超时/内存限制杀死的进程退出码必然非 0）。
func verdictFromExit(userStatus, exitStatus int) int {
	if userStatus == constants.OJ_AC && exitStatus != 0 {
		return constants.OJ_RE
	}
	return userStatus
}

func (jc *JudgeClient) handleSpecialJudge(config RunConfig) (int, int, int) {
	sysDataFile := filepath.Join(config.Rootdir, "code/sysdata.out")
	jc.copyFile(config.OutFile, sysDataFile)
	defer os.Remove(sysDataFile)

	spjName := "spj"
	spjCmds := []string{"spj", "data.in", "sysdata.out", "data.usr"}
	switch config.SpjProgram {
	case constants.OJ_SPJ_PROGRAM_TPJ:
		spjName = "tpj"
		spjCmds = []string{"tpj", "data.in", "data.usr", "sysdata.out"}
	case constants.OJ_SPJ_PROGRAM_UPJ:
		spjName = "upj"
		spjCmds = []string{"upj", "data.in", "sysdata.out", "data.usr"}
	default:
		spjName = "spj"
	}

	destSpjFile := filepath.Join(config.Rootdir, "code", spjName)
	srcSpjFile := filepath.Join(filepath.Dir(config.OutFile), spjName)

	jc.copyFile(srcSpjFile, destSpjFile)
	os.Chmod(destSpjFile, 0o755)
	defer os.ReadDir(destSpjFile)

	// 与用户程序、交互器保持一致：以语言 rootfs 为根、cwd=/code 运行，判题
	// 程序放在 /code 下按其完整路径调用。
	// 注意：不能拿 <rootfs>/code 当 rootfs —— 它只是 overlay 里的普通目录、
	// 不是挂载点，tini 的 pivot_root 会返回 EINVAL，导致 SPJ 一律判 WA。
	runArgs := []string{
		"run",
		fmt.Sprintf("--rootfs=%s", config.Rootdir),
		fmt.Sprintf("--cmd=/code/%s", strings.Join(spjCmds, " ")),
		fmt.Sprintf("--time=%d", config.Timelimit),
		fmt.Sprintf("--memory=%d", config.MemoryLimit<<10),
		fmt.Sprintf("--sid=%d", jc.solutionID),
		"--cwd=/code",
		// run 子命令总会 os.Create 自己的 stdout（缺省为 /code/data.usr），
		// 那会先把选手输出截断成空文件、SPJ 再读就是空的。故给 SPJ 单开一个
		// 输出文件，保住 data.usr。
		"--stdout=/code/spj.out",
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

	exitStatus := output.ExitStatus

	slog.Info("spj result", "status", exitStatus, "program", spjName)
	if exitStatus != 0 && output.CombinedOutput != "" {
		slog.Info("spj 诊断输出", "program", spjName, "output", output.CombinedOutput)
	}

	if config.SpjProgram == constants.OJ_SPJ_PROGRAM_UPJ {
		score := float64(exitStatus) / 100.0
		slog.Info("UPJ score", "score", score)
		if exitStatus == 100 {
			return constants.OJ_AC, 0, 0
		}
		return constants.OJ_WA, 0, 0
	}

	if exitStatus == 0 {
		return constants.OJ_AC, 0, 0
	}
	return constants.OJ_WA, 0, 0
}

func (jc *JudgeClient) compareFiles(file1, file2 string) (int, error) {
	return compareFiles(file1, file2)
}

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
