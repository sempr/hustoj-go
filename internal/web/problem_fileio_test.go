package web

import (
	"os"
	"path/filepath"
	"testing"
)

// problemFileIONames 读取 <OJHome>/data/<id>/input.name、output.name，
// 去掉首尾空白；题目没有这两个文件时都返回空串。
func TestProblemFileIONames(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "data", "42")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "input.name"), []byte("sort.in\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "output.name"), []byte("  sort.out  "), 0o644); err != nil {
		t.Fatal(err)
	}

	in, out := problemFileIONames(home, 42)
	if in != "sort.in" {
		t.Errorf("input = %q，期望 sort.in（去空白）", in)
	}
	if out != "sort.out" {
		t.Errorf("output = %q，期望 sort.out（去空白）", out)
	}

	// 无数据目录：都为空。
	if in, out := problemFileIONames(home, 999); in != "" || out != "" {
		t.Errorf("无数据目录时应为空，得到 %q / %q", in, out)
	}

	// 只配了输入文件（部分题目只固定输入）。
	dir2 := filepath.Join(home, "data", "7")
	if err := os.MkdirAll(dir2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "input.name"), []byte("a.in"), 0o644); err != nil {
		t.Fatal(err)
	}
	if in, out := problemFileIONames(home, 7); in != "a.in" || out != "" {
		t.Errorf("仅输入时 = %q / %q，期望 a.in / 空", in, out)
	}
}
