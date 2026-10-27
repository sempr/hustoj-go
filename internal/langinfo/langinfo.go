// langinfo 生成 FAQ 页面用的"受支持语言"信息表。
//
// 数据来自两处，都是判题环境的既有产物，不在这里重新探测：
//
//   - 语言注册表 <OJHome>/etc/langs/all.toml 与每语言 <id>.lang.toml
//     （展示名、编译命令、运行命令、ver 命令）；
//   - 版本缓存（extra/check_versions.sh 的输出），按语言 id 提供版本号。
//
// 生成结果是一段可嵌入 FAQ 页的 HTML，落盘后由 Web 侧读取，
// 因此不需要在每次请求时挂载语言 rootfs 或重新跑 ver。
package langinfo

import (
	"bufio"
	"fmt"
	"html"
	"io"
	"regexp"
	"strings"

	"github.com/sempr/hustoj-go/pkg/language"
)

// Entry 是语言信息表的一行。
type Entry struct {
	// ID 是语言索引，与 all.toml / solution.language 一致。
	ID int
	// DisplayName 是展示名（来自 all.toml 的 display_name）。
	DisplayName string
	// Compile / Run 是 <id>.lang.toml 里的编译/运行命令。
	Compile string
	Run     string
	// Ver 是 <id>.lang.toml 里的版本探测命令（仅供参考）。
	Ver string
	// Version 是从版本缓存解析出的版本号；缺失时为空。
	Version string
}

// versionHeader 匹配 check_versions.sh 的分节标题：`==> 6 (pascal):`。
var versionHeader = regexp.MustCompile(`^==>\s*(\d+)\s*\(`)

// ParseVersionsTxt 解析 extra/check_versions.sh 的输出，返回 id -> 版本号。
//
// 每个语言以 `==> <id> (<name>):` 开头，其后第一条非空行视为版本号
// （编译器 banner 通常第一行就是版本；Perl 等会先输出空行，故跳过空行）。
// 解析不了的段落直接忽略，不报错——版本缓存缺失不该阻断整表生成。
func ParseVersionsTxt(r io.Reader) map[int]string {
	out := make(map[int]string)
	cur := -1
	sc := bufio.NewScanner(r)
	// 个别编译器 banner 很长，放开行缓冲上限。
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if m := versionHeader.FindStringSubmatch(line); m != nil {
			id, _ := parseInt(m[1])
			cur = id
			continue
		}
		if cur < 0 {
			continue
		}
		if _, done := out[cur]; done {
			// 该语言版本已取到，继续读到下一个分节。
			continue
		}
		if s := strings.TrimSpace(line); s != "" {
			out[cur] = s
		}
	}
	return out
}

// Build 从语言注册表与版本缓存构造语言信息表。
//
// 只包含"已安装"（存在 <id>.lang.toml）的语言，与提交页可选语言一致；
// 未配置 <id>.lang.toml 的语言不在本站受理范围内，不列出。
func Build(ojHome string, versions map[int]string) ([]Entry, error) {
	mgr, err := language.NewLanguageManager(ojHome)
	if err != nil {
		return nil, err
	}
	installed := mgr.Installed()
	entries := make([]Entry, 0, len(installed))
	for _, l := range installed {
		cfg, err := mgr.GetLanguageConfig(l.ID)
		if err != nil {
			return nil, fmt.Errorf("读取语言 %d 配置失败: %w", l.ID, err)
		}
		entries = append(entries, Entry{
			ID:          l.ID,
			DisplayName: l.DisplayName,
			Compile:     cfg.Cmd.Compile,
			Run:         cfg.Cmd.Run,
			Ver:         cfg.Cmd.Ver,
			Version:     versions[l.ID],
		})
	}
	return entries, nil
}

// RenderHTML 把语言信息表渲染成可嵌入 FAQ 页的 HTML 片段。
//
// 手工拼接而非 html/template：后者会把命令里的 `+` 也转义成 `&#43;`，
// 虽能正确显示但源码噪音大。这里对每个字段做 html.EscapeString
// （转义 & < > " '），足以防止配置内容破坏页面结构。
func RenderHTML(entries []Entry) string {
	var sb strings.Builder
	sb.WriteString(`<h2 class="ui header">评测</h2>` + "\n")
	sb.WriteString(`<p>本站当前支持以下语言，版本取自各语言的实际编译/运行环境：</p>` + "\n")
	sb.WriteString("<table class=\"ui celled compact table\">\n")
	sb.WriteString("<thead><tr><th>语言</th><th>版本</th><th>编译命令</th><th>运行命令</th></tr></thead>\n")
	sb.WriteString("<tbody>\n")
	for _, e := range entries {
		sb.WriteString("<tr>\n")
		sb.WriteString("<td>" + html.EscapeString(e.DisplayName) + "</td>\n")
		if e.Version != "" {
			sb.WriteString("<td>" + html.EscapeString(e.Version) + "</td>\n")
		} else {
			sb.WriteString(`<td><span class="grey-text">未知</span></td>` + "\n")
		}
		sb.WriteString("<td>" + codeCell(e.Compile) + "</td>\n")
		sb.WriteString("<td>" + codeCell(e.Run) + "</td>\n")
		sb.WriteString("</tr>\n")
	}
	sb.WriteString("</tbody>\n</table>\n")
	sb.WriteString(`<p>请使用<strong>标准输入输出</strong>；若题目要求文件读写（freopen），题面会另行注明输入/输出文件名。</p>` + "\n")
	return sb.String()
}

// codeCell 把命令包进 <code>；空命令渲染为空单元格。
func codeCell(cmd string) string {
	if cmd == "" {
		return ""
	}
	return "<code>" + html.EscapeString(cmd) + "</code>"
}

// parseInt 解析非负十进制整数；失败返回 (0,false)。
func parseInt(s string) (int, bool) {
	n := 0
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}
