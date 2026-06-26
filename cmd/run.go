/*
Copyright © 2026 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"github.com/sempr/hustoj-go/internal/run"
	"github.com/sempr/hustoj-go/pkg/models"
	"github.com/spf13/cobra"
)

var runCfg models.SandboxArgs

// runCmd represents the run command
var runCmd = &cobra.Command{
	Use:   "run",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		run.RunMain(&runCfg)
	},
}

func init() {
	rootCmd.AddCommand(runCmd)

	runCmd.Flags().StringVar(&runCfg.Rootfs, "rootfs", "/tmp", "root filesystem path")
	runCmd.Flags().StringVar(&runCfg.Command, "cmd", "/bin/false", "command to execute")
	runCmd.Flags().StringVar(&runCfg.Workdir, "cwd", "/code", "working directory inside sandbox")
	runCmd.Flags().StringVar(&runCfg.Stdin, "stdin", "", "path to stdin file")
	runCmd.Flags().StringVar(&runCfg.Stdout, "stdout", "", "path to stdout file")
	runCmd.Flags().StringVar(&runCfg.Stderr, "stderr", "", "path to stderr file")
	runCmd.Flags().IntVar(&runCfg.TimeLimit, "time", 1000, "time limit in ms")
	runCmd.Flags().IntVar(&runCfg.MemoryLimit, "memory", 256<<10, "memory limit in KB")
	runCmd.Flags().IntVar(&runCfg.SolutionId, "sid", 0, "solution ID")
	runCmd.Flags().StringVar(&runCfg.Stage, "stage", "run", "running stage [compile, run0/run1, validate0, validate1...]")
}
