/*
Copyright © 2026
*/
package cmd

import (
	"github.com/sempr/hustoj-go/internal/client"
	"github.com/spf13/cobra"
)

// client2Cmd 表示复用 judge 流水线并上报数据库的评测客户端命令。
var client2Cmd = &cobra.Command{
	Use:   "client2",
	Short: "Execute judge client for single submission via the judge pipeline",
	Long: `The client2 command executes a single submission in an isolated environment,
identical in behavior to the client command. Unlike client, judging is performed
by reusing the standalone judge pipeline (RunStandalone), while progress and
results are reported back to the database.`,
	Run: func(cmd *cobra.Command, args []string) {
		client.Client2Main()
	},
}

func init() {
	rootCmd.AddCommand(client2Cmd)
}
