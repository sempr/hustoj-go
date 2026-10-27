// pkg/config 的配置加载测试。
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConf 在 <home>/etc/judge.conf 写入给定内容，返回 home 路径。
func writeConf(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	etc := filepath.Join(home, "etc")
	if err := os.MkdirAll(etc, 0o755); err != nil {
		t.Fatalf("建 etc 失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(etc, "judge.conf"), []byte(body), 0o600); err != nil {
		t.Fatalf("写 judge.conf 失败: %v", err)
	}
	return home
}

const goodConf = `OJ_HOST_NAME=127.0.0.1
OJ_PORT_NUMBER=3306
OJ_USER_NAME=hustoj
OJ_PASSWORD=s3cret
OJ_DB_NAME=jol
`

func TestLoadJudgeConfOK(t *testing.T) {
	cfg, err := LoadJudgeConf(writeConf(t, goodConf))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Host != "127.0.0.1" || cfg.Database.Port != 3306 {
		t.Errorf("连接地址错误: %+v", cfg.Database.Redacted())
	}
	if cfg.Database.User != "hustoj" || cfg.Database.Name != "jol" {
		t.Errorf("账号/库名错误: %+v", cfg.Database.Redacted())
	}
	if cfg.Database.Password != "s3cret" {
		t.Error("口令未正确读取")
	}
}

func TestLoadJudgeConfIgnoresComments(t *testing.T) {
	body := "# 注释行\n\n" + goodConf + "OJ_LANG_SET=java,all\n"
	cfg, err := LoadJudgeConf(writeConf(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Name != "jol" {
		t.Errorf("Name = %q", cfg.Database.Name)
	}
}

// 核心回归：judge.conf 缺失时必须报错，而不是回退到 root/password/hustoj。
// 回退一旦发生，判题进程可能连上错误的库取件并回写判罚。
func TestMissingFileIsFatal(t *testing.T) {
	_, err := LoadJudgeConf(t.TempDir())
	if err == nil {
		t.Fatal("judge.conf 缺失时应报错")
	}
	if !strings.Contains(err.Error(), "judge.conf") {
		t.Errorf("错误信息应指出配置文件路径，实际: %v", err)
	}
	// 错误信息不得泄漏口令
	if strings.Contains(err.Error(), "password") && !strings.Contains(err.Error(), "确认") {
		t.Errorf("错误信息措辞异常: %v", err)
	}
}

func TestMissingRequiredKeysAreFatal(t *testing.T) {
	cases := []struct {
		name string
		conf string
		want string
	}{
		{"缺账号", "OJ_HOST_NAME=127.0.0.1\nOJ_PORT_NUMBER=3306\nOJ_PASSWORD=x\nOJ_DB_NAME=jol\n", "OJ_USER_NAME"},
		{"缺库名", "OJ_HOST_NAME=127.0.0.1\nOJ_PORT_NUMBER=3306\nOJ_USER_NAME=hustoj\nOJ_PASSWORD=x\n", "OJ_DB_NAME"},
		{"缺主机", "OJ_PORT_NUMBER=3306\nOJ_USER_NAME=hustoj\nOJ_PASSWORD=x\nOJ_DB_NAME=jol\n", "OJ_HOST_NAME"},
		{"缺端口", "OJ_HOST_NAME=127.0.0.1\nOJ_USER_NAME=hustoj\nOJ_PASSWORD=x\nOJ_DB_NAME=jol\n", "OJ_PORT_NUMBER"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadJudgeConf(writeConf(t, tc.conf))
			if err == nil {
				t.Fatalf("%s 时应报错", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("错误信息应点名 %s，实际: %v", tc.want, err)
			}
		})
	}
}

// 空口令合法：MariaDB 可能走本地 socket 或 trust 认证。
func TestEmptyPasswordAllowed(t *testing.T) {
	conf := "OJ_HOST_NAME=127.0.0.1\nOJ_PORT_NUMBER=3306\nOJ_USER_NAME=hustoj\nOJ_PASSWORD=\nOJ_DB_NAME=jol\n"
	cfg, err := LoadJudgeConf(writeConf(t, conf))
	if err != nil {
		t.Fatalf("空口令应被接受: %v", err)
	}
	if cfg.Database.Password != "" {
		t.Error("口令应为空")
	}
}

// 旧实现在此静默沿用默认端口 3306。
func TestNonNumericPortIsFatal(t *testing.T) {
	conf := "OJ_HOST_NAME=127.0.0.1\nOJ_PORT_NUMBER=33o6\nOJ_USER_NAME=hustoj\nOJ_PASSWORD=x\nOJ_DB_NAME=jol\n"
	_, err := LoadJudgeConf(writeConf(t, conf))
	if err == nil {
		t.Fatal("非数字端口应报错")
	}
	if !strings.Contains(err.Error(), "OJ_PORT_NUMBER") {
		t.Errorf("错误信息: %v", err)
	}
}

func TestOutOfRangePortIsFatal(t *testing.T) {
	for _, p := range []string{"0", "70000", "-1"} {
		conf := "OJ_HOST_NAME=127.0.0.1\nOJ_PORT_NUMBER=" + p + "\nOJ_USER_NAME=hustoj\nOJ_PASSWORD=x\nOJ_DB_NAME=jol\n"
		if _, err := LoadJudgeConf(writeConf(t, conf)); err == nil {
			t.Errorf("端口 %s 应报错", p)
		}
	}
}

// 真实 judge.conf 的口令常含 = 与 #，解析不能把它们当分隔符或注释。
func TestPasswordWithEqualsAndHash(t *testing.T) {
	conf := "OJ_HOST_NAME=127.0.0.1\nOJ_PORT_NUMBER=3306\nOJ_USER_NAME=hustoj\nOJ_PASSWORD=a=b#c\nOJ_DB_NAME=jol\n"
	cfg, err := LoadJudgeConf(writeConf(t, conf))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.Password != "a=b#c" {
		t.Errorf("口令 = %q，期望 a=b#c", cfg.Database.Password)
	}
}

func TestRedactedNeverLeaksPassword(t *testing.T) {
	d := DatabaseConfig{Host: "10.0.0.1", Port: 3306, User: "hustoj", Password: "topsecret", Name: "jol"}
	got := d.Redacted()
	for _, leak := range []string{"topsecret"} {
		if strings.Contains(got, leak) {
			t.Errorf("Redacted() 泄漏口令: %s", got)
		}
	}
	if !strings.Contains(got, "hustoj@10.0.0.1:3306/jol") {
		t.Errorf("Redacted() 应保留定位信息: %s", got)
	}
}

// 写了键但留空，等同没配，必须报错。
func TestEmptyValueIsFatal(t *testing.T) {
	for _, k := range []string{"OJ_HOST_NAME", "OJ_USER_NAME", "OJ_DB_NAME"} {
		conf := "OJ_HOST_NAME=127.0.0.1\nOJ_PORT_NUMBER=3306\nOJ_USER_NAME=hustoj\nOJ_PASSWORD=x\nOJ_DB_NAME=jol\n"
		conf = strings.Replace(conf, k+"="+map[string]string{
			"OJ_HOST_NAME": "127.0.0.1", "OJ_USER_NAME": "hustoj", "OJ_DB_NAME": "jol",
		}[k], k+"=   ", 1)
		if _, err := LoadJudgeConf(writeConf(t, conf)); err == nil {
			t.Errorf("%s 留空应报错", k)
		}
	}
}
