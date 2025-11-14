package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/sempr/hustoj-go/pkg/constants"
	"github.com/sempr/hustoj-go/pkg/models"
	"golang.org/x/sys/unix"
)

var config *models.SandboxArgs
var logger *slog.Logger

func chRoot() {
	newRootPath := config.Rootfs
	// fmt.Println("Shim：Marking root as private (MS_PRIVATE)...")
	if err := syscall.Mount("none", "/", "none", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
		logger.Error("Shim 错误: Failed to mark root private", "err", err)
		os.Exit(1)
	}
	// 3. 确保新的根是一个挂载点 (旧的第2步)
	// fmt.Printf("Shim：Bind-mounting %s ...\n", newRootPath)
	if err := syscall.Mount(newRootPath, newRootPath, "bind", syscall.MS_BIND, ""); err != nil {
		logger.Error("Shim 错误: bind-mount", "rootPath", newRootPath, "err", err)
		os.Exit(1)
	}
	// 3. 创建一个目录来存放旧的 root
	//    这个目录必须在 newRootPath 里面
	putOldPath := filepath.Join(newRootPath, ".old_root")
	// fmt.Printf("Shim：创建旧 root 挂载点 %s ...\n", putOldPath)
	if err := os.MkdirAll(putOldPath, 0700); err != nil {
		logger.Error("Shim 错误: 创建 失败", "putOldPath", putOldPath, "err", err)
		os.Exit(1)
	}

	// 4. 执行 PivotRoot
	// fmt.Println("Shim：执行 syscall.PivotRoot...")
	if err := syscall.PivotRoot(newRootPath, putOldPath); err != nil {
		logger.Error("Shim 错误: PivotRoot 失败:  newRootPath=", "err", err, "newRootPath", newRootPath)
		os.Exit(1)
	}

	// 5. 切换到新的根目录
	// fmt.Println("Shim：Chdir 到新的 '/' ...")
	if err := syscall.Chdir("/"); err != nil {
		logger.Error("Shim 错误: Chdir('/') 失败", "err", err)
		os.Exit(1)
	}
	// 6. 卸载旧的 root
	//    这是 pivot_root 最关键的安全步骤！
	//    旧的 root 现在在 /.old_root (注意：/ 是新的根)
	// fmt.Println("Shim：卸载 /.old_root ...")
	if err := syscall.Unmount("/.old_root", syscall.MNT_DETACH); err != nil {
		logger.Error("Shim 错误: Unmount '/.old_root' 失败", "err", err)
		os.Exit(1)
	}

	// fmt.Println("Shim： 删除目录 /.old_root ...")
	// 7. (可选) 删除临时目录
	if err := os.RemoveAll("/.old_root"); err != nil {
		logger.Warn("Shim 警告: RemoveAll '/.old_root' 失败", "err", err)
	}
}

func changeFiles() {
	logger.Info("redirect files")
	if config.Stdin != "" {
		fi, err := os.OpenFile(config.Stdin, os.O_RDONLY, 0644)
		if err != nil {
			logger.Error("redir-stdin", "err", err)
		}
		unix.Dup2(int(fi.Fd()), int(os.Stdin.Fd()))
	}
	if config.Stdout != "" {
		fo, err := os.OpenFile(config.Stdout, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			logger.Error("redir-stdout", "err", err)
		}
		unix.Dup2(int(fo.Fd()), int(os.Stdout.Fd()))
	}
	if config.Stderr != "" {
		fe, err := os.OpenFile(config.Stderr, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			logger.Error("redir-stderr", "err", err)
		}
		unix.Dup2(int(fe.Fd()), int(os.Stderr.Fd()))
	}
}

func prepareMounts() {
	logger.Info("mount paths")
	unix.Mount("proc", "/proc", "proc", 0, "")
	unix.Mount("tmpfs", "/dev", "tmpfs", 0, "")
	unix.Mount("devpts", "/dev/pts", "devpts", 0, "")
	unix.Mount("sysfs", "/sys", "sysfs", 0, "")

	logger.Info("prepare /dev/null")
	os.Remove(os.DevNull)
	unix.Mknod("/dev/null", syscall.S_IFCHR|0666, int(unix.Mkdev(1, 3)))
	unix.Chmod("/dev/null", 0666)
}

