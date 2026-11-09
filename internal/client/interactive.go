package client

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/sempr/hustoj-go/pkg/constants"
	"github.com/sempr/hustoj-go/pkg/language"
	"github.com/sempr/hustoj-go/pkg/models"
)

// InteractorInfo 描述一次交互评测（spj=3）的 interactor。
//
// 选手和 interactor 各自运行在自己的 rootfs overlay 里（共享只读 lower，
// 但 upper/tmpfs 独立），所以选手看不到 spj 和 data.in。两者的 stdin/stdout
// 由 client 创建的两条匿名管道连接，fd 通过 ExtraFiles 传给 run，不需要在
// 文件系统里创建 FIFO。
type InteractorInfo struct {
	Cmdline     string // interactor 在沙箱内的执行命令，如 "/code/spj /code/data.in"
	ActorRootfs string // interactor 独立的 rootfs
	SpjErrPath  string // interactor stderr 文件（在 ActorRootfs 内）
	Cleanup     func()
}

// prepareInteractor 校验数据目录里的预编译 interactor（spj），为它准备一套
// 独立的 overlay，并把 spj 拷进去。交互管道在每次会话时由 client 用
// os.Pipe() 创建，不落文件。
func (jc *JudgeClient) prepareInteractor(rootfs, dataDir string, langConfig *language.LangConfig) (*InteractorInfo, error) {
	spjSrc := filepath.Join(dataDir, "spj")
	if _, err := os.Stat(spjSrc); err != nil {
		return nil, fmt.Errorf("interactive judge requires a precompiled spj binary in data dir (%s): %w", spjSrc, err)
	}

	actorRootfs, err := jc.mountOverlay(filepath.Join(filepath.Dir(rootfs), "actor"), langConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to setup interactor rootfs: %w", err)
	}
	cleanup := func() { jc.cleanupWorkEnvironment(actorRootfs) }

	codeRootfs := filepath.Join(actorRootfs, "code")
	dest := filepath.Join(codeRootfs, "spj")
	// 先删除再用 O_NOFOLLOW 创建，避免顺着残留链接 chmod/copy。
	if err := os.Remove(dest); err != nil && !os.IsNotExist(err) {
		cleanup()
		return nil, fmt.Errorf("failed to remove stale interactor: %w", err)
	}
	if err := copyFileWithMode(spjSrc, dest, 0o755); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to copy interactor into sandbox: %w", err)
	}

	return &InteractorInfo{
		Cmdline:     "/code/spj",
		ActorRootfs: actorRootfs,
		SpjErrPath:  filepath.Join(codeRootfs, "spj.err"),
		Cleanup:     cleanup,
	}, nil
}

// buildInteractiveRunArgs 组装一次交互评测的 run 子命令参数。
// stdin/stdout 走 --inherit-io（fd 4/5 由 spawnSandboxRun 传入匿名管道）；
// fileErr 非空时指定 stderr 文件（沙箱内绝对路径）。
func (jc *JudgeClient) buildInteractiveRunArgs(rootfs string, config RunConfig, cmdline, fileErr string) []string {
	runArgs := []string{
		"run",
		fmt.Sprintf("--rootfs=%s", rootfs),
		fmt.Sprintf("--cmd=%s", cmdline),
		fmt.Sprintf("--time=%d", config.Timelimit),
		fmt.Sprintf("--memory=%d", sandboxMemoryKB(config.MemoryLimit)),
		fmt.Sprintf("--sid=%d", jc.solutionID),
		"--cwd=/code",
		"--inherit-io",
	}
	if fileErr != "" {
		runArgs = append(runArgs, fmt.Sprintf("--stderr=%s", fileErr))
	}
	return runArgs
}

