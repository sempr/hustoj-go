package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
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

	"golang.org/x/sys/unix"
)

// 判题结果
const (
	OJ_WT0 = 0  // 提交排队
	OJ_WT1 = 1  // 重判排队
	OJ_CI  = 2  // 编译中
	OJ_RI  = 3  // 运行中
	OJ_AC  = 4  // 答案正确
	OJ_PE  = 5  // 格式错误
	OJ_WA  = 6  // 答案错误
	OJ_TL  = 7  // 时间超限
	OJ_ML  = 8  // 内存超限
	OJ_OL  = 9  // 输出超限
	OJ_RE  = 10 // 运行错误
	OJ_CE  = 11 // 编译错误
	OJ_CO  = 12 // 编译完成
	OJ_TR  = 13 // 测试运行结束
	OJ_MC  = 14 // 等待裁判手工确认
)

type Output struct {
	ExitStatus     int    `json:"status"`
	CombinedOutput string `json:"output"`
	Time           int    `json:"time"`
	Memory         int    `json:"memory"`
	UserStatus     int    `json:"user_status"`
	ProcessCnt     int    `json:"process_count"`
}

type Config struct {
	Command     string
	Rootfs      string
	Workdir     string
	Stdin       string
	Stdout      string
	Stderr      string
	TimeLimit   int
	MemoryLimit int
}

var config Config

func initConfig() {
	// fmt.Printf("child: %v\n", os.Args)
	flag.StringVar(&config.Rootfs, "rootfs", "/tmp", "")
	flag.StringVar(&config.Command, "cmd", "/bin/false", "")
	flag.StringVar(&config.Workdir, "cwd", "/code", "")
	flag.StringVar(&config.Stdin, "stdin", "", "")
	flag.StringVar(&config.Stdout, "stdout", "", "")
	flag.StringVar(&config.Stderr, "stderr", "", "")
	flag.IntVar(&config.TimeLimit, "time", 1000, "")
	flag.IntVar(&config.MemoryLimit, "memory", 256<<10, "")
	if os.Args[1] == "child" {
		flag.CommandLine.Parse(os.Args[2:])
	} else {
		flag.Parse()
	}
}

func chRoot() {
	newRootPath := config.Rootfs
	// fmt.Println("Shim：Marking root as private (MS_PRIVATE)...")
	if err := syscall.Mount("none", "/", "none", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
		fmt.Fprintf(os.Stderr, "Shim 错误: Failed to mark root private: %v\n", err)
		os.Exit(1)
	}
	// 3. 确保新的根是一个挂载点 (旧的第2步)
	// fmt.Printf("Shim：Bind-mounting %s ...\n", newRootPath)
	if err := syscall.Mount(newRootPath, newRootPath, "bind", syscall.MS_BIND, ""); err != nil {
		fmt.Fprintf(os.Stderr, "Shim 错误: bind-mount %s 失败: %v\n", newRootPath, err)
		os.Exit(1)
	}
	// 3. 创建一个目录来存放旧的 root
	//    这个目录必须在 newRootPath 里面
	putOldPath := filepath.Join(newRootPath, ".old_root")
	// fmt.Printf("Shim：创建旧 root 挂载点 %s ...\n", putOldPath)
	if err := os.MkdirAll(putOldPath, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "Shim 错误: 创建 %s 失败: %v\n", putOldPath, err)
		os.Exit(1)
	}

	// 4. 执行 PivotRoot
	// fmt.Println("Shim：执行 syscall.PivotRoot...")
	if err := syscall.PivotRoot(newRootPath, putOldPath); err != nil {
		fmt.Fprintf(os.Stderr, "Shim 错误: PivotRoot 失败: %v newRootPath=%s\n", err, newRootPath)
		os.Exit(1)
	}

	// 5. 切换到新的根目录
	// fmt.Println("Shim：Chdir 到新的 '/' ...")
	if err := syscall.Chdir("/"); err != nil {
		fmt.Fprintf(os.Stderr, "Shim 错误: Chdir('/') 失败: %v\n", err)
		os.Exit(1)
	}
	// 6. 卸载旧的 root
	//    这是 pivot_root 最关键的安全步骤！
	//    旧的 root 现在在 /.old_root (注意：/ 是新的根)
	// fmt.Println("Shim：卸载 /.old_root ...")
	if err := syscall.Unmount("/.old_root", syscall.MNT_DETACH); err != nil {
		fmt.Fprintf(os.Stderr, "Shim 错误: Unmount '/.old_root' 失败: %v\n", err)
		os.Exit(1)
	}

	// fmt.Println("Shim： 删除目录 /.old_root ...")
	// 7. (可选) 删除临时目录
	if err := os.RemoveAll("/.old_root"); err != nil {
		fmt.Fprintf(os.Stderr, "Shim 警告: RemoveAll '/.old_root' 失败: %v\n", err)
	}
}

