// /assets/ 静态资源（Simple.css + 覆盖样式）。
//
// Go 服务原本不提供静态文件（静态资源由 nginx 从 PHP web 根目录提供），
// 但纯 Go 域名没有那棵目录树。这里把样式编进二进制，nginx 只需用
// `location /assets/` 反代过来，不依赖磁盘，也不会因 PHP 侧改动而漂移。

package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/simple.min.css static/hustoj.css
var assetFS embed.FS

// registerAssetRoutes 注册 /assets/<file>，文件名即 static/ 下的文件名。
func (s *Server) registerAssetRoutes(mux *http.ServeMux) {
	sub, err := fs.Sub(assetFS, "static")
	if err != nil {
		// 只有在 embed 指令与目录结构不匹配时才会发生，属于构建期错误。
		panic("web: 静态资源目录缺失: " + err.Error())
	}
	handler := http.StripPrefix("/assets/", cacheControl(http.FileServer(http.FS(sub))))
	mux.Handle("GET /assets/", handler)
}

// cacheControl 给静态资源加上缓存头（内容随二进制发布，一天足够）。
func cacheControl(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		next.ServeHTTP(w, r)
	})
}
