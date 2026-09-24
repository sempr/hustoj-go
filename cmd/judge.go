/*
Copyright © 2026
*/
package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"syscall"

	"github.com/sempr/hustoj-go/internal/client"
	"github.com/sempr/hustoj-go/pkg/models"
	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"
)

var judgeTask models.JudgeTask

// judgeCmd 表示不依赖数据库的单机评测命令。
var judgeCmd = &cobra.Command{
	Use:   "judge",
	Short: "Run a standalone judge without database",
	Long: `The judge command runs a complete judge task without any database dependency.
All judge parameters (source file, language, test data directory, limits and
problem type) are provided via command line flags, and the result is written
to stdout as JSON.

Example:
  hustoj-go judge --source Main.c --lang 0 --data /home/judge/data/1000 \
      --time 1000 --memory 262144 --spj 0

  cat Main.cpp | hustoj-go judge --source=- --lang 1 --data ./data \
      --time 1000 --memory 262144 --spj 1`,
	Run: func(cmd *cobra.Command, args []string) {
		// 评测过程中大量日志（本进程 slog 及沙箱子进程输出）默认写入 stdout，
		// 先把 stdout 重定向到 stderr，保持 stdout 只输出最终的 JSON 结果。
		restoreStdout, err := redirectStdoutToStderr()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Failed to redirect stdout:", err)
			os.Exit(1)
		}

		if err := loadJudgeSource(&judgeTask); err != nil {
			slog.Error("Failed to load source code", "error", err)
			os.Exit(1)
		}
		if judgeTask.OJHome == "" {
			judgeTask.OJHome = "/home/judge"
		}
		if judgeTask.WorkBase == "" {
			judgeTask.WorkBase = "/tmp"
		}

		judgeClient, err := client.NewStandaloneClient(&judgeTask)
		if err != nil {
			slog.Error("Failed to create standalone judge client", "error", err)
			os.Exit(1)
		}
		defer judgeClient.Close()

		result, err := judgeClient.RunStandalone()
		restoreStdout()
		if err != nil {
			slog.Error("Standalone judge failed", "error", err)
			os.Exit(1)
		}

		// stdout 恢复后，只输出结果 JSON
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			slog.Error("Failed to encode result", "error", err)
			os.Exit(1)
		}
	},
}

// redirectStdoutToStderr 把当前进程的 stdout(fd 1) 复制到 stderr(fd 2)，
// 并返回一个恢复函数，把 stdout 指回保存的原始 fd。
func redirectStdoutToStderr() (func(), error) {
	saved, err := syscall.Dup(1)
	if err != nil {
		return nil, fmt.Errorf("failed to dup stdout: %w", err)
	}
	if err := unix.Dup2(2, 1); err != nil {
		syscall.Close(saved)
		return nil, fmt.Errorf("failed to redirect stdout to stderr: %w", err)
	}
	return func() {
		unix.Dup2(saved, 1)
		syscall.Close(saved)
	}, nil
}

// loadJudgeSource 读取源码：--source 为文件路径，"-" 表示从标准输入读取。
func loadJudgeSource(task *models.JudgeTask) error {
	if task.Source != "" {
		return nil
	}

	var data []byte
	var err error
	switch task.SourceFile {
	case "":
		return fmt.Errorf("no source provided, use --source")
	case "-":
		data, err = io.ReadAll(os.Stdin)
	default:
		data, err = os.ReadFile(task.SourceFile)
	}
	if err != nil {
		return fmt.Errorf("failed to read source: %w", err)
	}
	task.Source = string(data)
	return nil
}

func init() {
	rootCmd.AddCommand(judgeCmd)

	judgeCmd.Flags().StringVar(&judgeTask.SourceFile, "source", "", "path to source file, '-' means read from stdin")
	judgeCmd.Flags().IntVar(&judgeTask.Language, "lang", 0, "language ID (see etc/langs/all.toml)")
	judgeCmd.Flags().StringVar(&judgeTask.DataDir, "data", "", "test data directory")
	judgeCmd.Flags().IntVar(&judgeTask.TimeLimit, "time", 1000, "time limit in ms per test case")
	judgeCmd.Flags().IntVar(&judgeTask.MemLimitKB, "memory", 262144, "memory limit in KB")
	judgeCmd.Flags().IntVar(&judgeTask.Spj, "spj", 0, "problem type: 0=normal, 1=special judge, 2=raw text")
	judgeCmd.Flags().StringVar(&judgeTask.OJHome, "ojhome", "/home/judge", "judge home for etc/langs configs")
	judgeCmd.Flags().StringVar(&judgeTask.WorkBase, "workbase", "/tmp", "base directory for overlay mount workdir")
}
