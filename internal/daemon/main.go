package daemon

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/sempr/hustoj-go/pkg/models"
	"github.com/sevlyar/go-daemon"
	"golang.org/x/sys/unix"
)

var daemonArgs *models.DaemonArgs

func Main(ccfg *models.DaemonArgs) {
	daemonArgs = ccfg
	// Change to the working directory
	if err := os.Chdir(daemonArgs.OJHome); err != nil {
		slog.Error("FATAL: Could not change to directory", "path", daemonArgs.OJHome, "err", err)
		os.Exit(1)
	}

	// Load configuration
	cfg, err := LoadDaemonConfig("etc/judge.conf")
	if err != nil {
		slog.Error("FATAL: Error loading judge.conf", "err", err)
		os.Exit(1)
	}

	cfg.OJHome = daemonArgs.OJHome
	cfg.Debug = daemonArgs.Debug
	cfg.Once = daemonArgs.Once
	// OJ_CGROUP_ROOT / OJ_SPAWN_PATH 等需要传给 client 子进程。
	cfg.ApplyEnv()

	runDir := filepath.Join(cfg.OJHome, "run")
	logDir := filepath.Join(cfg.OJHome, "log")
	for _, d := range []string{runDir, logDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			slog.Error("FATAL: Could not create runtime directory", "path", d, "err", err)
			os.Exit(1)
		}
	}

	// Set up daemonization if not in debug mode.
	// systemd 的 Type=simple 要求主进程保持前台：自我 daemonize（Reborn）会让
	// 父进程退出，systemd 直接判定服务已停止。因此由 systemd 拉起时跳过 Reborn，
	// 不依赖 --debug 也能被正确监管（与 cmd/loginit.go 的 systemd 判断一致）。
	if !cfg.Debug && !runningUnderSystemd() {
		// daemon 以 judge 用户运行时不能写 <ojhome>/etc（judge.conf 是
		// root:judge 0640）；pid 与日志统一放到 install 时 chown 给 judge 的目录。
		pidFilePath := filepath.Join(runDir, "judge.pid")
		logFilePath := filepath.Join(logDir, "judged-go.log")

		cntxt := &daemon.Context{
			PidFileName: pidFilePath,
			PidFilePerm: 0o644,
			LogFileName: logFilePath,
			LogFilePerm: 0o640,
			WorkDir:     cfg.OJHome,
			Umask:       0o27,
		}

		d, err := cntxt.Reborn()
		if err != nil {
			slog.Error("FATAL: Could not reborn as daemon", "err", err)
			os.Exit(1)
		}
		if d != nil {
			return // Parent process exits
		}
		defer cntxt.Release()
	}

	slog.Info("judged-go started")

	// Lock PID file to ensure a single instance
	lockFile := filepath.Join(runDir, "judge.pid")
	if err := Lock(lockFile); err != nil {
		slog.Error("FATAL: Daemon is already running", "err", err)
		os.Exit(1)
	}
	defer Unlock()

	// Create the job fetcher
	fetcher, err := NewFetcher(cfg)
	if err != nil {
		slog.Error("FATAL: Could not create fetcher", "err", err)
		os.Exit(1)
	}
	defer fetcher.Close()

	// Channel to stop the program gracefully
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, unix.SIGINT, unix.SIGTERM, unix.SIGQUIT)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-stop
		slog.Info("Stop signal received, shutting down...")
		cancel()
	}()

	// Create and run the worker
	worker := NewWorker(cfg, fetcher)
	worker.Run(ctx)

	slog.Info("judged-go stopped.")
}

// runningUnderSystemd 判断进程是否由 systemd 拉起（systemd 会注入 INVOCATION_ID）。
func runningUnderSystemd() bool {
	return os.Getenv("INVOCATION_ID") != ""
}
