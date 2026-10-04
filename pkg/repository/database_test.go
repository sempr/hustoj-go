// pkg/repository 的 DSN 契约测试。
//
// 这些断言看似琐碎，但每一条都对应一次"改坏了线上会怎样"：
// collation 与表定义不一致会让 ORDER BY 的顺序和 PHP 侧分叉，
// loc 改错会让所有时间显示整体偏移，charset 漏掉则
// 4 字节 emoji（题目描述里很常见）会偶发乱码。
package repository

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/sempr/hustoj-go/pkg/config"
)

func testConfig() config.DatabaseConfig {
	return config.DatabaseConfig{
		Host: "127.0.0.1", Port: 3306,
		User: "hustoj", Password: "s3cret", Name: "jol",
	}
}

func testDSN() string {
	return DSN(testConfig())
}

// parseDSN 用驱动自己的解析器校验 DSN 语法，
// 避免用正则"看起来对"却实际连不上。
func parseDSN(t *testing.T, dsn string) *mysql.Config {
	t.Helper()
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("DSN 无法被驱动解析（真实连接也会失败）: %v\nDSN: %s", err, dsn)
	}
	return cfg
}

func TestDSNParams(t *testing.T) {
	dsn := testDSN()
	c := parseDSN(t, dsn)
	// 驱动把 charset 存进非导出字段，只能从 DSN 原文断言。
	if !strings.Contains(dsn, "charset=utf8mb4") {
		t.Errorf("DSN 缺少 charset=utf8mb4：4 字节 emoji（题目描述里很常见）会偶发乱码")
	}
	if c.Collation != "utf8mb4_general_ci" {
		t.Errorf("collation = %q，期望 utf8mb4_general_ci（须与表定义及 PHP 侧一致）", c.Collation)
	}
	if !c.ParseTime {
		t.Error("parseTime 应为 true，否则 datetime 列读不出 time.Time")
	}
	// loc=Local 的含义是"跟随进程时区"，故断言的是它解析出的偏移与 time.Local
	// 一致，而不是字面量 "Local"（TZ 有值时驱动会解析成具体时区名）。
	if !strings.Contains(dsn, "loc=Local") {
		t.Errorf("DSN 缺少 loc=Local：会用驱动默认 UTC，让时间整体偏移。DSN: %s", dsn)
	}
	gotOffset := time.Now().In(c.Loc).Format("-07:00")
	if wantOffset := time.Now().In(time.Local).Format("-07:00"); gotOffset != wantOffset {
		t.Errorf("loc 解析为 %q（偏移 %s），期望与进程时区一致（%s）", c.Loc, gotOffset, wantOffset)
	}
	// time_zone 对应 PHP 的 SET time_zone='+8:00'（init.php:148）。漏掉它，
	// 连接池的 NOW()/CURDATE() 按服务器默认时区（UTC）算，与 PHP 差 8 小时。
	// 取值不在代码里写死，应等于当前解析出的会话时区。
	if got, want := c.Params["time_zone"], "'"+sessionTimeZone()+"'"; got != want {
		t.Errorf("time_zone = %q，期望 %q（须与进程 TZ 一致，等于 PHP 的 SET time_zone）", got, want)
	}
}

// TestDSNTimeZoneFollowsEnvironment 验证会话时区来自环境而非代码常量：
// 设置 HUSTOJ_DB_TIME_ZONE 后 DSN 必须下发该值，未设置时回落到进程本地时区。
func TestDSNTimeZoneFollowsEnvironment(t *testing.T) {
	t.Setenv(dbTimeZoneEnv, "Asia/Shanghai")
	if got := parseDSN(t, testDSN()).Params["time_zone"]; got != "'Asia/Shanghai'" {
		t.Errorf("环境变量未生效：time_zone = %q", got)
	}
	t.Setenv(dbTimeZoneEnv, "-05:00")
	if got := parseDSN(t, testDSN()).Params["time_zone"]; got != "'-05:00'" {
		t.Errorf("环境变量给的偏移未保真：time_zone = %q", got)
	}
	t.Setenv(dbTimeZoneEnv, "")
	// 未显式指定：取进程本地时区的当前偏移，不含任何硬编码时区名。
	if got := parseDSN(t, testDSN()).Params["time_zone"]; !regexp.MustCompile(`^'[+-]\d{2}:\d{2}'$`).MatchString(got) {
		t.Errorf("回落到进程本地时区时格式异常：time_zone = %q", got)
	}
}

// TestDSNQuotesSessionTimeZone 覆盖引号归一化：时区名与偏移都要包进单引号，
// 否则驱动发出的 SET time_zone=Asia/Shanghai 会被当成列名而报错。
func TestDSNQuotesSessionTimeZone(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"+08:00", "'+08:00'"},
		{"-05:00", "'-05:00'"},
		{"Asia/Shanghai", "'Asia/Shanghai'"},
		{"UTC", "'UTC'"},
		{" 'Asia/Shanghai' ", "'Asia/Shanghai'"},
	} {
		if got := parseDSN(t, dsn(testConfig(), tc.in)).Params["time_zone"]; got != tc.want {
			t.Errorf("dsn(%q) 的 time_zone = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

// collation 不能留空：留空会回落到 MariaDB 服务器默认值
// （本机是 utf8mb4_uca1400_ai_ci），与表定义的 general_ci 不一致，
// 导致两侧 ORDER BY 输出顺序不同。
func TestDSNDoesNotInheritServerDefaultCollation(t *testing.T) {
	c := parseDSN(t, testDSN())
	if c.Collation == "" {
		t.Fatal("collation 为空：DSN 未显式指定，将回落到服务器默认值，不可接受")
	}
	if strings.Contains(c.Collation, "uca1400") {
		t.Errorf("collation = %q：不得依赖 MariaDB 服务器默认值", c.Collation)
	}
}

func TestDSNUsesConfiguredAddress(t *testing.T) {
	c := parseDSN(t, DSN(config.DatabaseConfig{
		Host: "10.0.0.5", Port: 3307, User: "u", Password: "p", Name: "db",
	}))
	if c.Addr != "10.0.0.5:3307" {
		t.Errorf("Addr = %q，期望 10.0.0.5:3307", c.Addr)
	}
	if c.DBName != "db" {
		t.Errorf("DBName = %q", c.DBName)
	}
}

// 口令只能出现在 userinfo 段；参数区重复携带会让 DSN 更容易出现在日志里。
func TestDSNDoesNotLeakPasswordInParams(t *testing.T) {
	c := parseDSN(t, testDSN())
	for k := range c.Params {
		if strings.EqualFold(k, "password") || strings.EqualFold(k, "passwd") {
			t.Errorf("参数区不应含口令: %s", k)
		}
	}
}
