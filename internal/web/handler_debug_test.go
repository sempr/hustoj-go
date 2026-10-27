package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sempr/hustoj-go/internal/web/config"
)

// TestDebugIPEndpointEchoesProxyHeaders 验证调试端点把代理链原样回显。
//
// 场景：站点挂在多层 Cloudflare 后面，各层塞的头不一样，
// 只有把 peer / xff / cf_connecting_ip 一起打出来才知道该采信哪个。
func TestDebugIPEndpointEchoesProxyHeaders(t *testing.T) {
	srv := newTestServer(t, &fakeQuerier{}, func(c *config.Config) {
		c.Debug = true
		c.TrustedProxies = []string{"203.0.113.0/24"}
	})
	// validate() 才会把 TrustedProxies 解析成网段，这里手动走一遍。
	if err := srv.cfg.ParseTrustedProxies(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/_debug/ip", nil)
	req.RemoteAddr = "203.0.113.7:54321"
	req.Header.Set("X-Forwarded-For", "45.33.42.190, 198.51.100.9")
	req.Header.Set("CF-Connecting-IP", "45.33.42.190")
	req.Header.Set("X-Real-IP", "198.51.100.9")
	req.Header.Set("X-Forwarded-Proto", "https")

	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200：%s", rec.Code, rec.Body.String())
	}

	var info debugIPInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("输出不是 JSON：%v\n%s", err, rec.Body.String())
	}
	if info.PeerHost != "203.0.113.7" {
		t.Errorf("peer_host = %q，期望 203.0.113.7", info.PeerHost)
	}
	if !info.TrustedPeer {
		t.Errorf("对端在 trusted_proxies 内，trusted_peer 应为 true")
	}
	if len(info.XFFChain) != 2 || info.XFFChain[0] != "45.33.42.190" {
		t.Errorf("xff_chain = %v，期望两段且首段为中转机 IP", info.XFFChain)
	}
	if info.TrueClientIP != "45.33.42.190" {
		t.Errorf("true_client_ip = %q，期望取链首段", info.TrueClientIP)
	}
	if info.CFConnectingIP != "45.33.42.190" {
		t.Errorf("cf_connecting_ip = %q", info.CFConnectingIP)
	}
	if info.Proto != "https" {
		t.Errorf("proto = %q，期望 https", info.Proto)
	}
}

// TestDebugIPEndpointNotMountedWithoutDebug 确认端点只在 debug 模式挂载。
//
// 它会回显来访者地址与代理头，生产开着等于给扫描器送信息。
func TestDebugIPEndpointNotMountedWithoutDebug(t *testing.T) {
	srv := newTestServer(t, &fakeQuerier{}, func(c *config.Config) {
		c.Debug = false
	})
	rec := httptest.NewRecorder()
	srv.handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/_debug/ip", nil))
	if rec.Code == http.StatusOK {
		t.Errorf("非 debug 模式不该挂载调试端点，却返回 200：%s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "404") {
		t.Errorf("期望 404 页面，实际：%s", rec.Body.String())
	}
}
