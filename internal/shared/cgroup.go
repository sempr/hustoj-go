package shared

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
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

// cgroupControllers 是判题需要的控制器。
//
// memory 用于 memory.max，cpu 用于 cpu.stat/cpu.max，pids 用于 pids.max。
var cgroupControllers = []string{"cpu", "memory", "pids"}

type Cgroup struct {
	solutionID  int
	childPid    int
	memoryLimit int

	// root 是 cgroup2 在当前命名空间里的挂载点（绝对路径）。
	// parent 是判题共用的父 cgroup（默认 <root>/hustoj，可用
	// OJ_CGROUP_ROOT 覆盖）。path 是本次运行独占的子 cgroup。
	root   string
	parent string
	path   string

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
//  1. 解析当前命名空间里的 cgroup2 挂载点（不再假定 /sys/fs/cgroup）。
//  2. 逐级启用 memory/cpu/pids 控制器。
//  3. 创建本次运行独占的子目录（带随机后缀，避免 PID 重用撞上残留目录）。
//  4. 写入 memory.max、cpu.max、pids.max、cgroup.procs。
func NewCgroup(solutionID, childPid, memoryLimit int, logger *slog.Logger) (*Cgroup, error) {
	root, err := cgroup2MountPoint()
	if err != nil {
		return nil, err
	}
	parent, err := cgroupParent(root)
	if err != nil {
		return nil, err
	}
	c := &Cgroup{
		solutionID:  solutionID,
		childPid:    childPid,
		memoryLimit: memoryLimit,
		root:        root,
		parent:      parent,
		path:        filepath.Join(parent, cgroupDirName(solutionID, childPid)),
		logger:      logger,
	}
	if err := c.setup(); err != nil {
		// 失败时删掉半成品目录，避免残留的空 cgroup 占着控制器。
		if isWithin(c.parent, c.path) {
			_ = os.Remove(c.path)
		}
		return nil, err
	}
	return c, nil
}

// setup 负责在文件系统里真正创建 cgroup 并写入必要文件。
func (c *Cgroup) setup() error {
	// 1. 父目录
	if err := os.MkdirAll(c.parent, 0o755); err != nil {
		return fmt.Errorf("创建 cgroup 父目录失败: %w", err)
	}

	// 2. 从挂载点到父目录逐级启用控制器。
	//    容器/systemd 通常已经委派好了，重复写是幂等的；如果这里没权限，
	//    后面写 memory.max 会失败，判题按系统错误处理（fail-closed）。
	if err := enableControllers(c.root, c.parent); err != nil {
		return err
	}

	// 3. 本次运行独占的目录。随机后缀保证即使旧进程崩溃留下同名目录，
	//    新任务也不会复用它的内存统计/限制。
	if err := os.Mkdir(c.path, 0o755); err != nil {
		return fmt.Errorf("创建 cgroup 目录失败: %w", err)
	}

	// 4. memory.max
	memFile := filepath.Join(c.path, "memory.max")
	if err := os.WriteFile(memFile, fmt.Appendf(nil, "%d", c.memoryLimit+4096), 0o644); err != nil {
		return fmt.Errorf("写 memory.max 失败: %w", err)
	}

	// 5. cpu.max (150ms per 100ms?? – 这里保持原值)
	cpuFile := filepath.Join(c.path, "cpu.max")
	if err := os.WriteFile(cpuFile, fmt.Appendf(nil, "%d %d", 15000, 10000), 0o644); err != nil {
		return fmt.Errorf("写 cpu.max 失败: %w", err)
	}

	// 6. pids.max
	pidsFile := filepath.Join(c.path, "pids.max")
	if err := os.WriteFile(pidsFile, fmt.Appendf(nil, "64"), 0o644); err != nil {
		return fmt.Errorf("写 pids.max 失败: %w", err)
	}

	// 7. cgroup.procs 将儿子进程写进去
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
	// 只允许清理 parent 下的子目录：路径异常时宁可不删，也不 rm -rf 到别处。
	if !isWithin(c.parent, c.path) {
		c.logger.Warn("cgroup 路径不在父目录内，跳过清理", "parent", c.parent, "path", c.path)
		return
	}
	procsFile := filepath.Join(c.path, "cgroup.procs")
	pprocs := filepath.Join(c.root, "cgroup.procs")
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
// cgroup 路径与控制器
// ---------------------------------------------------------------------------

// cgroup2MountPoint 从 /proc/self/mountinfo 解析 cgroup2 的挂载点。
//
// 不能假定 /sys/fs/cgroup：容器里可能挂在别处，也可能是 cgroup namespace
// 暴露出来的子目录。用实际挂载点才能保证写的是当前命名空间下有效的路径。
func cgroup2MountPoint() (string, error) {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return "", fmt.Errorf("读取 /proc/self/mountinfo 失败: %w", err)
	}
	return parseCgroup2Mount(string(data))
}

func parseCgroup2Mount(mountinfo string) (string, error) {
	for _, line := range strings.Split(mountinfo, "\n") {
		sep := strings.Index(line, " - ")
		if sep < 0 {
			continue
		}
		fields := strings.Fields(line[:sep])
		if len(fields) < 5 {
			continue
		}
		after := strings.Fields(line[sep+3:])
		if len(after) == 0 || after[0] != "cgroup2" {
			continue
		}
		mountPoint := unescapeMountPoint(fields[4])
		if !filepath.IsAbs(mountPoint) {
			continue
		}
		return filepath.Clean(mountPoint), nil
	}
	return "", fmt.Errorf("未找到 cgroup2 挂载点（需要 cgroup v2）")
}

// unescapeMountPoint 还原 mountinfo 里的八进制转义（空格写成 \040 等）。
func unescapeMountPoint(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	return strings.NewReplacer(
		`\040`, " ",
		`\011`, "\t",
		`\012`, "\n",
		`\134`, `\`,
	).Replace(s)
}

// cgroupParent 返回判题共用的父 cgroup。
//
// 默认 <root>/hustoj；OJ_CGROUP_ROOT 可覆盖（例如把判题器放进 systemd
// 已经委派的 slice 里）。覆盖值必须是绝对路径，避免相对路径随 cwd 漂移。
func cgroupParent(root string) (string, error) {
	if override := strings.TrimSpace(os.Getenv("OJ_CGROUP_ROOT")); override != "" {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("OJ_CGROUP_ROOT 必须是绝对路径: %q", override)
		}
		return filepath.Clean(override), nil
	}
	return filepath.Join(root, "hustoj"), nil
}

// cgroupDirName 生成每次运行独占的目录名。
//
// name 里的 solutionID / pid 都来自判题器自身（不是选手可控字符串），
// 再加 4 字节随机数，避免 PID 重用或崩溃残留目录让两次运行共用统计。
func cgroupDirName(solutionID, pid int) string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("run-%d-%d-%d", solutionID, pid, time.Now().UnixNano())
	}
	return fmt.Sprintf("run-%d-%d-%s", solutionID, pid, hex.EncodeToString(b[:]))
}

// enableControllers 从 root 到 dir 逐级启用 cgroupControllers。
func enableControllers(root, dir string) error {
	if !filepath.IsAbs(root) || !filepath.IsAbs(dir) {
		return fmt.Errorf("cgroup 路径必须是绝对路径: root=%q dir=%q", root, dir)
	}
	dirs := []string{root}
	if rel, err := filepath.Rel(root, dir); err == nil && isRelativeInside(rel) {
		cur := root
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			cur = filepath.Join(cur, part)
			dirs = append(dirs, cur)
		}
	} else {
		// 自定义目录不在挂载点下：只处理它自己，控制器需由上层委派。
		dirs = []string{dir}
	}
	for _, d := range dirs {
		if err := enableControllersAt(d); err != nil {
			return err
		}
	}
	return nil
}

// enableControllersAt 把 cgroupControllers 里该层可用的控制器写进
// subtree_control。已经启用时重复写是幂等的。
func enableControllersAt(dir string) error {
	controllersPath := filepath.Join(dir, "cgroup.controllers")
	data, err := os.ReadFile(controllersPath)
	if err != nil {
		return fmt.Errorf("读取 %s 失败（不是 cgroup v2 目录？）: %w", controllersPath, err)
	}
	available := make(map[string]bool)
	for _, name := range strings.Fields(string(data)) {
		available[name] = true
	}
	var wanted []string
	for _, name := range cgroupControllers {
		if available[name] {
			wanted = append(wanted, "+"+name)
		}
	}
	if len(wanted) == 0 {
		return nil
	}

	subtreePath := filepath.Join(dir, "cgroup.subtree_control")
	if err := os.WriteFile(subtreePath, []byte(strings.Join(wanted, " ")), 0o644); err != nil {
		// EBUSY：该层还有进程（常见于 cgroup 根，控制器已由 systemd 开好）。
		// EPERM/EACCES/EROFS：这一层不归判题器管，可能已经委派好了。
		// 都先跳过，真正的判据是稍后能否写出 memory.max。
		if errors.Is(err, syscall.EBUSY) || errors.Is(err, syscall.EPERM) ||
			errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EROFS) {
			return nil
		}
		return fmt.Errorf("启用 %s 的控制器失败: %w", dir, err)
	}
	return nil
}

// isWithin 判断 child 是否是 parent 下的子路径（两者都应是绝对路径）。
func isWithin(parent, child string) bool {
	if !filepath.IsAbs(parent) || !filepath.IsAbs(child) {
		return false
	}
	rel, err := filepath.Rel(parent, child)
	return err == nil && isRelativeInside(rel) && rel != "."
}

func isRelativeInside(rel string) bool {
	return rel != "" && rel != "." && rel != ".." &&
		!strings.HasPrefix(rel, ".."+string(filepath.Separator)) &&
		!filepath.IsAbs(rel)
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
