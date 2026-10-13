// Package web 是 OJ Web 服务的宿主。
//
// 迁移期与 PHP 前端并存：nginx 按路径把流量分给 PHP-FPM 或本服务，
// 两侧共享同一 MariaDB 与 /home/judge/data，不存在数据同步问题。
package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sempr/hustoj-go/internal/web/config"
	"github.com/sempr/hustoj-go/pkg/dbgen"
	"github.com/sempr/hustoj-go/pkg/repository"

	_ "github.com/go-sql-driver/mysql"
)

// Server 承载 Web 服务的 HTTP 生命周期。
type Server struct {
	cfg *config.Config
	log *slog.Logger

	// db 与 q 是长期持有的数据库句柄。
	//
	// 刻意不在每个请求里 Open/Close：那样每次请求都要重新握手 + 建连，
	// 高并发下连接数会打满 MariaDB 的 max_connections。
	// 早期骨架的 checkDatabase 用临时连接做 Ping，New 已改为长期持有。
	db *sql.DB
	// q 允许测试注入替身；为 nil 时回落到 dbgen.New(db)。
	q dbgen.FullQuerier

	// sessions 是 Go 自身的会话存储（Redis），登录态与 CSRF 的真相来源。
	sessions *GoSessionStore

	// sessionBridge 只读解析 PHP 会话，仅用于识别切流前已登录的老用户。
	// 为 nil 时跳过迁移兜底，resolveIdentity 直接按 Go 会话/匿名处理。
	sessionBridge *SessionBridge
}

// New 按配置构造服务：打开数据库连接池并验证连通性。
//
// 失败直接返回错误，不返回"能启动但每请求都报错"的服务：
// nginx 侧的上游健康检查只探 /healthz，服务假活会把流量引到坏实例上。
func New(cfg *config.Config, log *slog.Logger) (*Server, error) {
	db, err := openDatabase(cfg)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("连接数据库失败 (%s): %w", cfg.Database.Redacted(), err)
	}
	sessions, err := NewGoSessionStore(cfg.RedisURL, time.Duration(cfg.GoSessionTTL)*time.Second)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	var bridge *SessionBridge
	if cfg.SessionBridge {
		bridge = NewSessionBridge(cfg.SessionDir, cfg.OJName)
	}
	return &Server{cfg: cfg, log: log, db: db, sessions: sessions, sessionBridge: bridge}, nil
}

// Close 释放数据库连接池与 Redis 连接。调用方（cmd/web.go）必须在退出时执行。
func (s *Server) Close() error {
	var firstErr error
	if s.db != nil {
		firstErr = s.db.Close()
	}
	if s.sessions != nil {
		if err := s.sessions.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// openDatabase 打开数据库连接，DSN 与判题机共用同一构造函数，
// 避免两侧字符集与排序规则出现分歧。
func openDatabase(cfg *config.Config) (*sql.DB, error) {
	return sql.Open("mysql", repository.DSN(cfg.Database))
}

// handler 构造路由。当前为骨架，仅含健康检查与根占位；
// 业务路由在后续阶段按功能域逐步挂载。
func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()

	// 存活探针：只反映进程与监听状态，不触碰数据库，
	// 避免数据库抖动导致 nginx 把健康实例摘掉。
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok\n"))
	})

	// 就绪探针：反映数据库连通性。
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		// 复用长期连接池：探针本身不该每次新建连接，
		// 否则探针频率就会变成数据库连接创建速率。
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()
		if err := s.db.PingContext(ctx); err != nil {
			http.Error(w, "db unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ready\n"))
	})

	// 业务路由：首页/门户/新闻详情。
	// 根路径 / 交给 handleIndex（PHP 的 nginx index 指令内部走 index.php，URL 不变）。
	s.registerHomepageRoutes(mux)

	// 业务路由：题目列表与详情。
	// 路径与 PHP 一致，便于 nginx 按路径切流。
	s.registerProblemRoutes(mux)

	// 业务路由：提交页与提交处理。
	// 提交：GET /submitpage.php 与 POST /submit.php 一并切到 Go。
	// 两者必须同批切换，否则 PHP 渲染的表单带的是 PHPSESSID 的 token，
	// 会被 nosurf 判定为跨站请求直接拒绝。
	s.registerSubmitRoutes(mux)

	// 业务路由：比赛相关页面。
	s.registerContestRoutes(mux)

	// 业务路由：管理后台。
	s.registerAdminRoutes(mux)

	// 业务路由：状态页。
	s.registerStatusRoutes(mux)

	// 业务路由：用户档案页。
	s.registerUserInfoRoutes(mux)

	// 业务路由：全局排名页。
	s.registerRanklistRoutes(mux)

	// 业务路由：单题提交状态页。
	s.registerProblemStatusRoutes(mux)

	// 业务路由：源代码查看页。
	s.registerShowsourceRoutes(mux)

	// 业务路由：AJAX 接口（状态轮询 / 判题详情 / 命题人）。
	s.registerAjaxRoutes(mux)

	// 业务路由：P1 用户页（在线用户 / 最近比赛 / 代码导出 / 源码查看 / 滚榜）。
	s.registerP1Routes(mux)

	// 业务路由：题目来源分类页（category.php）。
	s.registerCategoryRoutes(mux)

	// 静态资源：/assets/pico.min.css、/assets/hustoj.css（编进二进制）。
	s.registerAssetRoutes(mux)

	// 业务路由：P2 用户功能（邮件 / 用户信息修改 / 密码重置）。
	s.registerP2UserRoutes(mux)

	// 业务路由：认证相关。
	s.registerAuthRoutes(mux)

	// 根路径 /：PHP 的 nginx index 指令内部走 index.php 且 URL 保持 /，
	// 因此这里直接渲染首页，不做 302 重定向，保持 URL 与内容一致。
	mux.HandleFunc("GET /{$}", s.handleIndex)

	// CSRF 校验包在最外层：nosurf 只拦不安全方法，
	// 所以读接口无感，POST 在进入任何 handler 前就已被验过一遍。
	//
	// 放在 recovery 里面而不是外面 —— recovery 要能兜住 nosurf
	// 内部可能出的 panic（例如 rand 读取失败），否则那类 panic
	// 会绕过 recover 直接把连接断掉。
	return s.withRecovery(wrapCSRF(mux))
}

// withRecovery 兜住处理器 panic，避免单个请求打挂整个服务。
func (s *Server) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("请求处理 panic",
					"panic", rec,
					"method", r.Method,
					"path", r.URL.Path)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Run 启动 HTTP 服务并阻塞，直到收到终止信号或监听失败。
func (s *Server) Run() error {
	srv := &http.Server{
		Handler:           s.handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}

	// 先绑定再输出"已启动"，避免日志早于实际监听
	ln, err := net.Listen("tcp", s.cfg.Listen)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", s.cfg.Listen, err)
	}

	s.log.Info("Web 服务已启动",
		"listen", s.cfg.Listen,
		"template", s.cfg.Template,
		"oj_home", s.cfg.OJHome,
		"session_bridge", s.cfg.SessionBridge,
		"redis_url", s.cfg.RedisURL,
		"free_practice", s.cfg.FreePractice,
		"debug", s.cfg.Debug,
		"db", s.cfg.Database.Redacted())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.log.Info("收到终止信号，开始优雅关闭")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("优雅关闭失败: %w", err)
	}
	s.log.Info("Web 服务已退出")
	return nil
}
