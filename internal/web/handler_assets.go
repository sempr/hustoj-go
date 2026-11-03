// /assets/ 静态资源（Simple.css + 覆盖样式）。
//
// Go 服务原本不提供静态文件（静态资源由 nginx 从 PHP web 根目录提供），
// 但纯 Go 域名没有那棵目录树。这里把样式编进二进制，nginx 只需用
// `location /assets/` 反代过来，不依赖磁盘，也不会因 PHP 侧改动而漂移。

package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
)

//go:embed static
var assetFS embed.FS

// assetVersion 是样式表内容的短哈希，用于给 /assets 链接加 ?v= 缓存击穿。
//
// 静态资源带了长 Cache-Control，改样式后浏览器/Cloudflare 会继续用旧文件，
// 表现为"改了没生效"。把内容哈希拼进 URL，内容一变 URL 就变，缓存自然失效，
// 因此无需手动刷新，也不必缩短缓存时间。
// 字体不参与哈希：它们由 CSS 内的相对路径引用，版本跟 CSS 走。
// minifiedAssetName 在存在 .min.<ext> 版本时，优先使用它；否则回退到原文件。
//
// 这样源文件仍可保持可读，服务端实际送出压缩后的版本，URL 路径保持不变
// （/assets/hustoj.css 实际返回 hustoj.min.css）。
func minifiedAssetName(name string) string {
	ext := filepath.Ext(name)
	if ext == "" {
		return name
	}
	stem := strings.TrimSuffix(name, ext)
	if strings.HasSuffix(stem, ".min") {
		return name
	}
	minName := stem + ".min" + ext
	if _, err := fs.Stat(assetFS, minName); err == nil {
		return minName
	}
	return name
}

var assetVersion = func() string {
	sum := sha256.New()
	for _, name := range []string{
		"static/simple.min.css", "static/hustoj.css", "static/katex.min.css",
		"static/katex.min.js", "static/auto-render.min.js",
		"static/highlight.min.js", "static/highlight-delphi.min.js",
		"static/highlight-github.min.css", "static/highlight-github-dark.min.css",
	} {
		b, err := assetFS.ReadFile(minifiedAssetName(name))
		if err != nil {
			// 只有 embed 指令与目录不符时才会发生，属构建期错误。
			panic("web: 读取静态资源失败: " + err.Error())
		}
		_, _ = sum.Write(b)
	}
	return hex.EncodeToString(sum.Sum(nil))[:8]
}()

// registerAssetRoutes 注册 /assets/<file>，文件名即 static/ 下的文件名。
func (s *Server) registerAssetRoutes(mux *http.ServeMux) {
	sub, err := fs.Sub(assetFS, "static")
	if err != nil {
		// 只有在 embed 指令与目录结构不匹配时才会发生，属于构建期错误。
		panic("web: 静态资源目录缺失: " + err.Error())
	}
	// registerAssetRoutes：请求 /assets/hustoj.css 时，优先送出 hustoj.min.css
	// 的压缩内容，URL 路径与缓存击穿哈希保持不变。
	fileHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.URL.Path, "/assets/")
		if raw == "" || strings.Contains(raw, "..") {
			http.NotFound(w, r)
			return
		}
		name := "static/" + raw
		if _, err := fs.Stat(assetFS, name); err != nil {
			http.NotFound(w, r)
			return
		}
		// 有 .min 版本就用它，避免 served 文件和哈希不一致。
		minName := minifiedAssetName(name)
		if minName != name {
			u := *r.URL
			u.Path = "/assets/" + strings.TrimPrefix(minName, "static/")
			r.URL = &u
		}
		handler := http.StripPrefix("/assets/", cacheControl(http.FileServer(http.FS(sub))))
		handler.ServeHTTP(w, r)
	})
	mux.Handle("GET /assets/", fileHandler)
}

// cacheControl 给静态资源加上缓存头（内容随二进制发布，一天足够）。
func cacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		next.ServeHTTP(w, r)
	})
}
