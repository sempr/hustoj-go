package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestReadBoundedRegularFileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret")
	link := filepath.Join(dir, "data.usr")
	if err := os.WriteFile(secret, []byte("host-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}

	if got, err := readBoundedRegularFile(link, 1024); err == nil {
		t.Fatalf("符号链接不应被读取，得到 %q", got)
	}
}

func TestReadBoundedRegularFileLimits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.err")
	if err := os.WriteFile(path, []byte(strings.Repeat("a", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readBoundedRegularFile(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got != "aaaaaaaaaa" {
		t.Fatalf("限长读取失败: %q", got)
	}
}

func TestReadCombinedOutputRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "judge.conf")
	if err := os.WriteFile(secret, []byte("OJ_PASSWORD=secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(dir, "data.usr")); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}

	got := readCombinedOutput(dir)
	if strings.Contains(got, "OJ_PASSWORD") {
		t.Fatalf("readCombinedOutput 跟随符号链接读到了宿主机文件: %q", got)
	}
}

func TestCreateSandboxOutputRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "host-target")
	link := filepath.Join(dir, "data.usr")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}

	if f, err := createSandboxOutput(link); err == nil {
		f.Close()
		t.Fatal("stdout 为符号链接时应拒绝打开")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != int64(len("original")) {
		t.Fatalf("符号链接目标被截断，size=%d", info.Size())
	}
}

func TestSandboxEnvStripsSecrets(t *testing.T) {
	t.Setenv("OJ_PASSWORD", "db-secret")
	t.Setenv("MYSQL_PWD", "mysql-secret")
	t.Setenv("SOME_API_TOKEN", "token-secret")
	t.Setenv("GOCACHE", "/tmp/gocache")
	t.Setenv("OJ_SECCOMP_MODE", "enforce")

	env := strings.Join(sandboxEnv(), "\n")
	if strings.Contains(env, "db-secret") || strings.Contains(env, "mysql-secret") || strings.Contains(env, "token-secret") {
		t.Fatalf("敏感环境变量泄漏进沙箱: %s", env)
	}
	if !strings.Contains(env, "GOCACHE=/tmp/gocache") {
		t.Fatalf("语言运行时需要的环境变量被误删: %s", env)
	}
	if !strings.Contains(env, "OJ_SECCOMP_MODE=enforce") {
		t.Fatalf("OJ_SECCOMP_MODE 应保留: %s", env)
	}
}

func TestReadBoundedRegularFileRejectsFIFOWithoutBlocking(t *testing.T) {
	fifo := filepath.Join(t.TempDir(), "data.err")
	if err := unix.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("无法创建 FIFO: %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := readBoundedRegularFile(fifo, 1024)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("FIFO 不应被当作普通文件读取")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("读取 FIFO 时阻塞了")
	}
}

func TestSandboxPathRejectsRelative(t *testing.T) {
	if _, err := sandboxPath("/rootfs", "../../etc/passwd"); err == nil {
		t.Fatal("相对路径应被拒绝")
	}
	got, err := sandboxPath("/rootfs", "/code/../code/data.in")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/rootfs/code/data.in" {
		t.Fatalf("sandboxPath 结果错误: %q", got)
	}
}

func TestEnsureDevNullReplacesRegularFile(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("需要 root 才能 mknod")
	}
	dir := t.TempDir()
	devNull := filepath.Join(dir, "null")
	if err := os.WriteFile(devNull, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureDevNull(devNull); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(devNull)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		t.Fatalf("/dev/null 仍是普通文件: %s", info.Mode())
	}
}
