package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeJudgeConf 在临时目录下按 <home>/etc/judge.conf 的约定写一份配置。
func writeJudgeConf(t *testing.T, extra string) string {
	t.Helper()
	home := t.TempDir()
	etc := filepath.Join(home, "etc")
	if err := os.MkdirAll(etc, 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	body := strings.Join([]string{
		"OJ_HOST_NAME=127.0.0.1",
		"OJ_PORT_NUMBER=3306",
		"OJ_USER_NAME=hustoj",
		"OJ_PASSWORD=secret",
		"OJ_DB_NAME=jol",
	}, "\n") + "\n" + extra
	path := filepath.Join(etc, "judge.conf")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}
	return path
}

// OJ_LANG_SET 不再是代码默认值：缺失必须报错，避免静默受理错误语言集合。
func TestLoadDaemonConfigRequiresLangSet(t *testing.T) {
	path := writeJudgeConf(t, "")
	if _, err := LoadDaemonConfig(path); err == nil {
		t.Fatal("缺少 OJ_LANG_SET 时应报错")
	} else if !strings.Contains(err.Error(), "OJ_LANG_SET") {
		t.Errorf("错误信息应提到 OJ_LANG_SET，实际: %v", err)
	}
}

// 显式配置 OJ_LANG_SET 时按原样加载。
func TestLoadDaemonConfigReadsLangSet(t *testing.T) {
	path := writeJudgeConf(t, "OJ_LANG_SET=0,1,6\n")
	cfg, err := LoadDaemonConfig(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if cfg.LangSet != "0,1,6" {
		t.Errorf("LangSet = %q，期望 0,1,6", cfg.LangSet)
	}
}

// 并发数默认 3；OJ_MAX_RUNNING 覆盖默认值。
func TestLoadDaemonConfigMaxRunning(t *testing.T) {
	path := writeJudgeConf(t, "OJ_LANG_SET=0\n")
	cfg, err := LoadDaemonConfig(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if cfg.MaxRunning != 3 {
		t.Errorf("默认 MaxRunning = %d，期望 3", cfg.MaxRunning)
	}

	path = writeJudgeConf(t, "OJ_LANG_SET=0\nOJ_MAX_RUNNING=1\n")
	cfg, err = LoadDaemonConfig(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if cfg.MaxRunning != 1 {
		t.Errorf("MaxRunning = %d，期望 1", cfg.MaxRunning)
	}
}

// OJ_RUNNING 是旧键名，仍可用于兼容存量配置。
func TestLoadDaemonConfigLegacyRunning(t *testing.T) {
	path := writeJudgeConf(t, "OJ_LANG_SET=0\nOJ_RUNNING=2\n")
	cfg, err := LoadDaemonConfig(path)
	if err != nil {
		t.Fatalf("加载失败: %v", err)
	}
	if cfg.MaxRunning != 2 {
		t.Errorf("MaxRunning = %d，期望 2", cfg.MaxRunning)
	}
}

// 非法并发数必须报错，不能静默退化成 0 导致永远不派发任务。
func TestLoadDaemonConfigRejectsInvalidMaxRunning(t *testing.T) {
	for _, v := range []string{"0", "-1", "abc"} {
		path := writeJudgeConf(t, "OJ_LANG_SET=0\nOJ_MAX_RUNNING="+v+"\n")
		if _, err := LoadDaemonConfig(path); err == nil {
			t.Errorf("OJ_MAX_RUNNING=%q 应报错", v)
		}
	}
}
