package config

import (
	"bytes"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// 用真实的生产配置文件跑一遍解析。
//
// 这类断言没法用 Default() 替代：默认值永远合法，
// 线上真正会出事的是「手写的那份 TOML 里键名拼错、类型写错、缩进坏了」，
// 而这些只有拿真文件去解才能暴露。
//
// 刻意不调用 Load：Load 还会校验数据库凭据，那部分靠 systemd 的
// EnvironmentFile 注入，单测环境里没有，会把「TOML 本身写错了」
// 这类真实问题掩盖成一条 skip。
func TestProductionConfigFileParses(t *testing.T) {
	const path = "/home/judge/etc/web.toml"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("生产配置不可读（%v），跳过", err)
	}

	// 必须开严格模式：与 Load() 保持一致。
	// 少了这一句，配置文件里写错的键名会被静默忽略 ——
	// 线上真正会出事的就是"配了但没生效"，比如把 need_login 拼成
	// need_loggin，服务照常启动，只是该拦的页面没拦住。
	dec := toml.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		t.Fatalf("生产 web.toml 解析失败: %v", err)
	}

	t.Logf("oj_name=%q redis=%s ttl=%d cookie=%q vcode=%v register=%v",
		c.OJName, c.RedisURLRedacted(), c.GoSessionTTL, c.GoSessionCookie, c.VCode, c.Register)

	// 这几项配错都会静默地让站点坏掉，值得在这里钉死。
	if c.OJName == "" {
		t.Error("oj_name 为空：PHP 会话键前缀全部对不上，表现为全员匿名")
	}
	if c.RedisURL == "" {
		t.Error("redis_url 为空：Go 会话无处存放，全员掉登录")
	}
	if c.GoSessionCookie == "" {
		t.Error("go_session_cookie 为空")
	}
	if c.GoSessionCookie == "PHPSESSID" {
		t.Error("Go 会话 Cookie 不能与 PHPSESSID 同名，否则两套会话互相覆盖")
	}
	if c.GoSessionTTL <= 0 {
		t.Error("go_session_ttl 必须为正")
	}

	// Redis 连接串若含密码，绝不能同时出现在可读给 www-data 的配置里。
	if strings.Contains(c.RedisURL, "@") && c.RedisURLRedacted() == c.RedisURL {
		t.Error("redis_url 含凭据但脱敏无效，启动日志会泄露密码")
	}

	assertOJNameMatchesPHP(t, c.OJName)
}

// phpOJNameRe 匹配 HustOJ 定义 $OJ_NAME 的那行。
// 源码里是 `static $OJ_NAME="...";`，值通常用双引号。
var phpOJNameRe = regexp.MustCompile(`\$OJ_NAME\s*=\s*"([^"]*)"`)

// assertOJNameMatchesPHP 核对 web.toml 的 oj_name 与 PHP 源码是否一致。
//
// 这是整套切流里最隐蔽的一个坑。两边的会话键是 "<oj_name>_user_id"，
// 前缀对不上时 PHP 会话桥读不到任何键 —— 不报错、不告警，
// 表现只是「所有人都变成匿名」，而且只有老用户受影响（他们的登录态
// 只存在于 PHPSESSID 里），新用户却一切正常，很容易误判成「迁移没问题」。
//
// 之前手工构造 PHP 会话文件做冒烟测试时就踩过一次：键名写成 OJ_USERID，
// 而真实前缀是 <oj_name>_user_id，测试结论直接是错的。
func assertOJNameMatchesPHP(t *testing.T, got string) {
	t.Helper()

	const phpInc = "/home/judge/src/web/include/db_info.inc.php"
	raw, err := os.ReadFile(phpInc)
	if err != nil {
		t.Skipf("PHP 源码不可读（%v），跳过 oj_name 一致性核对", err)
	}
	m := phpOJNameRe.FindSubmatch(raw)
	if m == nil {
		t.Skipf("未在 %s 中找到 $OJ_NAME 定义，跳过", phpInc)
	}
	want := string(m[1])
	if got != want {
		t.Errorf("oj_name 与 PHP 源码不一致：web.toml=%q，PHP=%q\n"+
			"会话键前缀对不上，老用户会全部掉登录（表现为 PHP 会话桥永远解析不出 user_id）",
			got, want)
	}
}
