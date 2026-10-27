// Package config 的加载行为测试。
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	ojconfig "github.com/sempr/hustoj-go/pkg/config"
)

// writeTOML 在临时目录写一份 web.toml，返回路径。
func writeTOML(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "web.toml")
	if err := os.WriteFile(p, []byte(body), 0o640); err != nil {
		t.Fatalf("写 web.toml 失败: %v", err)
	}
	return p
}

// withEnv 临时设置环境变量，测试结束自动清理。
func withEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		t.Setenv(k, v)
	}
}

// dbEnv 给出一组合法的 judge.conf 等价环境变量。
func dbEnv() map[string]string {
	return map[string]string{
		"OJ_HOST_NAME":   "127.0.0.1",
		"OJ_PORT_NUMBER": "3306",
		"OJ_USER_NAME":   "hustoj",
		"OJ_PASSWORD":    "s3cret",
		"OJ_DB_NAME":     "jol",
	}
}

func TestLoadDefaults(t *testing.T) {
	withEnv(t, dbEnv())
	cfg, err := Load(t.TempDir(), filepath.Join(t.TempDir(), "缺失.toml"))
	if err != nil {
		t.Fatalf("web.toml 缺失不应报错: %v", err)
	}
	if cfg.Listen != "127.0.0.1:8080" {
		t.Errorf("Listen = %q，期望默认 127.0.0.1:8080", cfg.Listen)
	}
	if cfg.Template != "syzoj" {
		t.Errorf("Template = %q，期望默认 syzoj", cfg.Template)
	}
	if cfg.Database.Password != "s3cret" {
		t.Errorf("Password 未从环境读取")
	}
}

func TestTOMLOverridesDefaults(t *testing.T) {
	withEnv(t, dbEnv())
	p := writeTOML(t, `
listen = "127.0.0.1:9000"
template = "bs3"
debug = true
`)
	cfg, err := Load("/home/judge", p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:9000" {
		t.Errorf("Listen = %q", cfg.Listen)
	}
	if cfg.Template != "bs3" {
		t.Errorf("Template = %q", cfg.Template)
	}
	if !cfg.Debug {
		t.Error("debug 未从 web.toml 生效")
	}
}

func TestEnvOverridesTOML(t *testing.T) {
	env := dbEnv()
	env["HUSTOJ_WEB_LISTEN"] = "127.0.0.1:7777"
	env["OJ_TEMPLATE"] = "mdui"
	env["HUSTOJ_WEB_DEBUG"] = "false"
	withEnv(t, env)

	p := writeTOML(t, `
listen = "127.0.0.1:9000"
template = "bs3"
debug = true
`)
	cfg, err := Load("/home/judge", p)
	if err != nil {
		t.Fatal(err)
	}
	// 环境变量优先级高于 web.toml
	if cfg.Listen != "127.0.0.1:7777" {
		t.Errorf("Listen = %q，期望环境变量 127.0.0.1:7777 覆盖 web.toml", cfg.Listen)
	}
	if cfg.Template != "mdui" {
		t.Errorf("Template = %q，期望环境变量 mdui 覆盖 web.toml", cfg.Template)
	}
	if cfg.Debug {
		t.Error("HUSTOJ_WEB_DEBUG=false 应覆盖 web.toml 的 true")
	}
}

func TestEmptyEnvDoesNotOverride(t *testing.T) {
	env := dbEnv()
	env["HUSTOJ_WEB_LISTEN"] = "   " // 空白视为未设置
	withEnv(t, env)

	p := writeTOML(t, `listen = "127.0.0.1:9000"`)
	cfg, err := Load("/home/judge", p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != "127.0.0.1:9000" {
		t.Errorf("空环境变量不应覆盖 web.toml，实际 %q", cfg.Listen)
	}
}

func TestUnknownTOMLKeyRejected(t *testing.T) {
	withEnv(t, dbEnv())
	// 严格解码：拼错的键必须报错，而不是被静默忽略
	p := writeTOML(t, `listne = "127.0.0.1:9000"`)
	if _, err := Load("/home/judge", p); err == nil {
		t.Error("web.toml 中未知键 listne 应报错")
	}
}

func TestMalformedTOMLRejected(t *testing.T) {
	withEnv(t, dbEnv())
	p := writeTOML(t, `listen = `)
	if _, err := Load("/home/judge", p); err == nil {
		t.Error("语法错误的 web.toml 应报错")
	}
}

func TestValidateRejectsIncomplete(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
	}{
		{"缺数据库主机", map[string]string{"OJ_PORT_NUMBER": "3306", "OJ_DB_NAME": "jol"}},
		{"缺数据库名", map[string]string{"OJ_HOST_NAME": "127.0.0.1", "OJ_PORT_NUMBER": "3306"}},
		{"端口非法", map[string]string{"OJ_HOST_NAME": "127.0.0.1", "OJ_PORT_NUMBER": "abc", "OJ_DB_NAME": "jol"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withEnv(t, tc.env)
			if _, err := Load("/home/judge", filepath.Join(t.TempDir(), "无.toml")); err == nil {
				t.Error("期望报错")
			}
		})
	}
}

func TestRedactedHidesPassword(t *testing.T) {
	d := ojconfig.DatabaseConfig{Host: "127.0.0.1", Port: 3306, User: "hustoj", Password: "s3cret", Name: "jol"}
	got := d.Redacted()
	if contains(got, "s3cret") {
		t.Errorf("Redacted() 泄漏了口令: %s", got)
	}
	if !contains(got, "hustoj@127.0.0.1:3306/jol") {
		t.Errorf("Redacted() 应保留定位信息，实际 %s", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// 启动日志会输出 Redis 连接串用于排障，但不能带上密码。
func TestRedisURLRedactedHidesPassword(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"无密码", "redis://127.0.0.1:6379/0", "redis://127.0.0.1:6379/0"},
		{"有密码", "redis://user:s3cr3t@127.0.0.1:6379/0", "redis://user:***@127.0.0.1:6379/0"},
		{"只有用户名", "redis://user@127.0.0.1:6379/0", "redis://user:***@127.0.0.1:6379/0"},
		{"空", "", "(空)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{RedisURL: tc.in}
			got := c.RedisURLRedacted()
			if got != tc.want {
				t.Errorf("= %q, want %q", got, tc.want)
			}
			if tc.in != "" && strings.Contains(tc.in, "s3cr3t") && strings.Contains(got, "s3cr3t") {
				t.Error("脱敏后仍含明文密码")
			}
		})
	}
}
