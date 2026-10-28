package client

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/sempr/hustoj-go/pkg/constants"
	"github.com/sempr/hustoj-go/pkg/models"
)

// 通讯题（ICPC problem-package-format 的 custom multi-pass，spj=4）：
// 每轮把当前输入喂给选手程序跑一次，再让出题人提供的 output validator
// 检查这一轮输出。校验器 argv 为 <本轮输入> <答案文件> <工作目录>，
// 选手本轮输出接 stdin；退出码 42（或 0）表示通过并写出 nextpass.in，
// 校验器不再写 nextpass.in 时本组数据通过。
const (
	// communicationMaxPasses 与 C++ 判题器 COMM_MAX_PASS 一致。
	communicationMaxPasses   = 100
	communicationValidatorAC = 42
	// 校验器的时限/内存与选手分开：C++ 侧给校验器固定 10 秒 alarm，
	// 且不套用题目内存限制（校验器可能要读整份答案）。
	validatorTimeLimitMS   = 10000
	validatorMemoryLimitMB = 1024
	validatorFeedbackLimit = 4096
)

// CommunicationInfo 描述通讯题判题所需的外部程序。
type CommunicationInfo struct {
	// ValidatorSrc 是数据目录里的 output validator（validator 优先，
	// 回退 output_validator）；空字符串表示题目数据缺少校验器。
	ValidatorSrc string
}

// prepareCommunication 检查 output validator，并把用例配对从 .out 换成 .ans。
//
// 通讯题的答案文件常为空文件，仅作为校验器 argv 的一部分；缺 .ans 时
// 判题侧会补一个空文件，因此这里把缺失记为 ""。
func (jc *JudgeClient) prepareCommunication(dataDir string, dataFiles [][]string) (*CommunicationInfo, [][]string) {
	info := &CommunicationInfo{ValidatorSrc: findCommunicationValidator(dataDir)}
	if info.ValidatorSrc == "" {
		slog.Error("Communication problem has no output validator", "data_dir", dataDir)
	}

	paired := make([][]string, 0, len(dataFiles))
	for _, pair := range dataFiles {
		ans := strings.TrimSuffix(pair[0], ".in") + ".ans"
		if _, err := os.Stat(ans); err != nil {
			ans = ""
		}
		paired = append(paired, []string{pair[0], ans})
	}
	return info, paired
}

