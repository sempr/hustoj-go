package main

import (
	"log"
	"os"
	"path/filepath"
)

var AppLogger *log.Logger

// InitLogger initializes the global logger.
func InitLogger(cfg *Config) {
	logFile, err := os.OpenFile(
		filepath.Join(cfg.OJHome, "log", "client.log"),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0644,
	)
	if err != nil {
		log.Fatalf("FATAL: Could not open log file: %v", err)
	}

	// If in debug mode, log to stdout as well as the file.
	if cfg.Debug {
		AppLogger = log.New(os.Stdout, "", 0)
	} else {
		AppLogger = log.New(logFile, "", log.LstdFlags)
	}
}
