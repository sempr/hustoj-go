package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sempr/hustoj-go/pkg/constants"
	"github.com/sempr/hustoj-go/pkg/models"
	"golang.org/x/sys/unix"
)

func truncateBytes(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}

func ParentMain(cfg *models.SandboxArgs) {
	runtime.LockOSThread()
	slog.SetLogLoggerLevel(slog.LevelDebug)
	file3 := os.NewFile(uintptr(3), "fd3")
	if file3 == nil {
		slog.Info("file3 is nil")
	}
	defer file3.Close()

	out, err := runParent(cfg, file3)
	if err != nil {
		slog.Error("runParent failed", "err", err)
	}
	json.NewEncoder(file3).Encode(out)
}

var config *models.SandboxArgs

func runParent(cfg *models.SandboxArgs, file3 *os.File) (*models.SandboxOutput, error) {
	var ru unix.Rusage
	var cgroupPath string
	var b bytes.Buffer
	var childMainPid int
	var ws unix.WaitStatus

	config = cfg
	slog.Debug("args", "args", config)
	tracerReady := make(chan bool)

	runTracer := newTracerRunner(cfg, &b, &childMainPid, &ws, &ru, tracerReady, &cgroupPath)

	cgroupLimit := time.Millisecond * time.Duration(config.TimeLimit)
	realTimeLimit := cgroupLimit * 3
	memoryLimit := config.MemoryLimit << 10

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup

	tracerDoneChan := make(chan TraceResult, 1)
	checkerFailureChan := make(chan error, 1)

	startTime := time.Now()
	slog.Info("ParentMain: 启动...", "cgroup_limit", cgroupLimit, "real_time_limit", realTimeLimit)

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
		cleanupCgroup(cgroupPath)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		err := runCPUChecker(ctx, startTime, cgroupLimit, realTimeLimit, filepath.Join(cgroupPath, "cpu.stat"), slog.Default())
		if err != nil {
			checkerFailureChan <- err
		}
	}()

	var finalResult error
	slog.Info("Main: 等待 ptrace 结束或检查器失败...")
	var finalTraceResult TraceResult
	select {
	case err := <-checkerFailureChan:
		finalResult = err
		slog.Warn("Checker 报告失败，正在终止被 trace 的进程...", "error", finalResult)

		if err := unix.Kill(childMainPid, unix.SIGKILL); err != nil {
			slog.Error("发送 SIGKILL 失败", "pid", childMainPid, "error", err)
		}
		<-tracerDoneChan
		slog.Info("Tracer 协程已确认进程终止。")

	case finalTraceResult = <-tracerDoneChan:
		finalResult = finalTraceResult.Err
		if finalTraceResult.Err != nil {
			slog.Warn("Tracer 首先完成 (有错误)", "error", finalTraceResult.Err)
		} else {
			slog.Info("Tracer 首先完成 (成功)。")
		}
	}

	cancel()
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
	slog.Debug("remove cgroup path")
	return &out, nil
}
