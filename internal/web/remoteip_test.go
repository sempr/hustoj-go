package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// 多级 Cloudflare 后面真实用户只在 X-Forwarded-For 的链首：X-Real-IP 是
// nginx 的 $remote_addr（最近一跳代理），CF-Connecting-IP 只认到上一层代理。
func TestRemoteIPPicksXFFHead(t *testing.T) {
	srv := newTestServer(t, &fakeQuerier{}, nil)
	srv.cfg.TrustedProxies = []string{"10.80.1.10/32"}
	if err := srv.cfg.ParseTrustedProxies(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "10.80.1.10:12345"
	req.Header.Set("X-Forwarded-For", "183.159.238.57, 104.22.17.83, 45.33.42.190")
	req.Header.Set("X-Real-IP", "2400:cb00::1")

	if got := srv.remoteIP(req); got != "183.159.238.57" {
		t.Errorf("remoteIP() = %q，期望链首 183.159.238.57", got)
	}
}

// 不可信来源直连时头一律不采信，否则谁都能伪造 IP 绕过比赛网段与考试 IP 绑定。
func TestRemoteIPIgnoresHeadersFromUntrustedPeer(t *testing.T) {
	srv := newTestServer(t, &fakeQuerier{}, nil)
	srv.cfg.TrustedProxies = []string{"10.80.1.10/32"}
	if err := srv.cfg.ParseTrustedProxies(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	req.Header.Set("X-Forwarded-For", "9.9.9.9")
	req.Header.Set("X-Real-IP", "8.8.8.8")

	if got := srv.remoteIP(req); got != "203.0.113.9" {
		t.Errorf("remoteIP() = %q，期望退回对端地址 203.0.113.9", got)
	}
}