func ChildMain(cfg *models.SandboxArgs) {
	config = cfg
	runtime.LockOSThread()
	file3 := os.NewFile(uintptr(3), "fd3")
	logger = slog.New(slog.NewJSONHandler(file3, nil)).With("P", "child")
	chRoot()

	logger.Info("change to workdir")
	syscall.Chdir(config.Workdir)

	prepareMounts()
	changeFiles()

	logger.Info("before setrlimit")
	// set rlimit, default 256MB
	var rlim unix.Rlimit
	rlim.Max = 256 << 20
	rlim.Cur = rlim.Max
	unix.Setrlimit(unix.RLIMIT_FSIZE, &rlim)

	logger.Info("before setuid")
	// run command
	unix.Setuid(65534)
	unix.Setgid(65534)

	logger.Info("before traceme")
	// start traceme then raise stop
	a, b, err := unix.Syscall(unix.SYS_PTRACE, uintptr(unix.PTRACE_TRACEME), 0, 0)
	logger.Info("traceme msg", "a", a, "b", b, "err", err)
	logger.Info("before stop myself")
	unix.Kill(os.Getpid(), unix.SIGSTOP)
	cmds := strings.Split(config.Command, " ")
	logger.Info("starting Exec", "cmds", cmds)
	if err := unix.Exec(cmds[0], cmds, os.Environ()); err != nil {
		logger.Error("exec error", "err", err)
	}
	logger.Info("maybe not used here")
}

// ErrCgroupLimitExceeded Cgroup CPU 时间超限
var ErrCgroupLimitExceeded = errors.New("cgroup CPU time limit exceeded")

// ErrRealTimeTimeout 物理时间执行超时
var ErrRealTimeTimeout = errors.New("real-time execution timeout")

var ErrRuntimeError = errors.New("runtime error")
var ErrOutputLimitExceeded = errors.New("output limit exceed")

func runCPUChecker(
	ctx context.Context,
	startTime time.Time,
	cgroupCPULimit time.Duration,
	realTimeLimit time.Duration,
	cgroupStatFile string, // 您配置的 cgroup cpu.stat 文件路径
	logger *slog.Logger,
) error {
	logger.Info("CPU Checker: 启动...")
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 1. 检查 Cgroup CPU 时间
			consumedCPUTime, err := readCgroupCPUTime(cgroupStatFile)
			if err != nil {
				logger.Warn("CPU Checker: 读取 cgroup 失败", "error", err)
				// 根据您的策略，这里也可以选择返回错误
				continue
			}

			if consumedCPUTime > cgroupCPULimit {
				logger.Warn("违规! Cgroup CPU 时间超出限制",
					"consumed_cpu_sec", consumedCPUTime.Seconds(), "limit_cpu_sec", cgroupCPULimit.Seconds())
				// 立即退出，并报告错误
				return ErrCgroupLimitExceeded
			}

			// 2. 检查物理时间
			elapsedRealTime := time.Since(startTime)
			if elapsedRealTime > realTimeLimit {
				logger.Warn("违规! 物理时间超出限制",
					"elapsed_real_sec", elapsedRealTime.Seconds(), "limit_real_sec", realTimeLimit.Seconds())
				// 立即退出，并报告错误
				return ErrRealTimeTimeout
			}

			// log.Printf("CPU Checker: OK (CPU: %.4fs, Real: %.4fs)",
			// 	consumedCPUTime.Seconds(), elapsedRealTime.Seconds())

		case <-ctx.Done():
			// context 被取消 (因为 tracer 协程退出了)
			logger.Info("CPU Checker: 收到停止信号，停止检查。")
			return nil // 正常停止
		}
	}
}

