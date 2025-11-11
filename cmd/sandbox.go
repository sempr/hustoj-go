/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"github.com/sempr/hustoj-go/internal/sandbox"
	"github.com/sempr/hustoj-go/pkg/models"
	"github.com/spf13/cobra"
)

var sandboxCfg models.SandboxArgs

// sandboxCmd represents the sandbox command
var sandboxCmd = &cobra.Command{
	Use:   "sandbox",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		sandbox.ParentMain(&sandboxCfg)
	},
}

func init() {
	rootCmd.AddCommand(sandboxCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// sandboxCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// sandboxCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	sandboxCmd.Flags().StringVar(&sandboxCfg.Rootfs, "rootfs", "/tmp", "root filesystem path")
	sandboxCmd.Flags().StringVar(&sandboxCfg.Command, "cmd", "/bin/false", "command to execute")
	sandboxCmd.Flags().StringVar(&sandboxCfg.Workdir, "cwd", "/code", "working directory inside sandbox")
	sandboxCmd.Flags().StringVar(&sandboxCfg.Stdin, "stdin", "", "path to stdin file")
	sandboxCmd.Flags().StringVar(&sandboxCfg.Stdout, "stdout", "", "path to stdout file")
	sandboxCmd.Flags().StringVar(&sandboxCfg.Stderr, "stderr", "", "path to stderr file")
	sandboxCmd.Flags().IntVar(&sandboxCfg.TimeLimit, "time", 1000, "time limit in ms")
	sandboxCmd.Flags().IntVar(&sandboxCfg.MemoryLimit, "memory", 256<<10, "memory limit in KB")
	sandboxCmd.Flags().IntVar(&sandboxCfg.SolutionId, "sid", 0, "solution ID")

}
