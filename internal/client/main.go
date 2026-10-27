package client

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/sempr/hustoj-go/pkg/constants"
)

// Main 是 client 子命令的入口。
//
//	hustoj client <solution_id> <runner_id> [oj_home_path] [-debug]
//
// 评测本体只有一条实现：普通提交走 judge 流水线（RunStandalone，与
// standalone 的 hustoj judge 同源），测试运行（custominput）走自定义输入
// 路径；状态与结果统一由 JudgeReporter 回写数据库。
func Main() {
	solutionID, runnerID, homePath, debug, err := parseClientArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Usage: %s client <solution_id> <runner_id> [oj_home_path] [-debug]\n", os.Args[0])
		os.Exit(1)
	}

	client, err := NewClient(solutionID, runnerID, homePath, debug)
	if err != nil {
		slog.Error("Failed to create judge client", "error", err)
		os.Exit(1)
	}
	defer client.Close()

	slog.Info("Starting judge process", "solution_id", solutionID, "runner_id", runnerID)

	result, err := client.Run()
	if err != nil {
		slog.Error("Judge process failed", "error", err)
		os.Exit(1)
	}
	if result.FinalResult == constants.OJ_CE || result.FinalResult == constants.OJ_SE {
		slog.Info("Judge process finished with compile/system error", "final_result", result.FinalResult)
		os.Exit(1)
	}

	slog.Info("Judge process completed successfully",
		"final_result", result.FinalResult, "time_ms", result.TimeMs, "memory_kb", result.MemoryKB)
}

// parseClientArgs 解析 os.Args[1:]，其中 args[0] 是子命令名。
func parseClientArgs(args []string) (solutionID int, runnerID, homePath string, debug bool, err error) {
	if len(args) < 3 {
		return 0, "", "", false, fmt.Errorf("缺少参数")
	}

	solutionID, err = strconv.Atoi(args[1])
	if err != nil {
		return 0, "", "", false, fmt.Errorf("非法的 solution_id %q: %w", args[1], err)
	}

	runnerID = args[2]
	homePath = "/home/judge"
	for _, arg := range args[3:] {
		if arg == "-debug" || arg == "DEBUG" {
			debug = true
		} else if !strings.HasPrefix(arg, "-") {
			homePath = arg
		}
	}
	return solutionID, runnerID, homePath, debug, nil
}
