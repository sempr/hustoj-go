// 语言注册表的 Web 侧视图。
//
// 展示名、源文件后缀、语法高亮 brush、已装语言列表全部来自
// <OJHome>/etc/langs/all.toml（pkg/language），代码里不再硬编码任何
// 具体语言。启动时加载一次，之后只读。
package web

import (
	"fmt"
	"sync"

	"github.com/sempr/hustoj-go/pkg/language"
)

// langTable 是从注册表派生出来的、可直接按下标取值的只读视图。
type langTable struct {
	// names 下标即语言 id；末位是 UnknownLanguage 哨兵（id == len(exte)）。
	names []string
	// exts 下标即语言 id，值为不含点的源文件后缀。
	exts []string
	// brushes 下标即语言 id，值为 showsource 语法高亮 brush。
	brushes []string
	// headers 下标即语言 id，值为源码强制前缀（如 Python 的 coding 头）。
	headers []string
	// installed 是已安装语言（存在 <id>.lang.toml）的 id，升序。
	installed []int32
	// defaultID 是 all.toml 里 default=true 的语言 id；没有则为 -1。
	defaultID int32
}

// buildLangTable 把 Manager 摊平成按 id 下标访问的切片。
func buildLangTable(mgr *language.Manager, unknown string) *langTable {
	ordered := mgr.Ordered()
	n := len(ordered)

	t := &langTable{
		names:     make([]string, 0, n+1),
		exts:      make([]string, n),
		brushes:   make([]string, n),
		headers:   make([]string, n),
		defaultID: -1,
	}
	for i := range ordered {
		// 注册表已校验 id 从 0 连续，因此 ordered[i].ID == i。
		id := ordered[i].ID
		t.names = append(t.names, ordered[i].DisplayName)
		if ext, ok := mgr.Extension(id); ok {
			t.exts[i] = ext
		}
		if br, ok := mgr.Brush(id); ok {
			t.brushes[i] = br
		}
		if hdr, ok := mgr.SourceHeader(id); ok {
			t.headers[i] = hdr
		}
	}
	t.names = append(t.names, unknown)

	for _, l := range mgr.Installed() {
		t.installed = append(t.installed, int32(l.ID))
	}
	if id := mgr.DefaultLanguageID(); id >= 0 {
		t.defaultID = int32(id)
	}
	return t
}

// loadLangTable 从 OJHome 读取 etc/langs/all.toml 并构建视图。
func loadLangTable(ojHome, unknown string) (*langTable, error) {
	mgr, err := language.NewLanguageManager(ojHome)
	if err != nil {
		return nil, err
	}
	return buildLangTable(mgr, unknown), nil
}

var (
	langTableMu sync.RWMutex
	langs       *langTable
)

// setLangTable 安装进程级语言视图；由 New 或测试在启动时调用一次。
func setLangTable(t *langTable) {
	langTableMu.Lock()
	langs = t
	langTableMu.Unlock()
}

func currentLangs() *langTable {
	langTableMu.RLock()
	defer langTableMu.RUnlock()
	return langs
}

// languageCount 返回注册表里真实语言的个数（不含 UnknownLanguage 哨兵）。
func languageCount() int {
	t := currentLangs()
	if t == nil {
		return 0
	}
	return len(t.exts)
}

// languageNamesAll 返回含哨兵的展示名切片（长度 = 语言数 + 1）。
//
// 统计页与 showsource 用下标直接索引：越界时按下标判定即可，无需再查表。
func languageNamesAll() []string {
	t := currentLangs()
	if t == nil {
		return nil
	}
	out := make([]string, len(t.names))
	copy(out, t.names)
	return out
}

// languageName 返回语言展示名；越界返回 "未知(id)"。
func languageName(id int32) string {
	t := currentLangs()
	if t != nil && id >= 0 && int(id) < len(t.names) {
		return t.names[id]
	}
	return fmt.Sprintf("未知(%d)", id)
}

// languageExtAt 返回语言源文件后缀（不含点）；越界返回 ok=false。
func languageExtAt(id int32) (string, bool) {
	t := currentLangs()
	if t == nil || id < 0 || int(id) >= len(t.exts) {
		return "", false
	}
	return t.exts[id], true
}

// languageBrushAt 返回语法高亮 brush；越界返回 ok=false。
func languageBrushAt(id int32) (string, bool) {
	t := currentLangs()
	if t == nil || id < 0 || int(id) >= len(t.brushes) {
		return "", false
	}
	return t.brushes[id], true
}

// languageSourceHeader 返回语言强制追加的源码头；未配置返回空串。
func languageSourceHeader(id int32) string {
	t := currentLangs()
	if t == nil || id < 0 || int(id) >= len(t.headers) {
		return ""
	}
	return t.headers[id]
}

// installedLanguageMap 返回状态页下拉用的 语言 id -> 展示名。
func installedLanguageMap() map[int32]string {
	t := currentLangs()
	m := make(map[int32]string)
	if t == nil {
		return m
	}
	for _, id := range t.installed {
		m[id] = languageName(id)
	}
	return m
}

// maxStatusLanguageID 是 status 筛选允许的语言编号上界。
//
// 等于真实语言数 N：展示名切片长度为 N+1（含哨兵），合法下标 0..N。
func maxStatusLanguageID() int32 { return int32(languageCount()) }

// defaultLanguageID 返回 all.toml 配置的默认语言 id；未配置返回 -1。
//
// 用于提交页在用户没有历史语言时选中它，避免把"默认语言"写死在代码里。
func defaultLanguageID() int32 {
	t := currentLangs()
	if t == nil {
		return -1
	}
	return t.defaultID
}
