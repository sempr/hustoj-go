package web

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain 在跑测试前装载仓库里的语言注册表（extra/etc/langs/all.toml）。
//
// 展示名/后缀/brush/已装语言现在全部来自注册表，测试若没有它，页面逻辑
// 会退化成"没有任何语言"。这里从仓库 extra 装载，等价于生产从 OJHome 装载。
func TestMain(m *testing.M) {
	home, err := filepath.Abs(filepath.Join("..", "..", "extra"))
	if err != nil {
		panic(err)
	}
	tbl, err := loadLangTable(home, "UnknownLanguage")
	if err != nil {
		panic("加载测试语言注册表失败: " + err.Error())
	}
	setLangTable(tbl)
	os.Exit(m.Run())
}

// 注册表视图必须与 all.toml 对齐：展示名切片长度为语言数+1（末位哨兵），
// 后缀/brush 与 id 一一对应。
func TestLangTableDerivedFromRegistry(t *testing.T) {
	tbl := currentLangs()
	if tbl == nil {
		t.Fatal("注册表未加载")
	}
	count := languageCount()
	if count == 0 {
		t.Fatal("语言数为 0")
	}
	if got := len(languageNamesAll()); got != count+1 {
		t.Errorf("展示名切片长度 = %d，期望语言数+1 = %d", got, count+1)
	}

	// 哨兵位于末位。
	if got := languageName(int32(count)); got != "UnknownLanguage" {
		t.Errorf("末位哨兵 = %q，期望 UnknownLanguage", got)
	}

	// 已知语言：id 1 = C++ / cc。
	if got := languageName(1); got != "C++" {
		t.Errorf("languageName(1) = %q，期望 C++", got)
	}
	if ext, ok := languageExtAt(1); !ok || ext != "cc" {
		t.Errorf("languageExtAt(1) = %q, %v，期望 cc", ext, ok)
	}

	// 越界。
	if _, ok := languageExtAt(int32(count)); ok {
		t.Error("languageExtAt(语言数) 应越界")
	}
}

// 已装语言集合与非零语言下拉一致：都来自注册表，且不含未安装语言。
func TestInstalledLanguageMapNonEmpty(t *testing.T) {
	m := installedLanguageMap()
	if len(m) == 0 {
		t.Fatal("已装语言集合为空：请检查 extra/etc/langs/*.lang.toml")
	}
	for id, name := range m {
		if name == "" {
			t.Errorf("已装语言 %d 的展示名为空", id)
		}
	}
}
