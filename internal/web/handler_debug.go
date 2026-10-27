package web

import (
	"encoding/json"
	"net/http"
	"strings"
)

// debugIPInfo 是 /_debug/ip 的输出。
//
// 排障真实 IP 时要同时看三件事：代理链长什么样、对端算不算可信、
// 当前取值逻辑会挑哪个 —— 只看一个头很容易被 Cloudflare 的多层链带偏。
type debugIPInfo struct {
	// Peer 是 TCP 直连对端（myserver 这层看到的是 Cloudflare 边缘节点）。
	Peer string `json:"peer"`
	// PeerHost 是对端 IP（去掉端口）。
	PeerHost string `json:"peer_host"`
	// TrustedPeer 表示对端是否落在 trusted_proxies 内。
	// 为 false 时，这些代理头就不该采信 —— 现在还没接上判断，先如实报出来。
	TrustedPeer bool `json:"trusted_peer"`

	// XForwardedFor 是各层代理留下的 X-Forwarded-For 原值。
	XForwardedFor string `json:"x_forwarded_for"`
	// XFFChain 是它按逗号拆开的每一段。
	XFFChain []string `json:"xff_chain"`
	// CFConnectingIP 是 Cloudflare 写的原始客户端头。
	CFConnectingIP string `json:"cf_connecting_ip"`
	// XRealIP 是 nginx 上最常见的"真实 IP"头。
	XRealIP string `json:"x_real_ip"`

	// TrueClientIP 是按"代理链最左一段"算出的候选真实用户 IP。
	// 链路是 用户 → CF1 → server1 → CF2 → myserver 时，真实用户只出现在这一段。
	TrueClientIP string `json:"true_client_ip"`
	// CurrentRemoteIP 是当前 remoteIP() 逻辑实际会用的值。
	CurrentRemoteIP string `json:"current_remote_ip"`

	Host  string `json:"host"`
	Proto string `json:"proto"`
}

// handleDebugIP 回显本次请求的 IP 相关头，用于排查反向代理取不到真实 IP 的问题。
//
// 只在 debug 模式挂载（见 main.go）：它把来访者地址与各类代理头原样回显，
// 生产环境开着等于给扫描器送信息。
//
// 用法：从公网打一次 https://<站点>/_debug/ip，看 JSON 里的
// xff_chain / cf_connecting_ip / trusted_peer 三项即可判断该采信哪个头。
func (s *Server) handleDebugIP(w http.ResponseWriter, r *http.Request) {
	peerHost, _, err := splitHostPort(r.RemoteAddr)
	if err != nil {
		peerHost = r.RemoteAddr
	}
	xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	chain := splitAddrList(xff)

	info := debugIPInfo{
		Peer:            r.RemoteAddr,
		PeerHost:        peerHost,
		TrustedPeer:     s.cfg.IsTrustedProxy(peerHost),
		XForwardedFor:   xff,
		XFFChain:        chain,
		CFConnectingIP:  strings.TrimSpace(r.Header.Get("CF-Connecting-IP")),
		XRealIP:         strings.TrimSpace(r.Header.Get("X-Real-IP")),
		CurrentRemoteIP: s.remoteIP(r),
		Host:            r.Host,
		Proto:           requestScheme(r),
	}
	if len(chain) > 0 {
		info.TrueClientIP = chain[0]
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(info)
}

// splitAddrList 把逗号分隔的地址串拆成列表，去掉空段。
func splitAddrList(list string) []string {
	if strings.TrimSpace(list) == "" {
		return nil
	}
	parts := strings.Split(list, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// requestScheme 判定本次请求对外的协议。
//
// TLS 在 nginx 终止，Go 看到的 r.TLS 恒为 nil，所以要看 X-Forwarded-Proto
// （与 csrf.go 里的 IsTLSFunc 同一口径）。
func requestScheme(r *http.Request) string {
	if r.Header.Get("X-Forwarded-Proto") == "https" || r.TLS != nil {
		return "https"
	}
	return "http"
}
