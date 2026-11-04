package client

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestSanitizeTestDataName(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"data.txt", "data.txt"},
		{"  data.txt  ", "data.txt"},
		{"", ""},
		{".", ""},
		{"..", ""},
		{"../etc/passwd", ""},
		{"../../../../etc/cron.d/pwn", ""},
		{"/etc/passwd", ""},
		{"sub/data.txt", ""},
		{`sub\data.txt`, ""},
		{"a\nb", ""},
	}
	for _, tc := range tests {
		if got := sanitizeTestDataName(tc.in); got != tc.want {
			t.Errorf("sanitizeTestDataName(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

func TestFindInputNameRejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "input.name"), []byte("../../etc/passwd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	jc := &JudgeClient{}
	if got := jc.findInputName(dir); got != "" {
		t.Fatalf("findInputName 接受了路径穿越: %q", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "input.name"), []byte("data.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := jc.findInputName(dir); got != "data.txt" {
		t.Fatalf("合法 input.name 被拒绝: %q", got)
	}
}

func TestCopyFileRefusesSymlinkDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	target := filepath.Join(dir, "host-target")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("sandbox-data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, dst); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}

	if err := copyFileWithMode(src, dst, 0o666); err == nil {
		t.Fatal("目标为符号链接时 copyFileWithMode 应失败")
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original" {
		t.Fatalf("符号链接目标被改写: %q", got)
	}
}

func TestCreateEmptyWithModeRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "host-target")
	dst := filepath.Join(dir, "data.usr")
	if err := os.WriteFile(target, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, dst); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}

	if err := createEmptyWithMode(dst, 0o666); err == nil {
		t.Fatal("目标为符号链接时 createEmptyWithMode 应失败")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != int64(len("original")) {
		t.Fatalf("符号链接目标被截断，size=%d", info.Size())
	}
}

func TestSanitizeCompileTree(t *testing.T) {
	dir := t.TempDir()
	regular := filepath.Join(dir, "Main")
	target := filepath.Join(dir, "host-target")
	link := filepath.Join(dir, "data.usr")
	fifo := filepath.Join(dir, "pipe_po")
	if err := os.WriteFile(regular, []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("host"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}
	if err := unix.Mkfifo(fifo, 0o644); err != nil {
		t.Skipf("无法创建 FIFO: %v", err)
	}

	sanitizeCompileTree(dir)

	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Errorf("符号链接未被清理: %v", err)
	}
	if _, err := os.Lstat(fifo); !os.IsNotExist(err) {
		t.Errorf("FIFO 未被清理: %v", err)
	}
	if _, err := os.Stat(regular); err != nil {
		t.Errorf("普通编译产物被误删: %v", err)
	}
}
