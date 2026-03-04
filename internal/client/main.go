package client

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

func Main() {
	args := os.Args[1:]

	if len(args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s client <solution_id> <runner_id> [oj_home_path] [-debug]\n", os.Args[0])
		os.Exit(1)
	}

	solutionID, err := strconv.Atoi(args[1])
	if err != nil {
		slog.Error("Invalid solution ID", "input", args[1], "error", err)
		os.Exit(1)
	}

	runnerID := args[2]
	homePath := "/home/judge"
	debug := false

	for _, arg := range args[3:] {
		if arg == "-debug" || arg == "DEBUG" {
			debug = true
		} else if !strings.HasPrefix(arg, "-") {
			homePath = arg
		}
	}

	client, err := NewJudgeClient(solutionID, runnerID, homePath, debug)
	if err != nil {
		slog.Error("Failed to create judge client", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	slog.Info("Starting judge process", "solution_id", solutionID, "runner_id", runnerID)

	if err := client.Run(); err != nil {
		slog.Error("Judge process failed", "error", err)
		os.Exit(1)
	}

	slog.Info("Judge process completed successfully")
}