// spawnSandboxRun 以 ExtraFiles[0] 作为结果 JSON 通道启动一次 run 子进程。
// extraFiles 依次成为 run 的 fd 4、5、6...（交互评测传两个匿名管道）。
// extraFiles 的所有权仍归调用方，启动后由调用方关闭。
func (jc *JudgeClient) spawnSandboxRun(args, env []string, extraFiles ...*os.File) (*exec.Cmd, *os.File, error) {
	selfName, _ := os.Executable()
	cmd := exec.Command(selfName, args...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	r, w, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create result pipe: %w", err)
	}
	cmd.ExtraFiles = append(cmd.ExtraFiles, w)
	cmd.ExtraFiles = append(cmd.ExtraFiles, extraFiles...)
	if err := cmd.Start(); err != nil {
		w.Close()
		r.Close()
		return nil, nil, fmt.Errorf("failed to start run process: %w", err)
	}
	w.Close()
	return cmd, r, nil
}

// readRunOutput 等待 run 子进程结束并解码其 sandbox 结果（fd3 JSON）。
func readRunOutput(cmd *exec.Cmd, r *os.File) models.SandboxOutput {
	defer r.Close()
	var out models.SandboxOutput
	if err := cmd.Wait(); err != nil {
		slog.Error("run process wait failed", "error", err)
	}
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		slog.Warn("failed to decode run output", "error", err)
		out.SystemError = true
		out.UserStatus = constants.OJ_SE
	}
	return out
}

