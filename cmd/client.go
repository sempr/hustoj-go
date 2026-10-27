/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"github.com/sempr/hustoj-go/internal/client"
	"github.com/spf13/cobra"
)

// clientCmd 是判题守护进程调用的入口。
//
// 它执行的是与 standalone（hustoj judge）同源的新实现：评测本体复用
// RunStandalone 流水线，进度与结果经 JudgeReporter 回写数据库。
// 旧的 client 实现（自己一套编译/执行/回写流程）已废弃删除。
var clientCmd = &cobra.Command{
	Use:   "client",
	Short: "Execute judge client for single submission",
	Long: `The judge client executes a single submission in an isolated environment.
It compiles the source code, runs test cases, and compares output
to determine the verdict.`,
	Run: func(cmd *cobra.Command, args []string) {
		client.Main()
	},
}

func init() {
	rootCmd.AddCommand(clientCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// clientCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// clientCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
