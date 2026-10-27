package langinfo

import (
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoExtra 返回仓库内 extra 目录（含 etc/langs/all.toml 与 <id>.lang.toml）。
func repoExtra(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "extra"))
	if err != nil {
		t.Fatalf("解析 extra 目录失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "etc", "langs", "all.toml")); err != nil {
		t.Skipf("all.toml 不存在，跳过: %v", err)
	}
	return dir
}

// TestParseVersionsTxt 覆盖 check_versions.sh 输出的解析：
// 分节按 id 归位、跳过空行取首条非空、无法识别的首行（命令回显）忽略。
func TestParseVersionsTxt(t *testing.T) {
	in := `cd extra && bash check_versions.sh
==> 0 (C):
gcc (Debian 14.2.0-19) 14.2.0
Copyright (C) 2024 ...
==> 1 (c++):
g++ (Debian 14.2.0-19) 14.2.0
==> 2 (pascal):
Free Pascal Compiler version 3.2.2+dfsg-46 [2025/02/08] for x86_64
Fatal: No source file name in command line
==> 6 (pascal):

Python 3.14.0
==> 17 (Go):
go version go1.27.1 linux/amd64
`
	got := ParseVersionsTxt(strings.NewReader(in))
	want := map[int]string{
		0:  "gcc (Debian 14.2.0-19) 14.2.0",
		1:  "g++ (Debian 14.2.0-19) 14.2.0",
		2:  "Free Pascal Compiler version 3.2.2+dfsg-46 [2025/02/08] for x86_64",
		6:  "Python 3.14.0",
		17: "go version go1.27.1 linux/amd64",
	}
	if len(got) != len(want) {
		t.Fatalf("解析出 %d 项，期望 %d 项: %v", len(got), len(want), got)
	}
	for id, v := range want {
		if got[id] != v {
			t.Errorf("id=%d 版本=%q，期望 %q", id, got[id], v)
		}
	}
}

// TestBuildCoversInstalledLanguages 断言只列已安装语言，且带上其编译/运行命令。
func TestBuildCoversInstalledLanguages(t *testing.T) {
	entries, err := Build(repoExtra(t), map[int]string{1: "g++ 14.2.0"})
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("Build 未返回任何语言")
	}

	byID := make(map[int]Entry, len(entries))
	for _, e := range entries {
		byID[e.ID] = e
		if e.DisplayName == "" {
			t.Errorf("id=%d 展示名为空", e.ID)
		}
	}

	// 1 = C++，应为已安装并带版本与命令。
	cpp, ok := byID[1]
	if !ok {
		t.Fatalf("缺少 C++(id=1): %v", byID)
	}
	if cpp.Version != "g++ 14.2.0" {
		t.Errorf("C++ 版本=%q，期望 g++ 14.2.0", cpp.Version)
	}
	if !strings.Contains(cpp.Compile, "Main") || cpp.Run == "" {
		t.Errorf("C++ 编译/运行命令异常: compile=%q run=%q", cpp.Compile, cpp.Run)
	}

	// 4 = Ruby，仓库 extra 下无 <id>.lang.toml，不应出现。
	if _, ok := byID[4]; ok {
		t.Errorf("未安装的 Ruby(id=4) 不应出现在表中")
	}
}

// TestRenderHTMLEscapesAndLists 断言渲染结果含展示名/命令且对特殊字符转义。
func TestRenderHTMLEscapesAndLists(t *testing.T) {
	entries := []Entry{
		{ID: 1, DisplayName: "C++", Version: "g++ 14.2.0", Compile: `g++ -o Main Main.cc && echo "<ok>"`, Run: "/code/Main"},
	}
	out := RenderHTML(entries)
	// RenderHTML 只用 html.EscapeString（不转义 +），展示名/版本应原样出现。
	display := html.UnescapeString(out)
	if !strings.Contains(display, "C++") || !strings.Contains(display, "g++ 14.2.0") {
		t.Errorf("片段缺少展示名/版本:\n%s", out)
	}
	if strings.Contains(out, "<ok>") {
		t.Errorf("命令里的尖括号未被转义:\n%s", out)
	}
	if !strings.Contains(out, "&lt;ok&gt;") {
		t.Errorf("命令里的尖括号应被转义成 &lt;ok&gt;:\n%s", out)
	}
	if !strings.Contains(out, "freopen") {
		t.Errorf("片段应提示 freopen:\n%s", out)
	}
}

// TestParseVersionsTxtEmpty 空输入返回空表。
func TestParseVersionsTxtEmpty(t *testing.T) {
	if got := ParseVersionsTxt(strings.NewReader("")); len(got) != 0 {
		t.Errorf("空输入应返回空表，得到 %v", got)
	}
}
