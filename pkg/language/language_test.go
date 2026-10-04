package language

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// langsDir 返回仓库内 extra/etc/langs 的路径。
func langsDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "extra", "etc", "langs"))
	if err != nil {
		t.Fatalf("解析 langs 目录失败: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("langs 目录不存在，跳过: %v", err)
	}
	return dir
}

// TestAllLangTOMLParsable 确保每个 <id>.lang.toml 都能被 go-toml/v2 解析。
//
// 回归背景：提交 b4d76bb "update lang.toml, add missing env" 对已含 env 键的
// 3.lang.toml 与 25.lang.toml 又追加了一行，TOML 规范禁止重复键，
// go-toml/v2 会直接报错。GetLanguageConfig 在判题时才解析该文件，
// 因此缺陷只在提交 Rust/Java 时才暴露，且 extra/replace.sh 的 sed 替换
// 无法自愈（两行都会被替换成相同内容，重复依旧）。
func TestAllLangTOMLParsable(t *testing.T) {
	dir := langsDir(t)

	entries, err := filepath.Glob(filepath.Join(dir, "*.lang.toml"))
	if err != nil {
		t.Fatalf("glob 失败: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("未找到任何 *.lang.toml")
	}

	for _, path := range entries {
		base := filepath.Base(path)
		t.Run(base, func(t *testing.T) {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("读取失败: %v", err)
			}
			var cfg LangConfig
			if err := toml.Unmarshal(data, &cfg); err != nil {
				t.Fatalf("TOML 解析失败（疑似重复键）: %v", err)
			}
			if strings.TrimSpace(cfg.Name) == "" {
				t.Error("name 为空")
			}
			if strings.TrimSpace(cfg.Cmd.Run) == "" {
				t.Error("cmd.run 为空")
			}
		})
	}
}

// TestAllTOMLIDsContiguous 确保 all.toml 的 id 从 0 开始连续且无重复。
// 语言索引是 phpweb/include/const.inc.php 与 judge.conf 的 OJ_LANG_SET
// 共同依赖的契约，索引空洞或重复会直接导致判题取到错误的语言配置。
func TestAllTOMLIDsContiguous(t *testing.T) {
	dir := langsDir(t)

	data, err := os.ReadFile(filepath.Join(dir, "all.toml"))
	if err != nil {
		t.Fatalf("读取 all.toml 失败: %v", err)
	}
	var parsed struct {
		Lang []LangBasic `toml:"lang"`
	}
	if err := toml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("解析 all.toml 失败: %v", err)
	}
	if len(parsed.Lang) == 0 {
		t.Fatal("all.toml 未声明任何语言")
	}

	seen := make(map[int]LangBasic, len(parsed.Lang))
	for _, l := range parsed.Lang {
		if prev, dup := seen[l.ID]; dup {
			t.Errorf("id %d 重复: %q 与 %q", l.ID, prev.Name, l.Name)
		}
		if strings.TrimSpace(l.Name) == "" {
			t.Errorf("id %d 的 name 为空", l.ID)
		}
		if strings.TrimSpace(l.Suffix) == "" {
			t.Errorf("id %d (%s) 的 suffix 为空", l.ID, l.Name)
		}
		seen[l.ID] = l
	}
	for i := 0; i < len(parsed.Lang); i++ {
		if _, ok := seen[i]; !ok {
			t.Errorf("id %d 缺失（索引必须从 0 连续，已覆盖 0-%d）", i, len(parsed.Lang)-1)
		}
	}
}

// TestLangTOMLIDDeclaredInAllTOML 确保每个存在的 <id>.lang.toml 都在 all.toml 中登记。
//
// 注意方向性：all.toml 是【完整注册表】（26 种，含仓库无法构建的 Ruby/Obj-C 等），
// 而 <id>.lang.toml 只为 extra/ 下存在 Dockerfile 的语言提供（当前 12 种）。
// 这与 phpweb/include/const.inc.php 列出 27 种、而 $OJ_LANGMASK 再禁用掉无
// judge 配置的那 13 种是同一套设计，因此不能断言注册表与配置文件一一对应。
func TestLangTOMLIDDeclaredInAllTOML(t *testing.T) {
	dir := langsDir(t)

	data, err := os.ReadFile(filepath.Join(dir, "all.toml"))
	if err != nil {
		t.Fatalf("读取 all.toml 失败: %v", err)
	}
	var parsed struct {
		Lang []LangBasic `toml:"lang"`
	}
	if err := toml.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("解析 all.toml 失败: %v", err)
	}
	declared := make(map[int]string, len(parsed.Lang))
	for _, l := range parsed.Lang {
		declared[l.ID] = l.Name
	}

	entries, err := filepath.Glob(filepath.Join(dir, "*.lang.toml"))
	if err != nil {
		t.Fatalf("glob 失败: %v", err)
	}
	for _, path := range entries {
		base := filepath.Base(path)
		id, err := strconv.Atoi(strings.TrimSuffix(base, ".lang.toml"))
		if err != nil {
			t.Errorf("文件名 %s 不是 <id>.lang.toml 形式", base)
			continue
		}
		if _, ok := declared[id]; !ok {
			t.Errorf("%s 的 id %d 未在 all.toml 中登记", base, id)
		}
	}
}
