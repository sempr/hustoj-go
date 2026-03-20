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

	controller := &SandboxController{
		cfg: cfg,
	}

	out, err := controller.execute()
	if err != nil {
		slog.Error("sandbox execution failed", "err", err)
	}
	json.NewEncoder(file3).Encode(out)
}

var config *models.SandboxArgs

// SandboxController manages the execution of sandboxed processes
type SandboxController struct {
	cfg                *models.SandboxArgs
	cgroupPath         string
	childMainPid       int
	ws                 unix.WaitStatus
	ru                 unix.Rusage
	outputBuffer       bytes.Buffer
	tracerReady        chan bool
	cgroupLimit        time.Duration
	realTimeLimit      time.Duration
	memoryLimit        int
	tracerDoneChan     chan TraceResult
	checkerFailureChan chan error
}

// SandboxInit holds initialization data for the sandbox
type SandboxInit struct {
	CgroupLimit   time.Duration
	RealTimeLimit time.Duration
	MemoryLimit   int
	TracerReady   chan bool
}


func (c *SandboxController) init() (*SandboxInit, error) {
	cgroupLimit := time.Millisecond * time.Duration(c.cfg.TimeLimit)
	realTimeLimit := cgroupLimit * 3
	memoryLimit := c.cfg.MemoryLimit << 10

	init := &SandboxInit{
		CgroupLimit:   cgroupLimit,
		RealTimeLimit: realTimeLimit,
		MemoryLimit:   memoryLimit,
		TracerReady:   make(chan bool),
	}

	return init, nil
}

func (c *SandboxController) execute() (*models.SandboxOutput, error) {
	init, err := c.init()
	if err != nil {
		return nil, err
	}

	slog.Info("ParentMain: 启动...", "cgroup_limit", init.CgroupLimit, "real_time_limit", init.RealTimeLimit)

	c.tracerDoneChan = make(chan TraceResult, 1)
	c.checkerFailureChan = make(chan error, 1)
	c.cgroupLimit = init.CgroupLimit
	c.realTimeLimit = init.RealTimeLimit
	c.memoryLimit = init.MemoryLimit
	c.tracerReady = init.TracerReady

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	// Start tracer
	runTracer := newTracerRunner(c.cfg, &c.outputBuffer, &c.childMainPid, &c.ws, &c.ru, c.tracerReady, &c.cgroupPath)

	wg.Add(1)
	go func() {
		defer wg.Done()
		res := runTracer()
		if res.Err != nil {
			slog.Warn("Tracer 协程因 ptrace 错误退出", "error", res.Err, "signal", res.Sig)
		}
		c.tracerDoneChan <- res
	}()

	slog.Info("before tracer Ready")
	select {
	case <-c.tracerReady:
		slog.Info("after tracer Ready")
	case res := <-c.tracerDoneChan:
		// Tracer 已经退出（出错），无需继续
		c.cancelAndWait(cancel, &wg)
		return c.buildOutput(res, res.Err)
	}

	// Setup resources cleanup AFTER tracer is ready
	defer c.cleanupResources()

	// Start CPU checker
	startTime := time.Now()
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := runCPUChecker(ctx, startTime, c.cgroupLimit, c.realTimeLimit, filepath.Join(c.cgroupPath, "cpu.stat"), slog.Default())
		if err != nil {
			c.checkerFailureChan <- err
		}
	}()

	slog.Info("Main: 等待 ptrace 结束或检查器失败...")
	timeoutResult, traceResult := c.waitForCompletion()

	c.cancelAndWait(cancel, &wg)

	return c.buildOutput(traceResult, timeoutResult)
}

type cancelFunc = context.CancelFunc

func (c *SandboxController) cancelAndWait(cancel cancelFunc, wg *sync.WaitGroup) {
	cancel()
	slog.Info("Main: 等待所有协程关闭...")
	wg.Wait()
}

func (c *SandboxController) cleanupResources() {
	cleanupCgroup(c.cgroupPath)
}

func (c *SandboxController) waitForCompletion() (error, TraceResult) {
	var finalResult error
	var traceResult TraceResult

	select {
	case err := <-c.checkerFailureChan:
		finalResult = err
		slog.Warn("Checker 报告失败，正在终止被 trace 的进程...", "error", finalResult)

		if err := unix.Kill(c.childMainPid, unix.SIGKILL); err != nil {
			slog.Error("发送 SIGKILL 失败", "pid", c.childMainPid, "error", err)
		}
		traceResult = <-c.tracerDoneChan
		slog.Info("Tracer 协程已确认进程终止。")

	case traceResult = <-c.tracerDoneChan:
		finalResult = traceResult.Err
		if traceResult.Err != nil {
			slog.Warn("Tracer 首先完成 (有错误)", "error", traceResult.Err)
		} else {
			slog.Info("Tracer 首先完成 (成功)。")
		}
	}

	return finalResult, traceResult
}

func (c *SandboxController) buildOutput(traceResult TraceResult, finalResult error) (*models.SandboxOutput, error) {
	slog.Info("Time Used: ", "sys", c.ru.Stime, "user", c.ru.Utime, "st", c.ws.ExitStatus())
	cdt, err1 := readCgroupCPUTime(filepath.Join(c.cgroupPath, "cpu.stat"))
	mdt, err2 := os.ReadFile(filepath.Join(c.cgroupPath, "memory.peak"))
	mem, err3 := strconv.Atoi(strings.TrimSpace(string(mdt)))
	slog.Info("Cgroup Data: ", "cpu", cdt, "memory", mdt, "err1", err1, "err2", err2, "err3", err3)

	out := &models.SandboxOutput{
		ExitStatus:     c.ws.ExitStatus(),
		CombinedOutput: truncateBytes(c.outputBuffer.String(), 1024),
		Memory:         mem / 1024,
		Time:           int(cdt) / int(time.Millisecond),
		UserStatus:     constants.OJ_AC,
		ProcessCnt:     processCnt,
	}

	slog.Debug("prepare output data")
	if c.ws.ExitStatus() != 0 && finalResult != nil {
		c.processErrorResult(out, traceResult, finalResult)
	}

	slog.Debug("write json file to file-no 3")
	slog.Debug("remove cgroup path")
	return out, nil
}

func (c *SandboxController) processErrorResult(out *models.SandboxOutput, traceResult TraceResult, finalResult error) {
	switch finalResult {
	case ErrCgroupLimitExceeded:
		out.UserStatus = constants.OJ_TL
	case ErrRealTimeTimeout:
		out.UserStatus = constants.OJ_TL
		out.Time = int(c.realTimeLimit/time.Millisecond) + 233
	case ErrRuntimeError:
		if out.Memory > c.memoryLimit/1024 {
			out.UserStatus = constants.OJ_ML
		} else {
			out.UserStatus = constants.OJ_RE
			out.ExitSignal = traceResult.Sig.String()
		}
	case ErrOutputLimitExceeded:
		out.UserStatus = constants.OJ_OL
	}
}
