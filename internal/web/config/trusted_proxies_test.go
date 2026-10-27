package config

import (
	"path/filepath"
	"testing"
)

// TestIsTrustedProxy 覆盖可信代理网段判定。
//
// 这条判定决定"要不要采信 X-Real-IP / X-Forwarded-For"：
// 判松了，任何人都能伪造来源 IP 绕过比赛网段与考试 IP 绑定；
// 判紧了，真实 IP 永远取不到（表现为"用户都在同一个 IP"）。
func TestIsTrustedProxy(t *testing.T) {
	withEnv(t, dbEnv())
	cfg, err := Load("/home/judge", filepath.Join(t.TempDir(), "无.toml"))
	if err != nil {
		t.Fatal(err)
	}
	// 默认只信回环。
	for _, tc := range []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"127.0.0.2", false},
		{"::1", true},
		{"10.80.1.11", false},
		{"", false},
		{"不是 IP", false},
	} {
		if got := cfg.IsTrustedProxy(tc.ip); got != tc.want {
			t.Errorf("IsTrustedProxy(%q) = %v，期望 %v", tc.ip, got, tc.want)
		}
	}
}

// TestTrustedProxiesAcceptsIPAndCIDR 覆盖网段写法：既接受 CIDR，也接受裸 IP。
func TestTrustedProxiesAcceptsIPAndCIDR(t *testing.T) {
	withEnv(t, dbEnv())
	p := writeTOML(t, "trusted_proxies = [\"10.80.1.0/24\", \"192.168.1.7\"]\n")
	cfg, err := Load("/home/judge", p)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ip   string
		want bool
	}{
		{"10.80.1.11", true},
		{"10.80.2.11", false},
		{"192.168.1.7", true},
		{"192.168.1.8", false},
	} {
		if got := cfg.IsTrustedProxy(tc.ip); got != tc.want {
			t.Errorf("IsTrustedProxy(%q) = %v，期望 %v", tc.ip, got, tc.want)
		}
	}
}

// TestTrustedProxiesRejectsGarbage 确认配错的网段会让启动失败，
// 而不是静默失效（静默失效表现为"真实 IP 一直取不到"，极难排查）。
func TestTrustedProxiesRejectsGarbage(t *testing.T) {
	withEnv(t, dbEnv())
	p := writeTOML(t, "trusted_proxies = [\"不是网段\"]\n")
	if _, err := Load("/home/judge", p); err == nil {
		t.Error("期望报错：trusted_proxies 里有非法条目")
	}
}
