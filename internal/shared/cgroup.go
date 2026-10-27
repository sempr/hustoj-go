package shared

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// ---------------------------------------------------------------------------
// 错误定义
// ---------------------------------------------------------------------------

var (
	ErrCgroupLimitExceeded = fmt.Errorf("cgroup CPU time limit exceeded")
	ErrRealTimeTimeout     = fmt.Errorf("real-time execution timeout")
	ErrRuntimeError        = fmt.Errorf("runtime error")
	ErrOutputLimitExceeded = fmt.Errorf("output limit exceed")
)

// ---------------------------------------------------------------------------
// Cgroup 结构体
// ---------------------------------------------------------------------------

type Cgroup struct {
	solutionID  int
	childPid    int
	memoryLimit int

	path   string // /sys/fs/cgroup/hustoj/run-<id>-<pid>
	logger *slog.Logger

	// peakAnon 采样到的匿名内存峰值（字节）。
	//
	// 匿名内存是沙箱里所有进程自己的堆/栈之和：多进程程序（bash 拉一堆子
	// 进程）算的是总和，跟 cgroup 限制（memory.max，限制的也是总量）口径
	// 一致；又因为不含沙箱 tmpfs 上的输出（那是 shmem），输出多少都不会
	// 把"程序用了多少内存"带偏。
	peakAnon atomic.Int64
}

// NewCgroup 创建并初始化一个新的 Cgroup。返回实例或错误。
// 该方法会:
//
//  1. 创建 cgroup 目录。
//  2. 写入 subtree_control、memory.max、cpu.max、pids.max、cgroup.procs
//  3. 返回可操作的 *Cgroup 结构体
func NewCgroup(solutionID, childPid, memoryLimit int, logger *slog.Logger) (*Cgroup, error) {
	c := &Cgroup{
		solutionID:  solutionID,
		childPid:    childPid,
		memoryLimit: memoryLimit,
		logger:      logger,
	}
	if err := c.setup(); err != nil {
		return nil, err
	}
	return c, nil
}

// setup 负责在文件系统里真正创建 cgroup 并写入必要文件。
func (c *Cgroup) setup() error {
	// 1. 目录
	c.path = filepath.Join("/sys/fs/cgroup", "hustoj", fmt.Sprintf("run-%d-%d", c.solutionID, c.childPid))
	if err := os.MkdirAll(c.path, 0o755); err != nil {
		return fmt.Errorf("创建 cgroup 目录失败: %w", err)
	}
	// 2. subtree_control（一次性写）
	if err := os.WriteFile(filepath.Join("/sys/fs/cgroup", "cgroup.subtree_control"),
		[]byte("+cpu +memory +pids"), 0o644); err != nil {
		return fmt.Errorf("设置全局 subtree_control 失败: %w", err)
	}
	if err := os.WriteFile(filepath.Join("/sys/fs/cgroup", "hustoj", "cgroup.subtree_control"),
		[]byte("+cpu +memory +pids"), 0o644); err != nil {
		return fmt.Errorf("设置 hustoj subtree_control 失败: %w", err)
	}

	// 3. memory.max
	memFile := filepath.Join(c.path, "memory.max")
	if err := os.WriteFile(memFile, fmt.Appendf(nil, "%d", c.memoryLimit+4096), 0o644); err != nil {
		return fmt.Errorf("写 memory.max 失败: %w", err)
	}

	// 4. cpu.max (150ms per 100ms?? – 这里保持原值)
	cpuFile := filepath.Join(c.path, "cpu.max")
	if err := os.WriteFile(cpuFile, fmt.Appendf(nil, "%d %d", 15000, 10000), 0o644); err != nil {
		return fmt.Errorf("写 cpu.max 失败: %w", err)
	}

	// 5. pids.max
	pidsFile := filepath.Join(c.path, "pids.max")
	if err := os.WriteFile(pidsFile, fmt.Appendf(nil, "64"), 0o644); err != nil {
		return fmt.Errorf("写 pids.max 失败: %w", err)
	}

	// 6. cgroup.procs 将儿子进程写进去
	procsFile := filepath.Join(c.path, "cgroup.procs")
	if err := os.WriteFile(procsFile, fmt.Append(nil, c.childPid), 0o644); err != nil {
		return fmt.Errorf("写 cgroup.procs 失败: %w", err)
	}

	return nil
}

func (c *Cgroup) ReadCPUtime() (time.Duration, error) {
	return readCgroupCPUTime(filepath.Join(c.path, "cpu.stat"))
}

func (c *Cgroup) ReadMemoryPeak() (int, error) {
	bt, err := os.ReadFile(filepath.Join(c.path, "memory.peak"))
	if err != nil {
		return 0, err
	}
	v, err := strconv.Atoi(strings.Trim(string(bt), "\n\t "))
	return v, err
}

// ReadMemoryAnon 读取 cgroup 当前的匿名内存用量（字节）。
func (c *Cgroup) ReadMemoryAnon() (int64, error) {
	return readMemoryStatValue(filepath.Join(c.path, "memory.stat"), "anon")
}

// ReadMemoryShmem 读取 cgroup 当前的 tmpfs（shmem）用量（字节）。
//
// 沙箱的输出就写在这里，用它把"输出占的内存"从总量里减掉。
func (c *Cgroup) ReadMemoryShmem() (int64, error) {
	return readMemoryStatValue(filepath.Join(c.path, "memory.stat"), "shmem")
}

