package web

import (
	"strings"
	"testing"
)

func TestMarkdownHTML(t *testing.T) {
	// Markdown 基本语法能转成 HTML。
	out := string(markdownHTML("# 标题\n\n正文"))
	if !strings.Contains(out, "<h1") || !strings.Contains(out, "正文") {
		t.Fatalf("expected rendered heading+text, got: %s", out)
	}

	// 数学定界符原样保留，交给浏览器端 auto-render。
	math := markdownHTML("$$\n\\oint E\\cdot dA = q\n$$\n行内 $x^2 + y^2$，\n内联\\(a+b\\)和\\[c=d\\]")
	for _, want := range []string{"$$", "$x^2 + y^2$", "\\(", "\\["} {
		if !strings.Contains(string(math), want) {
			t.Fatalf("math delimiter %q must be preserved, got: %s", want, math)
		}
	}

	// 已服务器端预渲染的 KaTeX span 原样透传（迁移兼容）。
	katex := `<p>公式<span class="katex-display"><span class="katex-mathml"><math display="inline"><mi>E</mi></math></span><span class="katex-html" aria-hidden="true"><span class="base"><span class="mord">E</span></span></span></span>。</p>`
	ko := string(markdownHTML(katex))
	for _, want := range []string{"katex-display", "katex-mathml", "<math", "katex-html", "mord"} {
		if !strings.Contains(ko, want) {
			t.Fatalf("pre-rendered katex HTML must be preserved (%s missing), got: %s", want, ko)
		}
	}

	// 空输入安全。
	if string(markdownHTML("")) != "" {
		t.Fatal("empty input must yield empty output")
	}
}

func TestKatexOptions(t *testing.T) {
	o := string(katexOptions())
	if !strings.Contains(o, "$$") || !strings.Contains(o, `\\(`) || !strings.Contains(o, `\\[`) {
		t.Fatalf("unexpected katex options: %s", o)
	}
}
