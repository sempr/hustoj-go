package run

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/sempr/hustoj-go/internal/shared"
	"github.com/sempr/hustoj-go/pkg/constants"
	"github.com/sempr/hustoj-go/pkg/models"
	"golang.org/x/sys/unix"
)

func RunMain(cfg *models.SandboxArgs) {
	slog.Info("The ENV: ", "envs", os.Environ())
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	file3 := os.NewFile(uintptr(3), "fd3")
	if file3 == nil {
		slog.Warn("fd3 is nil, cannot write output")
	}
	defer file3.Close()

	outerWorkdir := path.Join(cfg.Rootfs, cfg.Workdir)
	inputfile := path.Join(outerWorkdir, "data.in")
	outputfile := path.Join(outerWorkdir, "data.usr")
	errorfile := path.Join(outerWorkdir, "data.err")

	// --stdin/--stdout/--stderr 为沙箱内绝对路径（如 /code/xxx），
	// 供交互评测等场景把输入/输出指向 rootfs 内的 FIFO；为空则用默认文件。
	if cfg.Stdin != "" {
		inputfile = path.Join(cfg.Rootfs, path.Clean(cfg.Stdin))
	}
	if cfg.Stdout != "" {
		outputfile = path.Join(cfg.Rootfs, path.Clean(cfg.Stdout))
	}
	if cfg.Stderr != "" {
		errorfile = path.Join(cfg.Rootfs, path.Clean(cfg.Stderr))
	}

	pr, pw, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	defer pr.Close()
	defer pw.Close()
	// 先开输出/错误、后开输入：FIFO 场景下 os.Open(O_RDONLY) 会阻塞等待写端，
	// 若输入在前会造成两侧 run 互相阻塞的循环死锁；output 用 os.Create(O_RDWR)
	// 打开 FIFO 不阻塞，能先向对侧提供写端。普通文件场景顺序无关。
	fo, err := os.Create(outputfile)
	if err != nil {
		panic(fmt.Errorf("create output file %s: %w", outputfile, err))
	}
	defer fo.Close()
	fe, err := os.Create(errorfile)
	if err != nil {
		panic(fmt.Errorf("create error file %s: %w", errorfile, err))
	}
	defer fe.Close()
	fi, err := os.Open(inputfile)
	if err != nil {
		if os.IsNotExist(err) {
			// 编译等不需要 stdin 的阶段没有 data.in；退化为 /dev/null，
			// 否则这里 panic 会让 fd3 收不到 JSON，编译被误判为"成功"、
			// 产物缺失，最终所有用例都判 WA。
			if devNull, derr := os.Open("/dev/null"); derr == nil {
				fi, err = devNull, nil
			}
		}
		if err != nil {
			panic(fmt.Errorf("open input file %s: %w", inputfile, err))
		}
	}
	defer fi.Close()

	cmdline := strings.Split(cfg.Command, " ")

	cmds := []string{
		"--control-fd", "3",
		"--stdin-fd", "4",
		"--stdout-fd", "5",
		"--stderr-fd", "6",
		// "--uid", "0",
		// "--gid", "0",
		"--cwd", cfg.Workdir,
		"--rootfs", cfg.Rootfs,
		"--",
	}
	cmds = append(cmds, cmdline...)

	cmd := exec.Command(
		"/usr/local/bin/tini",
		cmds...,
	)
	cmd.ExtraFiles = append(cmd.ExtraFiles, pr, fi, fo, fe)
	devNull := path.Join(cfg.Rootfs, "dev", "null")
	if err := os.MkdirAll(path.Dir(devNull), 0o755); err != nil {
		slog.Error("create dev dir failed", "err", err)
		return
	}
	if _, err := os.Lstat(devNull); err != nil {
		if err := unix.Mknod(devNull, unix.S_IFCHR|0o666, int(unix.Mkdev(1, 3))); err != nil {
			slog.Error("mknod /dev/null failed", "err", err)
			return
		}
	}

	cmd.Env = append(cmd.Env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin")
	cmd.Env = append(cmd.Env, os.Environ()...)
	cmd.SysProcAttr = &unix.SysProcAttr{
		Cloneflags: unix.CLONE_NEWNS | unix.CLONE_NEWNET | unix.CLONE_NEWUTS | unix.CLONE_NEWIPC | unix.CLONE_NEWPID,
		Setpgid:    true,
		Ptrace:     true,
	}

	// 输出大小上限：给子进程加 RLIMIT_FSIZE，程序写超了会收到 SIGXFSZ，
	// 由此判定"输出超限"。不这样做的话，输出写在 tmpfs 上（占内存），
	// 只会撞上内存限制，输出超限这一类结果永远出不来。
	//
	// 这里设在本进程上、子进程继承；本进程自己只写几百字节的 JSON，不受影响。
	if err := unix.Setrlimit(unix.RLIMIT_FSIZE, &unix.Rlimit{
		Cur: uint64(constants.MaxOutputSize),
		Max: uint64(constants.MaxOutputSize),
	}); err != nil {
		slog.Warn("设置 RLIMIT_FSIZE 失败", "err", err)
	}

	if err := cmd.Start(); err != nil {
		slog.Error("cmd start failed", "err", err)
		return
	}

	stopFunc := func() error {
		// 写 int32 3 (little‑endian)
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, 2)
		n, err := pw.Write(b)
		if err != nil {
			slog.Warn("写 control‑fd 失败", "err", err)
			return err
		}
		if n != 4 {
			return fmt.Errorf("control‑fd 写入了 %d 字节，期望 4", n)
		}
		slog.Info("已写入 control‑fd，命令 3 (SIGTERM)")
		// 部分进程（如 node）可能捕获/忽略 SIGTERM，宽限期后强制 SIGKILL，
		// 避免 PID 命名空间残留进程占用 overlay/tmpfs 导致无法 umount。
		go func() {
			time.Sleep(500 * time.Millisecond)
			b := make([]byte, 4)
			binary.LittleEndian.PutUint32(b, 1)
			if _, err := pw.Write(b); err != nil {
				slog.Warn("写 control‑fd（SIGKILL）失败", "err", err)
			}
		}()
		return nil
	}

	mainPid := cmd.Process.Pid
	processCnt := 1
	pids := make(map[int]int)
	pids[mainPid] = 1
	ptree := make(map[int][]int)
	ptreename := make(map[int]string)
	ptree[mainPid] = []int{}
	ptreename[mainPid] = "main"

	// first wait here
	var ws unix.WaitStatus
	pidTmp, err := unix.Wait4(-1, &ws, 0, nil)
	slog.Debug("tracing(first wait)", "pid", pidTmp, "ws", fmt.Appendf(nil, "%X", ws))
	if err != nil {
		panic(fmt.Errorf("wait4 first tracee: pid=%d: %w", pidTmp, err))
	}
	err = unix.PtraceSetOptions(mainPid, unix.PTRACE_O_EXITKILL|unix.PTRACE_O_TRACECLONE|unix.PTRACE_O_TRACEFORK|unix.PTRACE_O_TRACEVFORK|unix.PTRACE_O_TRACEVFORKDONE|unix.PTRACE_O_TRACEEXIT|unix.PTRACE_O_TRACESYSGOOD|unix.PTRACE_O_TRACESECCOMP|unix.PTRACE_O_TRACEEXEC)
	if err != nil {
		slog.Error("ptrace setoptions failed", "mainPid", mainPid, "pidTmp", pidTmp)
	}

	// cgroup 的硬限制 = 题目内存上限 + 输出上限。
	//
	// 输出写在沙箱的 tmpfs 上，和程序用的是同一份内存额度；不留这份余量
	// 的话，输出多的程序会先被 OOM 杀掉，判成"内存超限"而不是"输出超限"。
	// 真正判定内存超限时用的是采样到的匿名内存峰值（不含输出）。
	memLimitBytes := cfg.MemoryLimit*1024 + constants.MaxOutputSize
	cg, err := shared.NewCgroup(cfg.SolutionId, mainPid, memLimitBytes, slog.Default())
	if err != nil {
		slog.Error("cg create failed", "err", err)
		return
	}
	defer cg.Cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ccerr error = nil
	go func() {
		if ccerr = cg.RunCPUChecker(
			ctx,
			time.Duration(cfg.TimeLimit*int(time.Millisecond)),
			time.Duration(3*cfg.TimeLimit*int(time.Millisecond)),
			slog.Default(),
			stopFunc,
		); ccerr != nil {
			slog.Info("ccerror", "ccerr", ccerr)
		}
	}()
	startTime := time.Now()
	if err := unix.PtraceCont(pidTmp, int(ws.StopSignal())); err != nil {
		slog.Error("ptraceCont error", "err", err)
	}

	// mainExitStatus 单独记一份主进程（tini）的退出码。
	//
	// ptrace 循环通常以 wait4 报错（ECHILD）收尾，而 wait4 失败会把 status
	// 缓冲区清零 —— 循环里的 ws 到这时已经不可信，退出码会凭空变成 0，
	// "程序崩溃/非零退出"这类信息就丢了。
	mainExitStatus := 0
	// outputLimitExceeded 表示程序因为写超 RLIMIT_FSIZE 被 SIGXFSZ 打断。
	outputLimitExceeded := false
	for {
		var ru unix.Rusage
		pidTmp, err := unix.Wait4(-1, &ws, 0, &ru)
		if err != nil {
			slog.Error("wait4 error", "pid", pidTmp, "err", err)
			break
		}

		slog.Debug("tracing", "pid", pidTmp, "ws", fmt.Appendf(nil, "%X", ws))
		if ws.Exited() {
			slog.Debug("process exit ", "pid", pidTmp, "exitCode", ws.ExitStatus())
			processCnt--
			if pidTmp == mainPid {
				mainExitStatus = ws.ExitStatus()
			}
			if processCnt == 1 {
				stopFunc()
				continue
			}
			if pidTmp == mainPid {
				break
			}
			continue
		}
		if ws.Signaled() {
			slog.Debug("process signaled", "pid", pidTmp, "signal", ws.Signal(), "status", ws.ExitStatus(), "exit", ws.Exited())
			if ws.Signal()&0x7f == unix.SIGXFSZ {
				// 写超了 RLIMIT_FSIZE：输出超限。
				outputLimitExceeded = true
				unix.Kill(-mainPid, unix.SIGKILL)
				continue
			}
			if pidTmp == mainPid {
				mainExitStatus = exitStatusOf(ws)
				break
			}
			continue
		}
		if ws.Stopped() {
			slog.Debug("process stopped", "pid", pidTmp, "signal", ws.StopSignal(), "signal", ws.StopSignal()&0x7f)
			stopsig := ws.StopSignal()
			if stopsig == unix.SIGSEGV {
			}
			if stopsig == (unix.SIGTRAP | 0x80) {
				slog.Info("got stopsig=85, just do Ptrace")
				unix.PtraceCont(pidTmp, 0)
				continue
			}

			if stopsig == unix.SIGTRAP {
				eventNumber := int(ws >> 16)
				if eventNumber != 0 {
					msg, err := unix.PtraceGetEventMsg(pidTmp)
					slog.Info("get event msg", "msg", msg, "err", err, "pid", pidTmp, "eventNumber", eventNumber)

					cloneEvents := []int{unix.PTRACE_EVENT_CLONE, unix.PTRACE_EVENT_FORK, unix.PTRACE_EVENT_VFORK}
					isCloneEvent := false
					if slices.Contains(cloneEvents, eventNumber) {
						isCloneEvent = true
					}
					if isCloneEvent {
						slog.Info("trap event, clone/fork/vfork", "stopsig", stopsig, "eventnumber", eventNumber)
						ptreename[int(msg)] = ptreename[pidTmp]
						ptree[int(msg)] = []int{}
						ptree[pidTmp] = append(ptree[pidTmp], int(msg))
						processCnt++
					} else if eventNumber == unix.PTRACE_EVENT_EXEC {
						slog.Info("trace event: exec", "eventNumber", eventNumber)
						data, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", msg))
						args := strings.Split(string(data), "\x00")
						slog.Info("exec argv:", "args", args, "pidTmp", pidTmp, "msg", msg)
						ptreename[pidTmp] = strings.Join(args, " ")
					} else if eventNumber == unix.PTRACE_EVENT_VFORK_DONE {
						slog.Info("trace event: vfork-done", "eventNumber", eventNumber)
					} else if eventNumber == unix.PTRACE_EVENT_EXIT {
						slog.Info("trace event: exit", "eventNumber", eventNumber)
					} else {
						slog.Info("trace event: todo", "eventNumber", eventNumber)
					}
				}
			}
			err := unix.PtraceCont(pidTmp, int(stopsig))
			if err != nil {
				slog.Error("ptraceCont failed: ", "err", err, "pid", pidTmp)
			}
			if ws.StopSignal() == unix.SIGURG {
				unix.Kill(pidTmp, syscall.SIGCONT)
			}
		}
	}

	var showPtree func(int, int)
	showPtree = func(pid int, depth int) {
		for range depth * 2 {
			fmt.Printf(" ")
		}
		fmt.Println(pid, " ", ptreename[pid])
		for _, v := range ptree[pid] {
			showPtree(v, depth+1)
		}
	}
	fmt.Printf("ccerr = %v\n", ccerr)
	// 循环结束后兜底清理：确保 PID 命名空间内无残留进程占用 rootfs。
	unix.Kill(-mainPid, unix.SIGKILL)
	// showPtree(mainPid, 0)
	tt, err1 := cg.ReadCPUtime()
	// 内存用量走 cgroup 口径，和 memory.max 限制的是同一把尺子。
	//
	//  - anonPeak：沙箱里所有进程的堆/栈之和。多进程程序（bash 拉一堆子
	//    进程）算的是总和，正好和限制的口径一致；不含 tmpfs 上的输出。
	//  - totalPeak - shmem：cgroup 内存总量减去 tmpfs 上的输出，用来兜住
	//    采样没抓到的峰值。
	// 两者取大者。
	//
	// 不能用 getrusage(RUSAGE_CHILDREN).ru_maxrss：Go 的 fork+exec 会让子
	// 进程在 exec 前继承父进程（Go 运行时）的 CoW 页面，这段水位线会被记成
	// 子进程的内存峰值，hello-world 会被报成 20MB。
	anonPeak := cg.PeakMemoryAnon()
	totalPeak, _ := cg.ReadMemoryPeak()
	shmem, _ := cg.ReadMemoryShmem()
	programBytes := anonPeak
	if est := int64(totalPeak) - shmem; est > programBytes {
		programBytes = est
	}
	memKB := int(programBytes / 1024)
	// 被 OOM killer 杀掉是"内存吃超了"的硬证据（采样可能正好卡在上限边上）。
	oomKilled, _ := cg.OOMKilled()
	slog.Error("CPU: ", "time", tt, "error", err1, "walltime", time.Since(startTime),
		"mem_kb", memKB, "anon_kb", anonPeak/1024, "total_kb", totalPeak/1024,
		"shmem_kb", shmem/1024, "oom_kill", oomKilled)

	out := buildOutput(ccerr, tt, memKB, cfg, len(ptree), outerWorkdir, mainExitStatus, outputLimitExceeded, oomKilled > 0)
	json.NewEncoder(file3).Encode(out)
}

