package client

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/sempr/hustoj-go/pkg/language"
	"golang.org/x/sys/unix"
)

func (jc *JudgeClient) workBaseDir() string {
	if jc.task != nil {
		return filepath.Join(jc.workBase, fmt.Sprintf("judge-%d", jc.solutionID))
	}
	return filepath.Join(jc.config.OJHome, "run"+jc.runnerID)
}

func (jc *JudgeClient) setupWorkEnvironment(langConfig *language.LangConfig) (string, error) {
	workBaseDir := jc.workBaseDir()

	for _, dir := range []string{"rootfs", "tmp"} {
		if err := os.MkdirAll(filepath.Join(workBaseDir, dir), 0o755); err != nil {
			return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	tmpfsDir := filepath.Join(workBaseDir, "tmp")
	if err := unix.Mount("tmpfs", tmpfsDir, "tmpfs", uintptr(unix.MS_NOSUID|unix.MS_NODEV), "size=580M"); err != nil {
		return "", fmt.Errorf("failed to mount tmpfs: %w", err)
	}

	for _, dir := range []string{"upper", "work"} {
		if err := os.MkdirAll(filepath.Join(tmpfsDir, dir), 0o755); err != nil {
			return "", fmt.Errorf("failed to create overlay directory %s: %w", dir, err)
		}
	}

	options := fmt.Sprintf(
		"lowerdir=%s,upperdir=%s,workdir=%s",
		langConfig.Fs.Base,
		filepath.Join(workBaseDir, "tmp", "upper"),
		filepath.Join(workBaseDir, "tmp", "work"),
	)

	rootfs := filepath.Join(workBaseDir, "rootfs")
	if err := unix.Mount("overlay", rootfs, "overlay", 0, options); err != nil {
		return "", fmt.Errorf("failed to mount overlay: %w", err)
	}

	return rootfs, nil
}

func (jc *JudgeClient) cleanupWorkEnvironment(rootfs string) {
	if jc.debug {
		slog.Info("Keeping rootfs due to debug option", "rootfs", rootfs)
		return
	}

	if err := jc.unmountWithRetry(rootfs, 3*time.Second); err != nil {
		slog.Warn("Failed to unmount overlay", "error", err)
	}

	tmpfsDir := filepath.Join(filepath.Dir(rootfs), "tmp")
	if err := jc.unmountWithRetry(tmpfsDir, 3*time.Second); err != nil {
		slog.Warn("Failed to unmount tmpfs", "error", err)
	}

	workBaseDir := filepath.Dir(rootfs)
	if err := os.RemoveAll(workBaseDir); err != nil {
		slog.Warn("Failed to remove work directory", "path", workBaseDir, "error", err)
	}
}

// unmountWithRetry 在挂载点被进程短暂占用（EBUSY）时等待并重试 umount，
// 超过 maxWait 后改用 lazy umount（MNT_DETACH）兜底，避免残留挂载点。
func (jc *JudgeClient) unmountWithRetry(mountpoint string, maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)
	for {
		if err := unix.Unmount(mountpoint, 0); err == nil {
			return nil
		} else if errors.Is(err, unix.EBUSY) {
			if time.Now().Before(deadline) {
				time.Sleep(100 * time.Millisecond)
				continue
			}
			if lerr := unix.Unmount(mountpoint, unix.MNT_DETACH); lerr == nil {
				slog.Warn("Forced lazy unmount after EBUSY", "mountpoint", mountpoint)
				return nil
			}
			return err
		} else {
			return err
		}
	}
}
