package sandbox

import (
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

func chRoot() {
	newRootPath := config.Rootfs
	if err := syscall.Mount("none", "/", "none", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
		logger.Error("Shim 错误: Failed to mark root private", "err", err)
		os.Exit(1)
	}
	if err := syscall.Mount(newRootPath, newRootPath, "bind", syscall.MS_BIND, ""); err != nil {
		logger.Error("Shim 错误: bind-mount", "rootPath", newRootPath, "err", err)
		os.Exit(1)
	}
	putOldPath := filepath.Join(newRootPath, ".old_root")
	if err := os.MkdirAll(putOldPath, 0700); err != nil {
		logger.Error("Shim 错误: 创建 失败", "putOldPath", putOldPath, "err", err)
		os.Exit(1)
	}

	if err := syscall.PivotRoot(newRootPath, putOldPath); err != nil {
		logger.Error("Shim 错误: PivotRoot 失败:  newRootPath=", "err", err, "newRootPath", newRootPath)
		os.Exit(1)
	}

	if err := syscall.Chdir("/"); err != nil {
		logger.Error("Shim 错误: Chdir('/') 失败", "err", err)
		os.Exit(1)
	}
	if err := syscall.Unmount("/.old_root", syscall.MNT_DETACH); err != nil {
		logger.Error("Shim 错误: Unmount '/.old_root' 失败", "err", err)
		os.Exit(1)
	}

	if err := os.RemoveAll("/.old_root"); err != nil {
		logger.Warn("Shim 警告: RemoveAll '/.old_root' 失败", "err", err)
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
