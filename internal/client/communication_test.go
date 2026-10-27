package client

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareCommunicationPairsAns(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"0.in", "0.ans", "1.in", "1.out"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "validator"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}

	pairs := [][]string{
		{filepath.Join(dir, "0.in"), filepath.Join(dir, "0.out")},
		{filepath.Join(dir, "1.in"), filepath.Join(dir, "1.out")},
	}
	jc := &JudgeClient{}
	info, got := jc.prepareCommunication(dir, pairs)

	if info.ValidatorSrc != filepath.Join(dir, "validator") {
		t.Errorf("ValidatorSrc = %q, 期望 validator", info.ValidatorSrc)
	}
	if len(got) != 2 {
		t.Fatalf("配对数 = %d, 期望 2", len(got))
	}
	// 有 .ans 时用它替换 .out；缺 .ans 时留空，由判题侧补空答案。
	if got[0][1] != filepath.Join(dir, "0.ans") {
		t.Errorf("0.in 的答案 = %q, 期望 0.ans", got[0][1])
	}
	if got[1][1] != "" {
		t.Errorf("1.in 缺 .ans 时应留空，得到 %q", got[1][1])
	}
	// .in 本身不能被改写。
	if got[0][0] != filepath.Join(dir, "0.in") || got[1][0] != filepath.Join(dir, "1.in") {
		t.Errorf("输入配对被改写: %v", got)
	}
}

func TestFindCommunicationValidator(t *testing.T) {
	dir := t.TempDir()
	if got := findCommunicationValidator(dir); got != "" {
		t.Errorf("空目录应返回空，得到 %q", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "output_validator"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findCommunicationValidator(dir); !strings.HasSuffix(got, "output_validator") {
		t.Errorf("应回退到 output_validator，得到 %q", got)
	}

	// validator 优先于 output_validator。
	if err := os.WriteFile(filepath.Join(dir, "validator"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findCommunicationValidator(dir); filepath.Base(got) != "validator" {
		t.Errorf("应优先取 validator，得到 %q", got)
	}
}

func TestReadRegularFileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	secret := filepath.Join(dir, "secret")
	if err := os.WriteFile(secret, []byte("host-only"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "judgemessage.txt")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}
	if got := readRegularFile(link, 1024); got != "" {
		t.Errorf("符号链接不应被读取，得到 %q", got)
	}

	// 普通文件受长度限制。
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(link, []byte(strings.Repeat("a", 100)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := readRegularFile(link, 10); got != "aaaaaaaaaa" {
		t.Errorf("readRegularFile 限长失败: %q", got)
	}
}