// readCgroupCPUTime 是 checkCPUUsage 的核心实现。
// 它读取指定的 cpu.stat 文件并解析 "usage_usec" 字段。
func readCgroupCPUTime(statFile string) (time.Duration, error) {
	file, err := os.Open(statFile)
	if err != nil {
		return 0, fmt.Errorf("无法打开 %s: %w", statFile, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "usage_usec") {
			parts := strings.Fields(line)
			if len(parts) == 2 {
				usec, err := strconv.ParseUint(parts[1], 10, 64)
				if err != nil {
					return 0, fmt.Errorf("无法解析 'usage_usec' 值: %w", err)
				}
				// 转换为 time.Duration
				return time.Duration(usec) * time.Microsecond, nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("扫描 %s 时出错: %w", statFile, err)
	}

	return 0, fmt.Errorf("在 %s 中未找到 'usage_usec' 字段", statFile)
}

func truncateBytes(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

func ParentMain(cfg *models.SandboxArgs) {
	runtime.LockOSThread()
	config = cfg
	slog.SetLogLoggerLevel(slog.LevelDebug)
	file3 := os.NewFile(uintptr(3), "fd3")
	if file3 == nil {
		slog.Info("file3 is nil")
	}
	defer file3.Close()
	// set options
	var ru unix.Rusage
	var cgroupPath string
	var b bytes.Buffer
	var childMainPid int
	var ws unix.WaitStatus
	var processCnt int = 1
	// 设置限制
	cgroupLimit := time.Millisecond * time.Duration(config.TimeLimit) // milisecond
	realTimeLimit := cgroupLimit * 3
	memoryLimit := config.MemoryLimit << 10 // kb to bytes
	slog.Debug("args", "args", config)
	tracerReady := make(chan bool)

	type TraceResult struct {
		Err  error
		Sig  unix.Signal
		Code int
	}

	var runTracer = func() TraceResult {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		slog.Info("runTracer")

		defer func() {
			slog.Info("final clear")
			pidt, err := unix.Wait4(-childMainPid, nil, unix.WALL, nil)
			slog.Info("final wait", "err", err, "pid", pidt)
		}()

		selfPath, err := os.Executable()
		if err != nil {
			panic(err)
		}

		var childArgs []string
		childArgs = append(childArgs, "child")
		childArgs = append(childArgs, os.Args[2:]...)
		cmd := exec.Command(selfPath, childArgs...)
		cmd.ExtraFiles = append(cmd.ExtraFiles, os.Stderr)

		cmd.SysProcAttr = &syscall.SysProcAttr{
			Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWNET | syscall.CLONE_NEWUTS | syscall.CLONE_NEWIPC,
			Setpgid:    true,
		}

		if config.Stderr == "" && config.Stdout == "" {
			cmd.Stdout = &b
			cmd.Stderr = &b
		}
		slog.Info("start cmd")

		err = cmd.Start()
		if err != nil {
			slog.Info("cmd start failed", "err", err)
			panic(err)
		}
		slog.Info("after start cmd")

		childMainPid = cmd.Process.Pid
		slog.Info("start wait for the stop")
		pidTmp, err := unix.Wait4(-childMainPid, &ws, 0, nil)
		slog.Info("before to stop signal", "pidTmp", pidTmp, "ws", fmt.Sprintf("%0X", ws))
		if err != nil {
			panic(err)
		}

		cgroupPath = filepath.Join("/sys/fs/cgroup", "hustoj", fmt.Sprintf("run-%d-%d", config.SolutionId, childMainPid))
		err = os.MkdirAll(cgroupPath, 0644)
		if err != nil {
			panic(err)
		}
		err = os.WriteFile(filepath.Join("/sys/fs/cgroup", "cgroup.subtree_control"), []byte("+cpu +memory +pids"), 0644)
		if err != nil {
			panic(err)
		}
		err = os.WriteFile(filepath.Join("/sys/fs/cgroup", "hustoj", "cgroup.subtree_control"), []byte("+cpu +memory +pids"), 0644)
		if err != nil {
			panic(err)
		}
		// memroy max, total, default 256M here
		if err = os.WriteFile(filepath.Join(cgroupPath, "memory.max"), fmt.Appendf(nil, "%d", memoryLimit+4096), 0644); err != nil {
			panic(err)
		}
		// cpu max, 1.2 core max
		if err = os.WriteFile(filepath.Join(cgroupPath, "cpu.max"), fmt.Appendf(nil, "120000 100000"), 0644); err != nil {
			panic(err)
		}
		// pid max
		if err = os.WriteFile(filepath.Join(cgroupPath, "pids.max"), fmt.Appendf(nil, "64"), 0644); err != nil {
			panic(err)
		}
		err = os.WriteFile(filepath.Join(cgroupPath, "cgroup.procs"), fmt.Append(nil, childMainPid), 0644)
		if err != nil {
			panic(err)
		}

		unix.PtraceSetOptions(childMainPid,
			unix.PTRACE_O_EXITKILL|
				unix.PTRACE_O_TRACECLONE|
				unix.PTRACE_O_TRACEFORK|
				unix.PTRACE_O_TRACEVFORK|
				unix.PTRACE_O_TRACEVFORKDONE|
				unix.PTRACE_O_TRACEEXIT|
				unix.PTRACE_O_TRACESYSGOOD|
				unix.PTRACE_O_TRACESECCOMP|
				unix.PTRACE_O_TRACEEXEC,
		)
		// continue
		defer func() {
			// detach tracee
			err := unix.PtraceDetach(childMainPid)
			slog.Info("ptrace detach", "err", err)
		}()
		slog.Info("before contnue child process", "sig", ws.StopSignal(), "pidMain", childMainPid)
		tracerReady <- true
		err = unix.PtraceCont(pidTmp, int(ws.StopSignal()))
		if err != nil {
			slog.Info("ptrace cont error", "err", err)
		}
		for {
			slog.Debug("new wait here....")
			pidTmp, err := unix.Wait4(-childMainPid, &ws, 0, &ru)
			if err != nil {
				return TraceResult{err, 0, 0}
			}
			slog.Debug("tracing", "pid", pidTmp, "ws", fmt.Appendf(nil, "%X", ws), "ru", ru)
			if ws.Exited() {
				slog.Debug("process exit ", "pid", pidTmp, "exitCode", ws.ExitStatus())
				if pidTmp == childMainPid {
					return TraceResult{nil, 0, 0}
				}
				continue
			}
			if ws == 0x85 {
				data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pidTmp))
				slog.Info("status info ", "data", data, "err", err)
			}
			if ws.Signaled() {
				slog.Debug("process signaled", "pid", pidTmp, "signal", ws.Signal(), "status", ws.ExitStatus(), "exit", ws.Exited())
				if ws.Signal()&0x7f == unix.SIGXFSZ {
					return TraceResult{ErrOutputLimitExceeded, 0, 0}
				}
				return TraceResult{ErrRuntimeError, ws.Signal(), 0}
			}
			if ws.Stopped() {
				slog.Debug("process stopped", "pid", pidTmp, "signal", ws.StopSignal(), "signal", ws.StopSignal()&0x7f)
				stopsig := ws.StopSignal()
				if stopsig == unix.SIGSEGV {

					type PtracePeekSigInfoArgs struct {
						Off   uint64
						Flags uint64
						Nr    uint64
					}
					args := PtracePeekSigInfoArgs{
						Off:   0,
						Flags: 0,
						Nr:    1, // 最多读取1个信号
					}
					var siginfo unix.Siginfo

					ret, _, errno := unix.Syscall6(
						unix.SYS_PTRACE,
						uintptr(unix.PTRACE_PEEKSIGINFO),
						uintptr(pidTmp),
						uintptr(unsafe.Pointer(&args)),
						uintptr(unsafe.Pointer(&siginfo)),
						0, 0,
					)
					slog.Info("siginfo peeked", "sig", ret, "errno", errno)
					// print the info here
				}

				if stopsig == (unix.SIGTRAP | 0x80) {
					slog.Info("got stopsig=85, just do Ptrace")
					unix.PtraceCont(pidTmp, 0)
					continue
				}

				if stopsig == unix.SIGTRAP {
					eventNumber := int(ws >> 16)
					if eventNumber != 0 {
						var Z []int = []int{unix.PTRACE_EVENT_CLONE, unix.PTRACE_EVENT_FORK, unix.PTRACE_EVENT_VFORK}
						if slices.Contains(Z, eventNumber) {
							slog.Info("trap event, clone/fork/vfork", "stopsig", stopsig, "eventnumber", eventNumber)
							processCnt++
						} else if eventNumber == unix.PTRACE_EVENT_EXEC {
							slog.Info("trace event: exec", "eventNumber", eventNumber)
							data, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", childMainPid))
							args := strings.Split(string(data), "\x00")
							slog.Info("exec argv:", "args", args)
						} else if eventNumber == unix.PTRACE_EVENT_VFORK_DONE {
							slog.Info("trace event: vfork-done", "eventNumber", eventNumber)
						} else if eventNumber == unix.PTRACE_EVENT_EXIT {
							slog.Info("trace event: exit", "eventNumber", eventNumber)
						} else {
							slog.Info("trace event: todo", "eventNumber", eventNumber)
						}
						msg, err := unix.PtraceGetEventMsg(pidTmp)
						slog.Info("get event msg", "msg", msg, "err", err, "pid", pidTmp)
					} else {
						slog.Info("eventNumber is 0")
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
	}

	// 创建用于通信的上下文和通道
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	// tracerDoneChan 用于接收 tracer 协程的退出信号
	tracerDoneChan := make(chan TraceResult, 1)
	// checkerFailureChan 仅用于接收 checker 协程的 *失败* 信号
	checkerFailureChan := make(chan error, 1)

	startTime := time.Now()
	slog.Info("ParentMain: 启动...", "cgroup_limit", cgroupLimit, "real_time_limit", realTimeLimit)

	// --- 2. 启动 Goroutines ---

	// 启动 Tracer Goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		res := runTracer()
		if res.Err != nil {
			slog.Warn("Tracer 协程因 ptrace 错误退出", "error", res.Err, "signal", res.Sig)
		}
		tracerDoneChan <- res
	}()

	slog.Info("before tracer Ready")
	<-tracerReady
	slog.Info("after  tracer Ready")
	defer func() {

		if strings.HasPrefix(cgroupPath, "/sys/fs/cgroup/hustoj") {
			// 如果还有活的进程 先迁到cgroup的最祖先 然后删除当前的cgroup
			procs := filepath.Join(cgroupPath, "cgroup.procs")
			pprocs := "/sys/fs/cgroup/cgroup.procs"
			if data, err := os.ReadFile(procs); err == nil {
				for _, pidstr := range strings.Fields(string(data)) {
					err := os.WriteFile(pprocs, []byte(pidstr), 0644)
					slog.Info("remove pid", "pid", pidstr, "err", err, "pprocs", pprocs)
				}
			}
			err := os.RemoveAll(cgroupPath)
			if err != nil {
				// slog.Info("sleep here.....")
				// time.Sleep(time.Second * 30)
				slog.Info("cgrouppath remove failed", "path", cgroupPath, "error", err)
			}
		}
	}()
	// 启动 CPU Checker Goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := runCPUChecker(ctx, startTime, cgroupLimit, realTimeLimit, filepath.Join(cgroupPath, "cpu.stat"), slog.Default())
		// 只有在 *真正* 发生违规时才发送信号
		if err != nil {
			checkerFailureChan <- err
		}
	}()

	var finalResult error
	slog.Info("Main: 等待 ptrace 结束或检查器失败...")
	var finalTraceResult TraceResult
	select {
	case err := <-checkerFailureChan:
		// **情况 1: Checker 报告失败 (Cgroup 或超时)**
		finalResult = err
		slog.Warn("Checker 报告失败，正在终止被 trace 的进程...", "error", finalResult)

		// *重要*: Main 负责发送 kill 信号
		if err := unix.Kill(childMainPid, unix.SIGKILL); err != nil {
			slog.Error("发送 SIGKILL 失败", "pid", childMainPid, "error", err)
		}
		// 等待 tracer 协程确认进程被 kill (它会从 tracerDoneChan 收到信号)
		<-tracerDoneChan
		slog.Info("Tracer 协程已确认进程终止。")

	case finalTraceResult = <-tracerDoneChan:
		// **情况 2: Tracer 协程首先结束** (进程正常退出或崩溃)
		finalResult = finalTraceResult.Err
		if finalTraceResult.Err != nil {
			slog.Warn("Tracer 首先完成 (有错误)", "error", finalTraceResult.Err)
		} else {
			slog.Info("Tracer 首先完成 (成功)。")
		}
		// 此时，tracer 已经结束，我们不需要再 kill 它了
	}

	// --- 4. 清理 ---

	// 无论哪种情况，我们都必须通知 CPU Checker 协程停止
	// (如果它尚未停止的话)
	cancel()

	// 等待所有协程 (特别是 checker) 干净地退出
	slog.Info("Main: 等待所有协程关闭...")
	wg.Wait()

	slog.Info("Time Used: ", "sys", ru.Stime, "user", ru.Utime, "st", ws.ExitStatus())
	cdt, err1 := readCgroupCPUTime(filepath.Join(cgroupPath, "cpu.stat"))
	mdt, err2 := os.ReadFile(filepath.Join(cgroupPath, "memory.peak"))
	mem, err3 := strconv.Atoi(strings.TrimSpace(string(mdt)))
	slog.Info("Cgroup Data: ", "cpu", cdt, "memory", mdt, "err1", err1, "err2", err2, "err3", err3)

	var out models.SandboxOutput
	out.ExitStatus = ws.ExitStatus()
	out.CombinedOutput = truncateBytes(b.String(), 1024)
	out.Memory = mem / 1024
	out.Time = int(cdt) / int(time.Millisecond)
	out.UserStatus = constants.OJ_AC
	out.ProcessCnt = processCnt
	slog.Debug("prepare output data")
	if ws.ExitStatus() != 0 {
		// check error
		if finalResult != nil {
			switch finalResult {
			case ErrCgroupLimitExceeded:
				out.UserStatus = constants.OJ_TL
			case ErrRealTimeTimeout:
				out.UserStatus = constants.OJ_TL
				out.Time = int(realTimeLimit/time.Millisecond) + 233
			case ErrRuntimeError:
				if out.Memory > memoryLimit/1024 {
					out.UserStatus = constants.OJ_ML
				} else {
					out.UserStatus = constants.OJ_RE
					out.ExitSignal = finalTraceResult.Sig.String()
				}
			case ErrOutputLimitExceeded:
				out.UserStatus = constants.OJ_OL
			}
		}
	}

	slog.Debug("write json file to file-no 3")
	json.NewEncoder(file3).Encode(out)
	slog.Debug("remove cgroup path")
}