// runInteractiveCase 执行一次交互评测会话（一个 .in 用例）：
// 玩家程序（config.Lang 的 Main）与 interactor 通过两条匿名管道双向通信，
// interactor 通过退出码给出 verdict（0=AC，非 0=WA；超过退出码，任何一侧
// 的沙箱超限/崩溃状态优先作为最终结果）。
func (jc *JudgeClient) runInteractiveCase(config RunConfig) (int, int, int) {
	info := config.Interactive
	if info == nil {
		slog.Error("runInteractiveCase called without interactor info")
		return constants.OJ_SE, 0, 0
	}

	// 用例数据拷进 interactor 的 rootfs，作为它的 argv（如 /code/data.in）。
	// 选手 rootfs 里没有这份数据。
	// 无 .in 的合成用例（交互模式单会话）不拷贝；interactor 对缺失 argv 文件需自行容错。
	dataInPath := ""
	if config.InFile != "" {
		if _, err := os.Stat(config.InFile); err != nil {
			slog.Warn("input file missing for interactive case, running without data", "path", config.InFile)
		} else {
			dataInName := "data.in"
			if config.InName != "" {
				dataInName = sanitizeTestDataName(config.InName)
				if dataInName == "" {
					slog.Error("交互题 input.name 非法", "value", config.InName)
					return constants.OJ_SE, 0, 0
				}
			}
			dataInPath = filepath.Join("/code", dataInName)
			if err := copyFileWithMode(config.InFile, filepath.Join(info.ActorRootfs, "code", dataInName), 0); err != nil {
				slog.Error("Failed to copy input data for interactive judge", "error", err)
				return constants.OJ_SE, 0, 0
			}
		}
	}

	langConfig, err := jc.langManager.GetLanguageConfig(config.Lang)
	if err != nil {
		slog.Error("Failed to get language config", "error", err)
		return constants.OJ_SE, 0, 0
	}

	// interactor 命令：/code/spj [/code/data.in]
	interCmdline := info.Cmdline
	if dataInPath != "" {
		interCmdline = info.Cmdline + " " + dataInPath
	}

	// 两条匿名管道：p2i 选手 stdout -> interactor stdin；i2p 反之。
	p2iR, p2iW, err := os.Pipe()
	if err != nil {
		slog.Error("Failed to create player->interactor pipe", "error", err)
		return constants.OJ_SE, 0, 0
	}
	i2pR, i2pW, err := os.Pipe()
	if err != nil {
		p2iR.Close()
		p2iW.Close()
		slog.Error("Failed to create interactor->player pipe", "error", err)
		return constants.OJ_SE, 0, 0
	}
	closePipes := func() {
		p2iR.Close()
		p2iW.Close()
		i2pR.Close()
		i2pW.Close()
	}

	// 选手与 interactor 使用各自独立的 rootfs；stdin/stdout 用匿名管道 fd。
	playerArgs := jc.buildInteractiveRunArgs(config.Rootdir, config, langConfig.Cmd.Run, "")
	interArgs := jc.buildInteractiveRunArgs(info.ActorRootfs, config, interCmdline, "/code/spj.err")

	interCmd, interR, err := jc.spawnSandboxRun(interArgs, nil, p2iR, i2pW)
	if err != nil {
		closePipes()
		slog.Error("Failed to start interactor sandbox", "error", err)
		return constants.OJ_SE, 0, 0
	}
	playerCmd, playerR, err := jc.spawnSandboxRun(playerArgs, langConfig.Cmd.Env, i2pR, p2iW)
	if err != nil {
		closePipes()
		_ = interCmd.Process.Kill()
		interR.Close()
		slog.Error("Failed to start player sandbox", "error", err)
		return constants.OJ_SE, 0, 0
	}
	// 子进程已各自继承了需要的 fd，父进程必须关掉全部管道端，
	// 否则读端永远等不到 EOF，交互会挂住。
	closePipes()

	var playerOut, interOut models.SandboxOutput
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		playerOut = readRunOutput(playerCmd, playerR)
	}()
	go func() {
		defer wg.Done()
		interOut = readRunOutput(interCmd, interR)
	}()
	wg.Wait()

	// 选手侧和普通评测一样，非零退出/被信号杀死应判 RE；交互器侧退出码
	// 才是 verdict（0=AC，非 0=WA）。不区分两者会把 pivot_root 失败之类
	// 的沙箱故障误判成选手 WA。
	playerResult := verdictFromExit(playerOut.UserStatus, playerOut.ExitStatus)
	result := constants.OJ_AC
	switch {
	case playerResult != constants.OJ_AC:
		result = playerResult
	case interOut.UserStatus != constants.OJ_AC:
		result = interOut.UserStatus
	case interOut.ExitStatus == 0:
		result = constants.OJ_AC
	default:
		result = constants.OJ_WA
	}

	timeUsed := playerOut.Time
	if interOut.Time > timeUsed {
		timeUsed = interOut.Time
	}
	memUsed := playerOut.Memory
	if interOut.Memory > memUsed {
		memUsed = interOut.Memory
	}

	slog.Info("Interactive judge case", "final_result", result, "player_status", playerOut.UserStatus, "interactor_status", interOut.UserStatus)
	if result != constants.OJ_AC {
		// 交互器用退出码给出 verdict，原因写在 /code/spj.err（--stderr 指向它，
		// 不在默认的 data.err 里）。这里把两侧输出带出来，否则偶发 WA 只有
		// "答案错误"四个字，没法定位。
		spjErr := string(readRegularFile(info.SpjErrPath, 1024))
		slog.Warn("Interactive judge case failed",
			"final_result", result,
			"player_status", playerOut.UserStatus, "player_exit", playerOut.ExitStatus,
			"player_output", playerOut.CombinedOutput,
			"interactor_status", interOut.UserStatus, "interactor_exit", interOut.ExitStatus,
			"interactor_output", interOut.CombinedOutput,
			"interactor_stderr", spjErr)
	}
	return result, timeUsed, memUsed
}

// interactiveDataFiles 处理交互评测的测试用例列表：没有 .in 文件时注入一条
// 合成用例，保证交互评测至少要跑一次完整会话。
func (jc *JudgeClient) interactiveDataFiles(dataDir string, dataFiles [][]string) [][]string {
	if len(dataFiles) > 0 {
		return dataFiles
	}
	slog.Warn("No .in data files for interactive judge, running a single session", "data_dir", dataDir)
	return [][]string{{filepath.Join(dataDir, "case1.in"), ""}}
}
