package namefile

import (
	"strings"
	"testing"
)

func TestParseName(t *testing.T) {
	valid := map[string]string{
		"data.txt":     "data.txt",
		"  data.txt  ": "data.txt",
		"":             "",
		"   ":          "",
		"input.file":   "input.file",
	}
	for in, want := range valid {
		got, err := ParseName(in)
		if err != nil {
			t.Errorf("ParseName(%q) 返回错误: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseName(%q) = %q，期望 %q", in, got, want)
		}
	}

	invalid := []string{
		".",
		"..",
		"../etc/passwd",
		"../../../../etc/cron.d/pwn",
		"/etc/passwd",
		"sub/data.txt",
		`sub\data.txt`,
		"a\nb",
		"a\x00b",
	}
	for _, in := range invalid {
		if got, err := ParseName(in); err == nil {
			t.Errorf("ParseName(%q) 应被拒绝，得到 %q", in, got)
		}
	}
}

func TestParseNameLengthLimits(t *testing.T) {
	long := strings.Repeat("a", MaxNameLength+1)
	if _, err := ParseName(long); err == nil {
		t.Fatal("超长文件名应被拒绝")
	}
	if _, err := ParseName(strings.Repeat("a", MaxNameContentSize+1)); err == nil {
		t.Fatal("超长内容应被拒绝")
	}
}

func TestSanitizeName(t *testing.T) {
	if got := SanitizeName("../etc/passwd"); got != "" {
		t.Fatalf("SanitizeName 接受了路径穿越: %q", got)
	}
	if got := SanitizeName("data.txt"); got != "data.txt" {
		t.Fatalf("SanitizeName 拒绝合法文件名: %q", got)
	}
}

func TestValidateFileContent(t *testing.T) {
	if err := ValidateFileContent(InputNameFile, "data.in"); err != nil {
		t.Fatalf("合法 input.name 被拒绝: %v", err)
	}
	if err := ValidateFileContent(OutputNameFile, "data.out"); err != nil {
		t.Fatalf("合法 output.name 被拒绝: %v", err)
	}
	if err := ValidateFileContent(InputNameFile, "../../etc/passwd"); err == nil {
		t.Fatal("非法 input.name 应被拒绝")
	}
	// 其它文件不看内容。
	if err := ValidateFileContent("1.in", "../../anything"); err != nil {
		t.Fatalf("普通数据文件不应被校验: %v", err)
	}
}

func TestIsNameFile(t *testing.T) {
	if !IsNameFile(InputNameFile) || !IsNameFile(OutputNameFile) {
		t.Fatal("IsNameFile 漏判")
	}
	if IsNameFile("data.in") {
		t.Fatal("IsNameFile 误判普通数据文件")
	}
}
