package language

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// LangBasic represents basic language information.
//
// 这些字段来自 etc/langs/all.toml —— 它是判题与 Web 共用的唯一语言注册表。
// DisplayName / Brush 只影响 Web 展示，判题逻辑只用到 ID 与 Suffix。
type LangBasic struct {
	Name        string `toml:"name"`
	DisplayName string `toml:"display_name"`
	ID          int    `toml:"id"`
	Suffix      string `toml:"suffix"`
	Brush       string `toml:"brush"`
	// SourceHeader 是提交源码前强制追加的头（如 Python 的
	// "# coding=utf-8"）。非空时在拼接 prepend/append 模板之前加进源码。
	SourceHeader string `toml:"source_header"`
	// Default 标记提交页在用户没有历史语言时使用的默认语言；
	// 全表至多一项为 true（loadLanguageMap 会校验）。
	Default bool `toml:"default"`
}

// LangConfig represents complete language configuration
type LangConfig struct {
	Name string  `toml:"name"`
	Fs   FsInfo  `toml:"fs"`
	Cmd  CmdInfo `toml:"cmd"`
}

// FsInfo represents filesystem information for a language
type FsInfo struct {
	Base    string `toml:"base"`
	Workdir string `toml:"workdir"`
}

// CmdInfo represents command information for a language
type CmdInfo struct {
	Compile string   `toml:"compile"`
	Run     string   `toml:"run"`
	Ver     string   `toml:"ver"`
	Env     []string `toml:"env"`
}

// Manager manages language configurations
type Manager struct {
	// ordered 按 id 升序保存注册表全部条目；校验保证 id 从 0 连续，
	// 因此 ordered[i].ID == i，可作为下标直接使用。
	ordered []LangBasic
	langMap map[int]LangBasic
	// defaultID 是 all.toml 中标记 default=true 的语言 id；没有则为 -1。
	defaultID int
	homeDir   string
}

// NewLanguageManager creates a new language manager
func NewLanguageManager(homeDir string) (*Manager, error) {
	manager := &Manager{
		homeDir:   homeDir,
		langMap:   make(map[int]LangBasic),
		defaultID: -1,
	}

	if err := manager.loadLanguageMap(); err != nil {
		return nil, fmt.Errorf("failed to load language map: %w", err)
	}

	return manager, nil
}

// loadLanguageMap loads the basic language mapping from all.toml.
//
// 严格校验：id 必须从 0 连续且唯一，name/display_name/suffix 均非空。
// 这些是 solution.language、$OJ_LANGMASK、prepend/append 后缀共同依赖的
// 隐式契约，缺项或空洞会让判题取到错误的语言配置，故宁可启动失败。
func (m *Manager) loadLanguageMap() error {
	path := filepath.Join(m.homeDir, "etc", "langs", "all.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	var config struct {
		Lang []LangBasic `toml:"lang"`
	}

	if err := toml.Unmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse %s: %w", path, err)
	}
	if len(config.Lang) == 0 {
		return fmt.Errorf("%s 未声明任何语言", path)
	}

	for _, lang := range config.Lang {
		if strings.TrimSpace(lang.Name) == "" {
			return fmt.Errorf("%s 中 id=%d 的 name 为空", path, lang.ID)
		}
		if strings.TrimSpace(lang.DisplayName) == "" {
			return fmt.Errorf("%s 中 id=%d (%s) 缺少 display_name", path, lang.ID, lang.Name)
		}
		if strings.TrimSpace(lang.Suffix) == "" {
			return fmt.Errorf("%s 中 id=%d (%s) 的 suffix 为空", path, lang.ID, lang.Name)
		}
		if _, dup := m.langMap[lang.ID]; dup {
			return fmt.Errorf("%s 中 id=%d 重复", path, lang.ID)
		}
		if lang.Default {
			if m.defaultID >= 0 {
				return fmt.Errorf("%s 中 default=true 出现多次（id=%d 与 id=%d）", path, m.defaultID, lang.ID)
			}
			m.defaultID = lang.ID
		}
		m.langMap[lang.ID] = lang
	}

	m.ordered = make([]LangBasic, len(config.Lang))
	for i := range config.Lang {
		lang, ok := m.langMap[i]
		if !ok {
			return fmt.Errorf("%s 的 id 必须从 0 连续，缺少 id=%d", path, i)
		}
		m.ordered[i] = lang
	}

	return nil
}

