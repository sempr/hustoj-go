package client

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/sempr/hustoj-go/internal/rootless"
	"github.com/sempr/hustoj-go/pkg/language"
	"golang.org/x/sys/unix"
)

// workBaseDir returns a directory unique to this judging instance.
// The solution_id is appended with a 5-digit random number to avoid collisions
// when a solution is rejudged while a previous run is still alive or its
// workdir hasn't been cleaned yet.
func (jc *JudgeClient) workBaseDir() string {
	base := ""
	if jc.task != nil {
		base = filepath.Join(jc.workBase, fmt.Sprintf("judge-%d", jc.solutionID))
	} else {
		base = filepath.Join(jc.config.OJHome, "run"+jc.runnerID)
	}
	rand, _ := rand5()
	return fmt.Sprintf("%s-%05d", base, rand)
}

func rand5() (int, error) {
	b := make([]byte, 2)
	f, err := os.Open("/dev/urandom")
	if err != nil {
		return 0, err
	}
	defer f.Close()
	f.Read(b)
	return int(b[0])<<8 | int(b[1])&0xffff%100000, nil
}

func (jc *JudgeClient) setupWorkEnvironment(langConfig *language.LangConfig) (string, error) {
	return jc.mountOverlay(jc.workBaseDir(), langConfig)
}

// mountOverlay 在指定的工作目录上挂一套独立的 tmpfs + overlay。
// 普通评测只用一套；交互评测用它给选手和 interactor 各准备一套
// rootfs，二者共享只读 lowerdir，但 upper/tmpfs 完全独立。
func (jc *JudgeClient) mountOverlay(workBaseDir string, langConfig *language.LangConfig) (string, error) {
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

	upperDir := filepath.Join(tmpfsDir, "upper")
	options := fmt.Sprintf(
		"lowerdir=%s,upperdir=%s,workdir=%s",
		langConfig.Fs.Base,
		upperDir,
		filepath.Join(tmpfsDir, "work"),
	)

	if rootless.InUserNamespace() {
		// lowerdir 里的目录（dev/code）属主是宿主机 root，在 userns 里
		// 不可写，overlay 无法对它们 copy-up。先在 upper 里建好，merged
		// 目录就归判题用户所有，后续 mkdir/chmod/删除都正常。
		//
		// 除了 dev/code，还要预建 /tmp（及 /var/tmp）：编译器和运行时会在
		// 那里建临时文件。如果只依赖 lower 的目录，overlay 在 copy-up 目录
		// 元数据时会因为属主是未映射的宿主 root 而返回 EOVERFLOW。
		for _, dir := range []string{"dev", "code", "tmp", "var/tmp", "home"} {
			if err := os.MkdirAll(filepath.Join(upperDir, dir), 0o755); err != nil {
				return "", fmt.Errorf("failed to pre-create overlay %s: %w", dir, err)
			}
		}
		if err := os.Chmod(filepath.Join(upperDir, "code"), 0o777); err != nil {
			return "", fmt.Errorf("failed to chmod overlay code: %w", err)
		}
		for _, dir := range []string{"tmp", "var/tmp"} {
			if err := os.Chmod(filepath.Join(upperDir, dir), 0o1777); err != nil {
				return "", fmt.Errorf("failed to chmod overlay %s: %w", dir, err)
			}
		}
		// 优先借助 host-root helper 创建真正的 /dev/null 字符设备；
		// helper 不存在时回退 to upper 层普通文件占位。
		nullPath := filepath.Join(upperDir, "dev", "null")
		// 根less 无法 reliable mknod charset; 总是对齐上 regular 文件.
		slog.Info("Creating /dev/null in upper", "path", nullPath)
		if f, ferr := os.OpenFile(nullPath, os.O_CREATE|os.O_WRONLY, 0o666); ferr == nil {
			f.Close()
			slog.Info("Created /dev/null in upper", "path", nullPath)
		} else {
			slog.Error("Failed to create /dev/null in upper", "path", nullPath, "err", ferr)
		}
		_ = os.Chmod(nullPath, 0o666)

		// 非特权 userns 里的 overlay 必须使用 user xattr（内核要求）。
		options += ",userxattr"
	}

	rootfs := filepath.Join(workBaseDir, "rootfs")
	if err := unix.Mount("overlay", rootfs, "overlay", 0, options); err != nil {
		return "", fmt.Errorf("failed to mount overlay: %w", err)
	}

	// Ensure /dev/null exists in the merged overlay view
	nullPath := filepath.Join(rootfs, "dev", "null")
	slog.Info("Ensuring /dev/null in rootfs", "path", nullPath)
	if f, ferr := os.OpenFile(nullPath, os.O_CREATE|os.O_WRONLY, 0o666); ferr == nil {
		f.Close()
		slog.Info("Created /dev/null in rootfs", "path", nullPath)
	} else {
		slog.Error("Failed to create /dev/null in rootfs", "path", nullPath, "err", ferr)
	}
	_ = os.Chmod(nullPath, 0o666)

	for _, dir := range []string{"dev", "code", "tmp", "var/tmp", "home"} {
		_ = os.MkdirAll(filepath.Join(upperDir, dir), 0o755)
	}

	// Go 的预热 build cache 在 lower 的 /go-cache 里，但它是宿主机 root 所有：
	// 在 userns 中属于未映射 uid，直接往里写会因 overlay copy-up 失败而 EPERM。
	// 在 upper 里预建同名目录和 256 个分片目录（Go cache 的两级布局），
	// overlay 会把上下层目录合并：预热的条目仍然可读，新条目写进 upper，
	// 既保住了预热缓存，又允许编译进程写入。
	// 编译以 nobody 运行，而 MkdirAll 受 umask 影响，必须显式 chmod 0777。
	goCache := filepath.Join(upperDir, "go-cache")
	if err := os.MkdirAll(goCache, 0o777); err == nil {
		_ = os.Chmod(goCache, 0o777)
		for i := 0; i < 256; i++ {
			shard := filepath.Join(goCache, fmt.Sprintf("%02x", i))
			if err := os.MkdirAll(shard, 0o777); err != nil {
				continue
			}
			_ = os.Chmod(shard, 0o777)
		}
	}

	return rootfs, nil
}

func (jc *JudgeClient) cleanupWorkEnvironment(rootfs string) {
	if jc.debug {
		slog.Info("Keeping rootfs due to debug option", "rootfs", rootfs)
		return
	}

	// rootless 下 /dev/null 是 overlay 里额外的一层 bind mount，先摘掉，
	// 否则卸载 overlay 会 EBUSY。
	// Unbind the host-privileged /dev/null mount so upper stays clean.
	upperNull := filepath.Join(filepath.Dir(rootfs), "tmp", "upper", "dev", "null")
	rootless.UnmountNull(upperNull)

	// Also unbind the host-proc bind mount from rootfs/proc
	procMount := filepath.Join(rootfs, "proc")
	rootless.UnmountNull(procMount)

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
