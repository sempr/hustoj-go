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
	cfg, err := LoadConfig("etc/judge.conf")
	if err != nil {
		slog.Error("FATAL: Error loading judge.conf", "err", err)
		os.Exit(1)
	}

	cfg.OJHome = daemonArgs.OJHome
	cfg.Debug = daemonArgs.Debug
	cfg.Once = daemonArgs.Once

	// Initialize logger
	InitLogger(cfg)

	// Set up daemonization if not in debug mode
	if !cfg.Debug {
		pidFilePath := filepath.Join(cfg.OJHome, "etc", "judge.pid")
		logFilePath := filepath.Join(cfg.OJHome, "log", "judged-go.log")

		cntxt := &daemon.Context{
			PidFileName: pidFilePath,
			PidFilePerm: 0644,
			LogFileName: logFilePath,
			LogFilePerm: 0640,
			WorkDir:     cfg.OJHome,
			Umask:       027,
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
	lockFile := filepath.Join(cfg.OJHome, "etc", "judge.pid")
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
