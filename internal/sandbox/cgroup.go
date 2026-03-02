package sandbox

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var ErrCgroupLimitExceeded = fmt.Errorf("cgroup CPU time limit exceeded")
var ErrRealTimeTimeout = fmt.Errorf("real-time execution timeout")
var ErrRuntimeError = fmt.Errorf("runtime error")
var ErrOutputLimitExceeded = fmt.Errorf("output limit exceed")

func runCPUChecker(
	ctx context.Context,
	startTime time.Time,
	cgroupCPULimit time.Duration,
	realTimeLimit time.Duration,
	cgroupStatFile string,
	logger *slog.Logger,
) error {
	logger.Info("CPU Checker: 启动...")
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			consumedCPUTime, err := readCgroupCPUTime(cgroupStatFile)
			if err != nil {
				logger.Warn("CPU Checker: 读取 cgroup 失败", "error", err)
				continue
			}

			if consumedCPUTime > cgroupCPULimit {
				logger.Warn("违规! Cgroup CPU 时间超出限制",
					"consumed_cpu_sec", consumedCPUTime.Seconds(), "limit_cpu_sec", cgroupCPULimit.Seconds())
				return ErrCgroupLimitExceeded
			}

			elapsedRealTime := time.Since(startTime)
			if elapsedRealTime > realTimeLimit {
				logger.Warn("违规! 物理时间超出限制",
					"elapsed_real_sec", elapsedRealTime.Seconds(), "limit_real_sec", realTimeLimit.Seconds())
				return ErrRealTimeTimeout
			}

		case <-ctx.Done():
			logger.Info("CPU Checker: 收到停止信号，停止检查。")
			return nil
		}
	}
}

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

func setupCgroup(solutionId int, childPid int, memoryLimit int) (string, error) {
	cgroupPath := filepath.Join("/sys/fs/cgroup", "hustoj", fmt.Sprintf("run-%d-%d", solutionId, childPid))
	err := os.MkdirAll(cgroupPath, 0644)
	if err != nil {
		return "", err
	}

	err = os.WriteFile(filepath.Join("/sys/fs/cgroup", "cgroup.subtree_control"), []byte("+cpu +memory +pids"), 0644)
	if err != nil {
		return "", err
	}
	err = os.WriteFile(filepath.Join("/sys/fs/cgroup", "hustoj", "cgroup.subtree_control"), []byte("+cpu +memory +pids"), 0644)
	if err != nil {
		return "", err
	}

	if err = os.WriteFile(filepath.Join(cgroupPath, "memory.max"), fmt.Appendf(nil, "%d", memoryLimit+4096), 0644); err != nil {
		return "", err
	}

	if err = os.WriteFile(filepath.Join(cgroupPath, "cpu.max"), fmt.Appendf(nil, "120000 100000"), 0644); err != nil {
		return "", err
	}

	if err = os.WriteFile(filepath.Join(cgroupPath, "pids.max"), fmt.Appendf(nil, "64"), 0644); err != nil {
		return "", err
	}

	err = os.WriteFile(filepath.Join(cgroupPath, "cgroup.procs"), fmt.Append(nil, childPid), 0644)
	return cgroupPath, err
}

func cleanupCgroup(cgroupPath string) {
	if strings.HasPrefix(cgroupPath, "/sys/fs/cgroup/hustoj") {
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
			slog.Info("cgrouppath remove failed", "path", cgroupPath, "error", err)
		}
	}
}
