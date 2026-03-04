package client

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/sempr/hustoj-go/pkg/language"
	"golang.org/x/sys/unix"
)

func (jc *JudgeClient) setupWorkEnvironment(langConfig *language.LangConfig) (string, error) {
	workBaseDir := filepath.Join(jc.config.OJHome, "run"+jc.runnerID)

	for _, dir := range []string{"rootfs", "tmp"} {
		if err := os.MkdirAll(filepath.Join(workBaseDir, dir), 0755); err != nil {
			return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	tmpfsDir := filepath.Join(workBaseDir, "tmp")
	if err := unix.Mount("tmpfs", tmpfsDir, "tmpfs", uintptr(unix.MS_NOSUID|unix.MS_NODEV), "size=580M"); err != nil {
		return "", fmt.Errorf("failed to mount tmpfs: %w", err)
	}

	for _, dir := range []string{"upper", "work"} {
		if err := os.MkdirAll(filepath.Join(tmpfsDir, dir), 0755); err != nil {
			return "", fmt.Errorf("failed to create overlay directory %s: %w", dir, err)
		}
	}

	options := fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s",
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

	if err := unix.Unmount(rootfs, 0); err != nil {
		slog.Warn("Failed to unmount overlay", "error", err)
	}

	tmpfsDir := filepath.Join(filepath.Dir(rootfs), "tmp")
	if err := unix.Unmount(tmpfsDir, 0); err != nil {
		slog.Warn("Failed to unmount tmpfs", "error", err)
	}

	workBaseDir := filepath.Dir(rootfs)
	if err := os.RemoveAll(workBaseDir); err != nil {
		slog.Warn("Failed to remove work directory", "path", workBaseDir, "error", err)
	}
}
