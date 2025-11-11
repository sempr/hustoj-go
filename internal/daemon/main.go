package daemon

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/sevlyar/go-daemon"
)

var (
	ojHome = flag.String("oj_home", "/home/judge", "OJ home directory")
	debug  = flag.Bool("debug", false, "Enable debug mode")
	once   = flag.Bool("once", false, "Run only one work cycle")
)

func Main() {
	flag.Parse()

	// Change to the working directory
	if err := os.Chdir(*ojHome); err != nil {
		log.Fatalf("FATAL: Could not change to directory %s: %v", *ojHome, err)
	}

	// Load configuration
	cfg, err := LoadConfig("etc/judge.conf")
	if err != nil {
		log.Fatalf("FATAL: Error loading judge.conf: %v", err)
	}
	cfg.OJHome = *ojHome
	cfg.Debug = *debug
	cfg.Once = *once

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
			log.Fatalf("FATAL: Could not reborn as daemon: %v", err)
		}
		if d != nil {
			return // Parent process exits
		}
		defer cntxt.Release()
	}

	AppLogger.Println("INFO: judged-go started")

	// Lock PID file to ensure a single instance
	lockFile := filepath.Join(cfg.OJHome, "etc", "judge.pid")
	if err := Lock(lockFile); err != nil {
		AppLogger.Printf("FATAL: Daemon is already running: %v", err)
		log.Fatalf("FATAL: Daemon is already running: %v", err)
	}
	defer Unlock()

	// Create the job fetcher
	fetcher, err := NewFetcher(cfg)
	if err != nil {
		AppLogger.Printf("FATAL: Could not create fetcher: %v", err)
		log.Fatalf("FATAL: Could not create fetcher: %v", err)
	}
	defer fetcher.Close()

	// Channel to stop the program gracefully
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-stop
		AppLogger.Println("INFO: Stop signal received, shutting down...")
		cancel()
	}()

	// Create and run the worker
	worker := NewWorker(cfg, fetcher)
	worker.Run(ctx)

	AppLogger.Println("INFO: judged-go stopped.")
}
