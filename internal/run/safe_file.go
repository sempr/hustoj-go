package run

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"golang.org/x/sys/unix"
)

// openNoFollow 打开沙箱的数据文件，拒绝最后一级符号链接。
//
// run 子进程以 root 身份在进入 mount namespace 之前打开这些文件；而 /code
// 在编译阶段对选手可写，选手可以留下 data.usr -> /etc/... 的符号链接。
// 普通 os.Create/os.Open 会跟随链接，造成宿主机的任意读写。这里统一用
// O_NOFOLLOW，并校验类型，只允许普通文件（输入/输出还允许 FIFO）。
func openNoFollow(path string, flag int, perm os.FileMode, allowFIFO bool) (*os.File, error) {
	f, err := os.OpenFile(path, flag|unix.O_NOFOLLOW, perm)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if info.Mode().IsRegular() {
		return f, nil
	}
	if allowFIFO && info.Mode()&os.ModeNamedPipe != 0 {
		return f, nil
	}
	f.Close()
	return nil, fmt.Errorf("%s 不是允许的文件类型（mode=%s）", path, info.Mode())
}

// createSandboxOutput 创建/截断 stdout/stderr 文件（普通文件或 FIFO）。
func createSandboxOutput(path string) (*os.File, error) {
	return openNoFollow(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o666, true)
}

// openSandboxInput 打开 stdin 文件（普通文件或 FIFO）。
func openSandboxInput(path string) (*os.File, error) {
	return openNoFollow(path, os.O_RDONLY, 0, true)
}

// readBoundedRegularFile 只读取普通文件，并限制最大字节数。
//
// 旧实现直接 os.ReadFile：data.usr 一旦被换成指向 /dev/zero 的链接，
// 判题器会一直读到 OOM；即使不是链接，读几百 MB 输出也只保留前 1KB。
func readBoundedRegularFile(path string, max int64) (string, error) {
	f, err := openNoFollow(path, os.O_RDONLY|unix.O_NONBLOCK, 0, false)
	if err != nil {
		return "", err
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, max))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ensureDevNull 确保 rootfs 里的 /dev/null 是字符设备。
//
// 旧的“存在就跳过”逻辑遇到普通文件（例如镜像里带了一个空 /dev/null）时
// 会一直保留普通文件，程序写 /dev/null 实际写进 tmpfs。
// ensureDevNull 确保 sandbox rootfs 里 /dev/null 可用。
//
// 在 rootless user namespace 里，overlay 无法创建字符设备，因此上层会在
// overlay upper 里落下一个可读写的普通文件 (mode 0666) 作为占位。这里
// 只在路径完全不存在时尽力补建普通文件; 已存在的regular file 直接复用，
// 不再 Remove/mknod，避免每次交互式测试重编译 /dev/null 时的 races.
func ensureDevNull(path string) error {
	info, err := os.Lstat(path)
	if err == nil {
		if info.Mode()&os.ModeCharDevice != 0 {
			if f, err2 := os.OpenFile(path, os.O_RDWR, 0); err2 == nil {
				f.Close()
				return nil
			}
			return nil
		}
		// Try to create char device only if we can (not in userns without CAP_MKNOD)
		if os.Geteuid() == 0 {
			tmp := path + ".tmp"
			if err := unix.Mknod(tmp, unix.S_IFCHR|0o666, int(unix.Mkdev(1, 3))); err == nil {
				os.Remove(path)
				os.Rename(tmp, path)
				return nil
			}
			// mknod failed (e.g., userns without CAP_MKNOD), keep regular file
		}
		// rootless/fallback: keep regular file, just truncate
		if err := os.Truncate(path, 0); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if !os.IsNotExist(err) {
		return err
	}
	// File doesn't exist: create regular file
	f, ferr := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o666)
	if ferr != nil {
		return ferr
	}
	f.Close()
	return nil
}


// sandboxPath 把沙箱内绝对路径拼到 rootfs 下，并拒绝相对路径。
//
// 只做 path.Clean 再 Join 时，"../../etc" 这类相对路径可能把结果带到
// rootfs 之外；这里强制要求是沙箱内绝对路径。
func sandboxPath(rootfs, inner string) (string, error) {
	clean := path.Clean(inner)
	if !path.IsAbs(clean) {
		return "", fmt.Errorf("沙箱内路径必须是绝对路径: %q", inner)
	}
	return path.Join(rootfs, clean), nil
}

// sandboxEnv 构造传给沙箱的环境变量。
//
// 旧实现把 os.Environ() 原样传进去，一旦按文档给 daemon 配了
// EnvironmentFile=judge.conf，数据库密码就会出现在选手进程的环境里。
// 这里保留语言运行时需要的变量，但剔除判题器/数据库/云凭据等敏感项。
func sandboxEnv() []string {
	env := []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin"}
	for _, kv := range os.Environ() {
		key, _, ok := strings.Cut(kv, "=")
		if !ok || key == "" || key == "PATH" {
			continue
		}
		if isSensitiveEnvKey(key) {
			continue
		}
		env = append(env, kv)
	}
	return env
}

func isSensitiveEnvKey(key string) bool {
	u := strings.ToUpper(key)
	// OJ_SECCOMP_MODE 是 tini 需要的开关；其余 OJ_*（数据库、语言集等）
	// 都不需要进入沙箱。
	if u == "OJ_SECCOMP_MODE" {
		return false
	}
	for _, prefix := range []string{
		"OJ_", "MYSQL", "MARIADB", "REDIS", "DB_", "DATABASE_",
		"AWS_", "AZURE_", "GCP_", "GOOGLE_", "OPENAI_", "ANTHROPIC_",
	} {
		if strings.HasPrefix(u, prefix) {
			return true
		}
	}
	for _, part := range []string{
		"PASSWORD", "PASSWD", "SECRET", "TOKEN", "APIKEY", "API_KEY",
		"CREDENTIAL", "PRIVATE_KEY", "ACCESS_KEY", "SESSION_KEY",
		"SSH_AUTH_SOCK", "COOKIE",
	} {
		if strings.Contains(u, part) {
			return true
		}
	}
	return false
}