func changeFiles() {
	// slog.Info("redirect files")
	if config.Stdin != "" {
		fi, _ := os.OpenFile(config.Stdin, os.O_RDONLY, 0644)
		unix.Dup2(int(fi.Fd()), int(os.Stdin.Fd()))
	}
	if config.Stdout != "" {
		fo, _ := os.OpenFile(config.Stdout, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		unix.Dup2(int(fo.Fd()), int(os.Stdout.Fd()))
	}
	if config.Stderr != "" {
		fe, _ := os.OpenFile(config.Stderr, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		unix.Dup2(int(fe.Fd()), int(os.Stderr.Fd()))
	}
}

func prepareMounts() {
	slog.Debug("mount paths")
	unix.Mount("proc", "/proc", "proc", 0, "")
	unix.Mount("tmpfs", "/dev", "tmpfs", 0, "")
	unix.Mount("devpts", "/dev/pts", "devpts", 0, "")
	unix.Mount("sysfs", "/sys", "sysfs", 0, "")

	slog.Debug("prepare /dev/null")
	os.Remove(os.DevNull)
	unix.Mknod("/dev/null", syscall.S_IFCHR|0666, int(unix.Mkdev(1, 3)))
	unix.Chmod("/dev/null", 0666)
}

func runChild() {
	initConfig()
	chRoot()

	slog.Debug("change to workdir")
	syscall.Chdir(config.Workdir)

	prepareMounts()
	changeFiles()

	// set rlimit, default 256MB
	var rlim unix.Rlimit
	rlim.Max = 256 << 20
	rlim.Cur = rlim.Max
	unix.Setrlimit(unix.RLIMIT_FSIZE, &rlim)

	// run command
	unix.Setuid(65534)
	unix.Setgid(65534)

	// start traceme then raise stop
	unix.Syscall(unix.SYS_PTRACE, uintptr(unix.PTRACE_TRACEME), 0, 0)
	unix.Kill(os.Getpid(), unix.SIGSTOP)

	cmds := strings.Split(config.Command, " ")
	if err := syscall.Exec(cmds[0], cmds, os.Environ()); err != nil {
		panic(err)
	}
	slog.Debug("compile ok")
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
) error {
	log.Println("CPU Checker: 启动...")
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 1. 检查 Cgroup CPU 时间
			consumedCPUTime, err := readCgroupCPUTime(cgroupStatFile)
			if err != nil {
				log.Printf("CPU Checker: 读取 cgroup 失败: %v", err)
				// 根据您的策略，这里也可以选择返回错误
				continue
			}

			if consumedCPUTime > cgroupCPULimit {
				log.Printf("CPU Checker: 违规! Cgroup CPU 时间 (%.2fs) 超出限制 (%.2fs)",
					consumedCPUTime.Seconds(), cgroupCPULimit.Seconds())
				// 立即退出，并报告错误
				return ErrCgroupLimitExceeded
			}

			// 2. 检查物理时间
			elapsedRealTime := time.Since(startTime)
			if elapsedRealTime > realTimeLimit {
				log.Printf("CPU Checker: 违规! 物理时间 (%.2fs) 超出限制 (%.2fs)",
					elapsedRealTime.Seconds(), realTimeLimit.Seconds())
				// 立即退出，并报告错误
				return ErrRealTimeTimeout
			}

			// log.Printf("CPU Checker: OK (CPU: %.4fs, Real: %.4fs)",
			// 	consumedCPUTime.Seconds(), elapsedRealTime.Seconds())

		case <-ctx.Done():
			// context 被取消 (因为 tracer 协程退出了)
			log.Println("CPU Checker: 收到停止信号，停止检查。")
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

func runParent() {
	initConfig()
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

	var runTracer = func() error {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		selfPath, err := os.Executable()
		if err != nil {
			panic(err)
		}

		var childArgs []string
		childArgs = append(childArgs, "child")
		childArgs = append(childArgs, os.Args[1:]...)
		cmd := exec.Command(selfPath, childArgs...)

		cmd.SysProcAttr = &syscall.SysProcAttr{
			Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWNET | syscall.CLONE_NEWPID | syscall.CLONE_NEWUTS | syscall.CLONE_NEWIPC,
			Setpgid:    true,
		}

		if config.Stderr == "" && config.Stdout == "" {
			cmd.Stdout = &b
			cmd.Stderr = &b
		}

		cmd.Start()

		childMainPid = cmd.Process.Pid
		pidTmp, err := unix.Wait4(-childMainPid, &ws, 0, nil)
		if err != nil {
			panic(err)
		}

		cgroupPath = filepath.Join("/sys/fs/cgroup", "hustoj", fmt.Sprintf("run-%d", childMainPid))
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
				unix.PTRACE_O_TRACEEXEC|
				unix.PTRACE_O_TRACESYSGOOD|
				unix.PTRACE_O_TRACESECCOMP,
		)
		// continue
		slog.Info("before contnue child process", "sig", ws.StopSignal(), "pidMain", childMainPid, "pidTmp", pidTmp)
		tracerReady <- true
		unix.PtraceCont(childMainPid, int(ws.StopSignal()))
		for {
			slog.Debug("new wait here....")
			pidTmp, err := unix.Wait4(-childMainPid, &ws, 0, &ru)
			if err != nil {
				return err
			}
			slog.Debug("tracing", "pid", pidTmp, "ws", fmt.Appendf(nil, "%X", ws), "ru", ru)
			if ws.Exited() {
				slog.Debug("process exit ", "pid", pidTmp, "exitCode", ws.ExitStatus())
				if pidTmp == childMainPid {
					return nil
				}
				continue
			}
			if ws.Signaled() {
				slog.Debug("process signaled", "pid", pidTmp, "signal", ws.Signal())
				if ws.Signal()&0x7f == unix.SIGXFSZ {
					return ErrOutputLimitExceeded
				}
				return ErrRuntimeError
			}
			if ws.Stopped() {
				slog.Debug("process stopped", "stopsig", ws.StopSignal(), "pidTmp", pidTmp)
				stopsig := ws.StopSignal() & 0x7f
				if stopsig == unix.SIGXFSZ {
					unix.Kill(childMainPid, unix.SIGKILL)
					return ErrOutputLimitExceeded
				}
				if stopsig == unix.SIGSEGV {
					unix.Kill(childMainPid, unix.SIGKILL)
					return ErrRuntimeError
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
				err := unix.PtraceCont(pidTmp, int(ws.StopSignal()))
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
	tracerDoneChan := make(chan error, 1)
	// checkerFailureChan 仅用于接收 checker 协程的 *失败* 信号
	checkerFailureChan := make(chan error, 1)

	startTime := time.Now()
	log.Printf("Main: 启动... Cgroup 限制: %s, 物理时间限制: %s", cgroupLimit, realTimeLimit)

	// --- 2. 启动 Goroutines ---

	// 启动 Tracer Goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := runTracer()
		if err != nil {
			log.Printf("Main: Tracer 协程因 ptrace 错误退出: %v", err)
		}
		tracerDoneChan <- err
	}()

	<-tracerReady
	defer os.RemoveAll(cgroupPath)
	// 启动 CPU Checker Goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := runCPUChecker(ctx, startTime, cgroupLimit, realTimeLimit, filepath.Join(cgroupPath, "cpu.stat"))
		// 只有在 *真正* 发生违规时才发送信号
		if err != nil {
			checkerFailureChan <- err
		}
	}()

	var finalResult error
	log.Println("Main: 等待 ptrace 结束或检查器失败...")

	select {
	case err := <-checkerFailureChan:
		// **情况 1: Checker 报告失败 (Cgroup 或超时)**
		finalResult = err
		log.Printf("Main: Checker 报告失败: %v。正在终止被 trace 的进程...", finalResult)

		// *重要*: Main 负责发送 kill 信号
		if err := unix.Kill(childMainPid, unix.SIGKILL); err != nil {
			log.Printf("Main: 发送 SIGKILL 到 PID %d 失败: %v", childMainPid, err)
		}
		// 等待 tracer 协程确认进程被 kill (它会从 tracerDoneChan 收到信号)
		<-tracerDoneChan
		log.Println("Main: Tracer 协程已确认进程终止。")

	case err := <-tracerDoneChan:
		// **情况 2: Tracer 协程首先结束** (进程正常退出或崩溃)
		finalResult = err
		if err != nil {
			log.Printf("Main: Tracer 首先完成 (有错误): %v", err)
		} else {
			log.Println("Main: Tracer 首先完成 (成功)。")
		}
		// 此时，tracer 已经结束，我们不需要再 kill 它了
	}

	// --- 4. 清理 ---

	// 无论哪种情况，我们都必须通知 CPU Checker 协程停止
	// (如果它尚未停止的话)
	cancel()

	// 等待所有协程 (特别是 checker) 干净地退出
	log.Println("Main: 等待所有协程关闭...")
	wg.Wait()

	slog.Info("Time Used: ", "sys", ru.Stime, "user", ru.Utime, "st", ws.ExitStatus())
	cdt, err1 := readCgroupCPUTime(filepath.Join(cgroupPath, "cpu.stat"))
	mdt, err2 := os.ReadFile(filepath.Join(cgroupPath, "memory.peak"))
	mem, err3 := strconv.Atoi(strings.TrimSpace(string(mdt)))
	slog.Info("Cgroup Data: ", "cpu", cdt, "memory", mdt, "err1", err1, "err2", err2, "err3", err3)

	var out Output
	out.ExitStatus = ws.ExitStatus()
	out.CombinedOutput = truncateBytes(b.String(), 1024)
	out.Memory = mem / 1024
	out.Time = int(cdt) / int(time.Millisecond)
	out.UserStatus = OJ_AC
	out.ProcessCnt = processCnt
	slog.Debug("prepare output data")
	if ws.ExitStatus() != 0 {
		// check error
		if finalResult != nil {
			switch finalResult {
			case ErrCgroupLimitExceeded:
				out.UserStatus = OJ_TL
			case ErrRealTimeTimeout:
				out.UserStatus = OJ_TL
				out.Time = -1
			case ErrRuntimeError:
				if out.Memory > memoryLimit/1024 {
					out.UserStatus = OJ_ML
				} else {
					out.UserStatus = OJ_RE
				}
			case ErrOutputLimitExceeded:
				out.UserStatus = OJ_OL
			}
		}
	}

	slog.Debug("write json file to file-no 3")
	json.NewEncoder(file3).Encode(out)
	slog.Debug("remove cgroup path")
}

func main() {
	if os.Args[1] == "child" {
		runChild()
	} else {
		runtime.GOMAXPROCS(1)
		runParent()
	}
}