// exitStatusOf 从 WaitStatus 取进程退出码；被信号杀死时按 128+signal 计算，
// 与 shell 的惯例一致。
func exitStatusOf(ws unix.WaitStatus) int {
	if ws.Signaled() {
		return 128 + int(ws.Signal())
	}
	return ws.ExitStatus()
}

func buildOutput(ccerr error, tt time.Duration, memKB int, cfg *models.SandboxArgs, processCnt int, outerWorkdir string, exitStatus int, outputLimitExceeded bool, oomKilled bool) *models.SandboxOutput {
	out := &models.SandboxOutput{
		ExitStatus:     exitStatus,
		CombinedOutput: truncateBytes(readCombinedOutput(outerWorkdir), 1024),
		Memory:         memKB,
		Time:           int(tt) / int(time.Millisecond),
		UserStatus:     constants.OJ_AC,
		ProcessCnt:     processCnt,
	}

	// 内存超限：两种证据任一成立即判定。
	//
	// 1) cgroup 触发了 OOM kill：程序把内存吃超了，这是硬证据；
	// 2) 采样到的匿名内存峰值超过题目上限：程序没被杀掉，但确实用超了。
	if oomKilled || out.Memory > cfg.MemoryLimit {
		out.UserStatus = constants.OJ_ML
		return out
	}

	// 输出超限：用户程序写了太多内容。
	//
	// 正常路径由 RLIMIT_FSIZE 拦下（SIGXFSZ），这里再兜一层：程序若忽略
	// SIGXFSZ 继续写，文件大小会超过上限，后面输出比较会失败，只能给出含糊
	// 的"答案错误"；判成输出超限更贴近真实原因。
	if outputLimitExceeded {
		out.UserStatus = constants.OJ_OL
		return out
	}
	if info, err := os.Stat(path.Join(outerWorkdir, "data.usr")); err == nil && info.Size() > constants.MaxOutputSize {
		out.UserStatus = constants.OJ_OL
		return out
	}

	// 退出码落在 128+signal 区间说明进程是被信号杀死的（tini 对被信号杀死的
	// 子进程统一 _exit(128)，这里退一步按 shell 惯例还原成具体信号）。
	if exitStatus > 128 && exitStatus < 128+64 {
		out.ExitSignal = unix.Signal(exitStatus - 128).String()
	}

	if ccerr != nil {
		switch ccerr {
		case shared.ErrCgroupLimitExceeded:
			out.UserStatus = constants.OJ_TL
		case shared.ErrRealTimeTimeout:
			out.UserStatus = constants.OJ_TL
			out.Time = 3*cfg.TimeLimit + 233
		case shared.ErrRuntimeError:
			out.UserStatus = constants.OJ_RE
		case shared.ErrOutputLimitExceeded:
			out.UserStatus = constants.OJ_OL
		}
	}

	return out
}

func truncateBytes(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

func readCombinedOutput(outerWorkdir string) string {
	output, err := os.ReadFile(path.Join(outerWorkdir, "data.usr"))
	if err != nil {
		slog.Warn("read data.usr failed", "err", err)
	}
	perr, err := os.ReadFile(path.Join(outerWorkdir, "data.err"))
	if err != nil {
		slog.Warn("read data.err failed", "err", err)
	}
	return string(output) + string(perr)
}
