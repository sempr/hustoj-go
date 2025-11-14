package daemon

import (
	"log"
	"log/slog"
	"os"
	"path/filepath"
)

// InitLogger initializes the global logger.
func InitLogger(cfg *Config) {
	var handler slog.Handler

	logLevel := slog.LevelInfo
	if cfg.Debug {
		logLevel = slog.LevelDebug
	}

	opts := &slog.HandlerOptions{
		Level: logLevel,
	}

	if cfg.Debug {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		logFilePath := filepath.Join(cfg.OJHome, "log", "judged-go.log")
		logFile, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			log.Fatalf("FATAL: Could not open log file %s: %v", logFilePath, err)
		}
		handler = slog.NewJSONHandler(logFile, opts)
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)
}