func findCommunicationValidator(dataDir string) string {
	for _, name := range []string{"validator", "output_validator"} {
		p := filepath.Join(dataDir, name)
		if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

// runCommunicationCase 按 ICPC 语义判定一组通讯题数据。
//
// 返回的时间是本组数据所有轮次之和，内存取各轮峰值（与 C++ 判题器的
// usedtime 累加 / topmemory 取最大一致）。
func (jc *JudgeClient) runCommunicationCase(ctx *TestContext, dataFile []string) (int, int, int) {
	cfg := ctx.RunConfig
	codeDir := cfg.Workdir
	caseName := filepath.Base(dataFile[0])

	langConfig, err := jc.langManager.GetLanguageConfig(cfg.Lang)
	if err != nil {
		slog.Error("Failed to get language config for communication judge", "error", err)
		return constants.OJ_SE, 0, 0
	}

	if ctx.Communication == nil || ctx.Communication.ValidatorSrc == "" {
		jc.appendCommMessage(ctx, "communication %s: missing output validator: put a compiled `validator` (or `output_validator`) in the problem data directory", caseName)
		return constants.OJ_RE, 0, 0
	}

	// state.txt / validator.log 在本组数据内跨轮累积，其余文件每组数据重来。
	// .pass 与 .state0/.state1 是 DOMjudge 那套 validate.h 的跨轮状态（轮次计数 + 状态文件
	// 交替写），和 Kattis 的 state.txt 一样必须在每组数据开始前清掉，否则第二组起校验器
	// 会把这一组当成上一组的下一轮来读状态。
	for _, name := range []string{
		"data.in", "data.usr", "nextpass.in", "judgemessage.txt", "teammessage.txt",
		"validator", "validator.out", "validator.err", "jury.ans",
		"state.txt", "validator.log", ".pass", ".state0", ".state1",
	} {
		os.Remove(filepath.Join(codeDir, name))
	}

	validatorSrc := ctx.Communication.ValidatorSrc
	ansSrc := dataFile[1]
	result := constants.OJ_AC
	totalTime, peakMem, passes := 0, 0, 0

	for pass := 1; pass <= communicationMaxPasses; pass++ {
		passes = pass

		passInput := dataFile[0]
		if pass > 1 {
			passInput = filepath.Join(codeDir, "nextpass.in")
		}
		if err := jc.installCommFile(passInput, filepath.Join(codeDir, "data.in"), 0o644); err != nil {
			jc.appendCommMessage(ctx, "communication %s: copy pass input failed on pass %d: %v", caseName, pass, err)
			result = constants.OJ_RE
			break
		}

		// 选手每轮只应该看到本轮的输入和自己本轮的输出。
		os.Remove(filepath.Join(codeDir, "data.usr"))

		out := jc.runCommSandbox(cfg, langConfig.Cmd.Run,
			"/code/data.in", "/code/data.usr", "",
			cfg.Timelimit, cfg.MemoryLimit, langConfig.Cmd.Env)
		totalTime += out.Time
		if out.Memory > peakMem {
			peakMem = out.Memory
		}
		if out.SystemError {
			jc.appendCommMessage(ctx, "communication %s: sandbox system error on pass %d", caseName, pass)
			result = constants.OJ_SE
			break
		}

		result = verdictFromExit(out.UserStatus, out.ExitStatus)
		slog.Info("Communication pass", "case", caseName, "pass", pass,
			"user_status", out.UserStatus, "exit_status", out.ExitStatus,
			"time_ms", out.Time, "mem_kb", out.Memory)
		if result != constants.OJ_AC {
			break
		}

		// 选手程序也在 /code 里跑，它自己造的 nextpass.in / judgemessage.txt
		// 不算数：问校验器之前清掉，之后还在的只可能是校验器本轮写出的。
		// validator.out/err 由 run 子命令在进沙箱前以 root 身份 os.Create，
		// 先删掉，避免上一轮留下的符号链接被跟随写到宿主机路径。
		os.Remove(filepath.Join(codeDir, "nextpass.in"))
		os.Remove(filepath.Join(codeDir, "judgemessage.txt"))
		os.Remove(filepath.Join(codeDir, "teammessage.txt"))
		os.Remove(filepath.Join(codeDir, "validator.out"))
		os.Remove(filepath.Join(codeDir, "validator.err"))

		// 选手与校验器共享同一个 overlay：每轮重新铺校验器与答案，
		// 选手在上一轮里替换它们不会影响到本轮判定。
		if err := jc.installCommFile(validatorSrc, filepath.Join(codeDir, "validator"), 0o755); err != nil {
			jc.appendCommMessage(ctx, "communication %s: install validator failed on pass %d: %v", caseName, pass, err)
			result = constants.OJ_RE
			break
		}
		ansDst := filepath.Join(codeDir, "jury.ans")
		os.Remove(ansDst)
		if ansSrc != "" {
			// 0644：校验器在沙箱里以 nobody 运行，需要读取答案文件。
			if err := jc.installCommFile(ansSrc, ansDst, 0o644); err != nil {
				jc.appendCommMessage(ctx, "communication %s: install answer failed on pass %d: %v", caseName, pass, err)
				result = constants.OJ_RE
				break
			}
		} else if err := os.WriteFile(ansDst, nil, 0o644); err != nil {
			jc.appendCommMessage(ctx, "communication %s: create empty answer failed on pass %d: %v", caseName, pass, err)
			result = constants.OJ_RE
			break
		}

		// 校验器与选手一样以 nobody 在沙箱内运行，而 /code 是判题器以 root
		// 创建的 0755 目录。校验器要往 argv[3] 里写 nextpass.in / state.txt /
		// judgemessage.txt，必须临时放开目录写权限（C++ 判题器的 work_dir
		// 本就属于运行用户）。下一轮选手开跑前恢复 0755，免得它在 /code 里
		// 放置符号链接等文件。
		if err := os.Chmod(codeDir, 0o777); err != nil {
			jc.appendCommMessage(ctx, "communication %s: make work directory writable failed on pass %d: %v", caseName, pass, err)
			result = constants.OJ_SE
			break
		}
		vout := jc.runCommSandbox(cfg,
			"/code/validator /code/data.in /code/jury.ans /code",
			"/code/data.usr", "/code/validator.out", "/code/validator.err",
			validatorTimeLimitMS, validatorMemoryLimitMB, nil)
		if cerr := os.Chmod(codeDir, 0o755); cerr != nil {
			slog.Warn("Failed to restore work directory mode", "dir", codeDir, "error", cerr)
		}
		// 答案文件只在本次校验期间存在：下一轮先跑选手，不能让它读到标准答案。
		os.Remove(ansDst)

		if vout.SystemError || vout.UserStatus != constants.OJ_AC {
			jc.appendCommMessage(ctx, "communication %s: validator crashed or timed out on pass %d (status=%d)", caseName, pass, vout.UserStatus)
			jc.appendValidatorFeedback(ctx, codeDir)
			result = constants.OJ_RE
			break
		}
		// 被信号杀死或 exec 失败（127）与 C++ 判题器一样算校验器崩溃。
		if vout.ExitSignal != "" || vout.ExitStatus >= 128 || vout.ExitStatus == 127 {
			jc.appendCommMessage(ctx, "communication %s: validator crashed on pass %d (exit=%d signal=%s)", caseName, pass, vout.ExitStatus, vout.ExitSignal)
			jc.appendValidatorFeedback(ctx, codeDir)
			result = constants.OJ_RE
			break
		}

		if vout.ExitStatus != communicationValidatorAC && vout.ExitStatus != 0 {
			jc.appendCommMessage(ctx, "communication %s: validator rejected pass %d (exit=%d)", caseName, pass, vout.ExitStatus)
			jc.appendValidatorFeedback(ctx, codeDir)
			result = constants.OJ_WA
			break
		}

		if _, err := os.Stat(filepath.Join(codeDir, "nextpass.in")); err != nil {
			break // 本轮通过且校验器不再出题，本组数据通过
		}
		if pass == communicationMaxPasses {
			jc.appendCommMessage(ctx, "communication %s: validator asked for more than %d passes", caseName, communicationMaxPasses)
			result = constants.OJ_WA
			break
		}
	}

	slog.Info("Communication case", "case", caseName, "passes", passes,
		"result", result, "time_ms", totalTime, "mem_kb", peakMem)
	return result, totalTime, peakMem
}

// runCommSandbox 在语言 rootfs 里跑一次命令，返回沙箱结果。
// 选手与校验器共用该通道，各自的时限/内存/标准流文件由调用方给出。
func (jc *JudgeClient) runCommSandbox(cfg RunConfig, cmdline, stdin, stdout, stderr string, timeMS, memMB int, env []string) models.SandboxOutput {
	args := []string{
		"run",
		fmt.Sprintf("--rootfs=%s", cfg.Rootdir),
		fmt.Sprintf("--cmd=%s", cmdline),
		fmt.Sprintf("--time=%d", timeMS),
		fmt.Sprintf("--memory=%d", sandboxMemoryKB(memMB)),
		fmt.Sprintf("--sid=%d", jc.solutionID),
		"--cwd=/code",
	}
	if stdin != "" {
		args = append(args, "--stdin="+stdin)
	}
	if stdout != "" {
		args = append(args, "--stdout="+stdout)
	}
	if stderr != "" {
		args = append(args, "--stderr="+stderr)
	}

	cmd, r, err := jc.spawnSandboxRun(args, env)
	if err != nil {
		slog.Error("Failed to start communication sandbox", "error", err)
		return models.SandboxOutput{SystemError: true, UserStatus: constants.OJ_SE}
	}
	return readRunOutput(cmd, r)
}

// installCommFile 把 src 复制到 overlay 内的 dst。
//
// 先删目标再创建：选手程序拥有 /code 的写权限，可能把目标换成符号链接，
// os.Create 跟随后会写到宿主机的对应路径上。
func (jc *JudgeClient) installCommFile(src, dst string, mode os.FileMode) error {
	os.Remove(dst)
	if err := jc.copyFile(src, dst); err != nil {
		return err
	}
	return os.Chmod(dst, mode)
}

// appendValidatorFeedback 把校验器留下的说明与日志附到题目的 runtimeinfo。
func (jc *JudgeClient) appendValidatorFeedback(ctx *TestContext, codeDir string) {
	for _, name := range []string{"judgemessage.txt", "teammessage.txt", "validator.log", "validator.err", "validator.out"} {
		msg := readRegularFile(filepath.Join(codeDir, name), validatorFeedbackLimit)
		if strings.TrimSpace(msg) == "" {
			continue
		}
		ctx.CommMessages = append(ctx.CommMessages,
			fmt.Sprintf("------ %s ------\n%s", name, strings.TrimRight(msg, "\n")))
	}
}

func (jc *JudgeClient) appendCommMessage(ctx *TestContext, format string, args ...any) {
	ctx.CommMessages = append(ctx.CommMessages, fmt.Sprintf(format, args...))
}

// readRegularFile 只读取普通文件：/code 里的内容是选手可写的，
// 跟随符号链接会把宿主机上的文件读出来。
func readRegularFile(path string, limit int64) string {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return ""
	}
	return string(data)
}