// Ordered returns all languages sorted by id.
func (m *Manager) Ordered() []LangBasic {
	out := make([]LangBasic, len(m.ordered))
	copy(out, m.ordered)
	return out
}

// Count returns the number of registered languages.
func (m *Manager) Count() int { return len(m.ordered) }

// GetLanguageBasic returns basic language information
func (m *Manager) GetLanguageBasic(langID int) (LangBasic, error) {
	lang, ok := m.langMap[langID]
	if !ok {
		return LangBasic{}, fmt.Errorf("unknown language ID: %d", langID)
	}
	return lang, nil
}

// Extension 返回语言下标对应的源文件后缀（不含点）。
//
// 用途：题目目录 prepend.<ext> / append.<ext> 模板文件名，以及判题侧
// Main<suffix> 的文件名基。越界返回 ("", false)。
func (m *Manager) Extension(langID int) (string, bool) {
	lang, ok := m.langMap[langID]
	if !ok {
		return "", false
	}
	return strings.TrimPrefix(lang.Suffix, "."), true
}

// DisplayName 返回 Web 展示名；越界返回 ("", false)。
func (m *Manager) DisplayName(langID int) (string, bool) {
	lang, ok := m.langMap[langID]
	if !ok {
		return "", false
	}
	return lang.DisplayName, true
}

// Brush 返回 showsource 语法高亮 brush 名。
//
// 显式配置了 brush 就用它，否则回退到 display_name 小写 —— 这只是一条通用
// 规则，不针对任何具体语言，因此新增语言无需改代码。
func (m *Manager) Brush(langID int) (string, bool) {
	lang, ok := m.langMap[langID]
	if !ok {
		return "", false
	}
	if strings.TrimSpace(lang.Brush) != "" {
		return lang.Brush, true
	}
	return strings.ToLower(lang.DisplayName), true
}

// SourceHeader 返回语言需要强制追加的源码头（如 Python 的
// "# coding=utf-8"）；未配置返回 ("", false)。
func (m *Manager) SourceHeader(langID int) (string, bool) {
	lang, ok := m.langMap[langID]
	if !ok || strings.TrimSpace(lang.SourceHeader) == "" {
		return "", false
	}
	return lang.SourceHeader, true
}

// DefaultLanguageID 返回 all.toml 里 default=true 的语言 id；未配置返回 -1。
//
// 用于提交页在用户没有历史语言时的默认选项，避免把"默认语言"写死在代码里。
func (m *Manager) DefaultLanguageID() int { return m.defaultID }

// Installed 返回本机真正配置了编译器的语言（存在 <id>.lang.toml）。
//
// 状态页/提交页只应列出这些语言：列出一个没有判题配置的语言，用户选中后
// 必然 OJ_SE。是否安装由配置文件存在与否决定，不在代码里维护白名单。
func (m *Manager) Installed() []LangBasic {
	var out []LangBasic
	for _, lang := range m.ordered {
		if m.hasLangConfig(lang.ID) {
			out = append(out, lang)
		}
	}
	return out
}

// IsInstalled 判断某语言是否已安装（存在 <id>.lang.toml）。
func (m *Manager) IsInstalled(langID int) bool { return m.hasLangConfig(langID) }

func (m *Manager) hasLangConfig(langID int) bool {
	path := filepath.Join(m.homeDir, "etc", "langs", fmt.Sprintf("%d.lang.toml", langID))
	_, err := os.Stat(path)
	return err == nil
}

// GetAllLanguages returns all available languages
func (m *Manager) GetAllLanguages() map[int]LangBasic {
	result := make(map[int]LangBasic, len(m.langMap))
	for k, v := range m.langMap {
		result[k] = v
	}
	return result
}

// SortedIDs 返回升序的语言 id 列表，供需要稳定性时使用。
func (m *Manager) SortedIDs() []int {
	ids := make([]int, 0, len(m.langMap))
	for id := range m.langMap {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

// GetLanguageConfig returns complete language configuration
func (m *Manager) GetLanguageConfig(langID int) (*LangConfig, error) {
	langPath := filepath.Join(m.homeDir, "etc", "langs", fmt.Sprintf("%d.lang.toml", langID))

	data, err := os.ReadFile(langPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read language config: %w", err)
	}

	var config LangConfig
	if err := toml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse language config: %w", err)
	}

	return &config, nil
}
