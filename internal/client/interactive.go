package client

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sync"

	"github.com/sempr/hustoj-go/pkg/constants"
	"github.com/sempr/hustoj-go/pkg/models"
	"golang.org/x/sys/unix"
)

// InteractorInfo 描述一次交互评测（spj=16）的 interactor 程序与 FIFO 通道。
// 复用原有 client 使用 run 命令的模式：把 interactor 二进制与数据拷进 rootfs，
// 用两个 FIFO 充当玩家与 interactor 之间的双向管道，两个 run 各自把 FIFO
// 当作 stdin/stdout（--stdin/--stdout 为沙箱内绝对路径）。
type InteractorInfo struct {
	Cmdline  string // interactor 在沙箱内的执行命令，如 "/code/spj /code/data.in"
	P2IFifo  string // 玩家 stdout -> interactor stdin 的 FIFO（rootfs 内路径）
	I2PFifo  string // interactor stdout -> 玩家 stdin 的 FIFO（rootfs 内路径）
	Verifier bool   // 保留：verdict 以 interactor 退出码为准
}

// prepareInteractor 校验数据目录里的预编译 interactor（spj）并拷入 rootfs，
// 同时创建玩家/交互器之间的两个 FIFO。
func (jc *JudgeClient) prepareInteractor(rootfs, dataDir string) (*InteractorInfo, error) {
	spjSrc := filepath.Join(dataDir, "spj")
	if _, err := os.Stat(spjSrc); err != nil {
		return nil, fmt.Errorf("interactive judge requires a precompiled spj binary in data dir (%s): %w", spjSrc, err)
	}

	codeRootfs := filepath.Join(rootfs, "code")
	dest := filepath.Join(codeRootfs, "spj")
	if err := jc.copyFile(spjSrc, dest); err != nil {
		return nil, fmt.Errorf("failed to copy interactor into sandbox: %w", err)
	}
	if err := os.Chmod(dest, 0o755); err != nil {
		return nil, fmt.Errorf("failed to chmod interactor: %w", err)
	}

	const (
		p2iName = "pipe_po" // player out -> interactor in
		i2pName = "pipe_op" // interactor out -> player in
	)
	p2i := path.Join("/code", p2iName)
	i2p := path.Join("/code", i2pName)
	for _, fifo := range []string{filepath.Join(codeRootfs, p2iName), filepath.Join(codeRootfs, i2pName)} {
		if err := unix.Mkfifo(fifo, 0o666); err != nil && !os.IsExist(err) {
			return nil, fmt.Errorf("failed to create FIFO %s: %w", fifo, err)
		}
		if err := os.Chmod(fifo, 0o666); err != nil {
			return nil, fmt.Errorf("failed to chmod FIFO %s: %w", fifo, err)
		}
	}

	return &InteractorInfo{
		Cmdline: "/code/spj",
		P2IFifo: p2i,
		I2PFifo: i2p,
	}, nil
}

// buildRunArgs 组装一次 run 子命令参数（沿用 runAndCompare 的方式）。
// fileIn/fileOut 为沙箱内绝对路径，空字符串表示不传递对应 flag（沿用默认文件）。
func (jc *JudgeClient) buildInteractiveRunArgs(config RunConfig, cmdline, fileIn, fileOut, fileErr string, env []string) []string {
	runArgs := []string{
		"run",
		fmt.Sprintf("--rootfs=%s", config.Rootdir),
		fmt.Sprintf("--cmd=%s", cmdline),
		fmt.Sprintf("--time=%d", config.Timelimit),
		fmt.Sprintf("--memory=%d", config.MemoryLimit<<10),
		fmt.Sprintf("--sid=%d", jc.solutionID),
		"--cwd=/code",
	}
	if fileIn != "" {
		runArgs = append(runArgs, fmt.Sprintf("--stdin=%s", fileIn))
	}
	if fileOut != "" {
		runArgs = append(runArgs, fmt.Sprintf("--stdout=%s", fileOut))
	}
	if fileErr != "" {
		runArgs = append(runArgs, fmt.Sprintf("--stderr=%s", fileErr))
	}
	_ = env
	return runArgs
}

// spawnSandboxRun 以 ExtraFiles[0] 作为结果 JSON 通道启动一次 run 子进程。
func (jc *JudgeClient) spawnSandboxRun(args []string) (*exec.Cmd, *os.File, error) {
	selfName, _ := os.Executable()
	cmd := exec.Command(selfName, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	r, w, err := os.Pipe()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create result pipe: %w", err)
	}
	cmd.ExtraFiles = append(cmd.ExtraFiles, w)
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
	}
	return out
}

// runInteractiveCase 执行一次交互评测会话（一个 .in 用例）：
// 玩家程序（config.Lang 的 Main）与 interactor 通过两个 FIFO 双向通信，
// interactor 通过退出码给出 verdict（0=AC，非 0=WA；先于退出码，任何一侧
// 的沙箱超限/崩溃状态优先作为最终结果）。
func (jc *JudgeClient) runInteractiveCase(config RunConfig) (int, int, int) {
	info := config.Interactive
	if info == nil {
		slog.Error("runInteractiveCase called without interactor info")
		return constants.OJ_SE, 0, 0
	}

	// 用例数据拷入 rootfs，作为 interactor 的 argv（如 /code/data.in）。
	// 无 .in 的合成用例（交互模式单会话）不拷贝；interactor 对缺失 argv 文件需自行容错。
	dataInPath := ""
	if config.InFile != "" {
		if _, err := os.Stat(config.InFile); err != nil {
			slog.Warn("input file missing for interactive case, running without data", "path", config.InFile)
		} else {
			dataInName := "data.in"
			if config.InName != "" {
				dataInName = config.InName
			}
			dataInPath = filepath.Join("/code", dataInName)
			if err := jc.copyFile(config.InFile, filepath.Join(config.Rootdir, "code", dataInName)); err != nil {
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

	playerArgs := jc.buildInteractiveRunArgs(config, langConfig.Cmd.Run, info.I2PFifo, info.P2IFifo, "", langConfig.Cmd.Env)
	interArgs := jc.buildInteractiveRunArgs(config, interCmdline, info.P2IFifo, info.I2PFifo, "/code/spj.err", nil)

	// 先起 interactor 再起玩家：FIFO 由 run 进程的 os.Create(O_RDWR) 提供对侧写端，
	// 起两个 run 子进程后它们会自动配对，不会互锁。
	interCmd, interR, err := jc.spawnSandboxRun(interArgs)
	if err != nil {
		slog.Error("Failed to start interactor sandbox", "error", err)
		return constants.OJ_SE, 0, 0
	}
	playerCmd, playerR, err := jc.spawnSandboxRun(playerArgs)
	if err != nil {
		_ = interCmd.Process.Kill()
		slog.Error("Failed to start player sandbox", "error", err)
		return constants.OJ_SE, 0, 0
	}

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

	result := constants.OJ_AC
	switch {
	case playerOut.UserStatus != constants.OJ_AC:
		result = playerOut.UserStatus
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