// PeakMemoryAnon 返回采样到的匿名内存峰值（字节）。
func (c *Cgroup) PeakMemoryAnon() int64 {
	return c.peakAnon.Load()
}

// sampleMemoryAnon 采样一次匿名内存并更新峰值。
func (c *Cgroup) sampleMemoryAnon() {
	anon, err := c.ReadMemoryAnon()
	if err != nil {
		return
	}
	for {
		old := c.peakAnon.Load()
		if anon <= old || c.peakAnon.CompareAndSwap(old, anon) {
			return
		}
	}
}

// OOMKilled 返回 cgroup 发生过 OOM kill 的次数。
//
// 这是"程序把内存吃超了"的硬证据：cgroup 的硬限制是题目内存上限加上
// 输出余量，能触发 OOM kill 说明程序自己就超了上限。
func (c *Cgroup) OOMKilled() (int64, error) {
	return readMemoryStatValue(filepath.Join(c.path, "memory.events"), "oom_kill")
}

// readMemoryStatValue 从 memory.stat 里取某个键的数值。
func readMemoryStatValue(path, key string) (int64, error) {
	bt, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(bt), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == key {
			return strconv.ParseInt(fields[1], 10, 64)
		}
	}
	return 0, fmt.Errorf("memory.stat 里没有 %s 这一项", key)
}

func (c *Cgroup) ReadMemory() {
	statFile := filepath.Join(c.path, "memory.peak")
	bt, err := os.ReadFile(statFile)
	if err == nil {
		v, _ := strconv.Atoi(strings.Trim(string(bt), "\n\t "))
		slog.Debug("memory", "bt", v)
	} else {
		slog.Error("memory error", "error", err)
	}
}

// ---------------------------------------------------------------------------
// CPU 检查器
// ---------------------------------------------------------------------------

// runCPUChecker 负责周期性检查 CPU 时间/真实时间是否超限。
// 若超限，返回对应 Errxxx，否则在 ctx 失效时优雅退出。
func (c *Cgroup) RunCPUChecker(ctx context.Context, cpuLimit, realLimit time.Duration, logger *slog.Logger, stopFunc func() error) error {
	logger.Info("CPU Checker: 启动...", "cpuLimit", cpuLimit, "realLimit", realLimit)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	// 内存采样比 CPU 检查密一些：程序分配很快时，50ms 的间隔容易漏掉峰值。
	memTicker := time.NewTicker(10 * time.Millisecond)
	defer memTicker.Stop()
	startTime := time.Now()
	// 需要监控的文件: cpu.stat（在 c.path 下）
	cpuStat := filepath.Join(c.path, "cpu.stat")

	for {
		select {
		case <-memTicker.C:
			c.sampleMemoryAnon()
		case <-ticker.C:
			consumed, err := readCgroupCPUTime(cpuStat)
			if err != nil {
				logger.Warn("CPU Checker: 读取 cgroup 失败", "error", err)
				continue
			}
			if consumed > cpuLimit {
				logger.Warn("违规! Cgroup CPU 时间超出限制",
					"consumed_cpu_sec", consumed.Seconds(), "limit_cpu_sec", cpuLimit.Seconds())
				if stopFunc != nil {
					stopFunc()
				}
				return ErrCgroupLimitExceeded
			}

			if elapsed := time.Since(startTime); elapsed > realLimit {
				logger.Warn("违规! 物理时间超出限制",
					"elapsed_real_sec", elapsed.Seconds(), "limit_real_sec", realLimit.Seconds())
				if stopFunc != nil {
					stopFunc()
				}
				return ErrRealTimeTimeout
			}

		case <-ctx.Done():
			logger.Info("CPU Checker: 收到停止信号，停止检查。")
			return nil
		}
	}
}

// ---------------------------------------------------------------------------
// Cgroup 清理
// ---------------------------------------------------------------------------

// Cleanup 删除 Cgroup 目录并尝试把其中的进程移到全局 cgroup。
// 注意：该方法不再返回错误，而是把错误记录到 logger。
func (c *Cgroup) Cleanup() {
	if !strings.HasPrefix(c.path, "/sys/fs/cgroup/hustoj") {
		return
	}
	procsFile := filepath.Join(c.path, "cgroup.procs")
	pprocs := "/sys/fs/cgroup/cgroup.procs"
	if data, err := os.ReadFile(procsFile); err == nil {
		for pidstr := range strings.FieldsSeq(string(data)) {
			if pidErr := os.WriteFile(pprocs, []byte(pidstr), 0o644); err == nil {
				c.logger.Info("remove pid", "pid", pidstr, "pprocs", pprocs)
			} else {
				c.logger.Info("remove pid failed", "pid", pidstr, "err", pidErr)
			}
		}
	}
	if err := os.RemoveAll(c.path); err != nil {
		c.logger.Info("cgrouppath remove failed", "path", c.path, "error", err)
	}
}

// ---------------------------------------------------------------------------
// 工具函数（保留与之前相同的实现，只是移到包级别）
// ---------------------------------------------------------------------------

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
				return time.Duration(usec) * time.Microsecond, nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("扫描 %s 时出错: %w", statFile, err)
	}

	return 0, fmt.Errorf("在 %s 中未找到 'usage_usec' 字段", statFile)
}
