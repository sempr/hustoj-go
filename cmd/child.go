/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"github.com/sempr/hustoj-go/internal/sandbox"
	"github.com/sempr/hustoj-go/pkg/models"
	"github.com/spf13/cobra"
)

var childArgs models.SandboxArgs

// childCmd represents the child command
var childCmd = &cobra.Command{
	Use:   "child",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		sandbox.ChildMain(&childArgs)
	},
}

func init() {
	rootCmd.AddCommand(childCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// childCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// childCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	childCmd.Flags().StringVar(&childArgs.Rootfs, "rootfs", "/tmp", "root filesystem path")
	childCmd.Flags().StringVar(&childArgs.Command, "cmd", "/bin/false", "command to execute")
	childCmd.Flags().StringVar(&childArgs.Workdir, "cwd", "/code", "working directory inside sandbox")
	childCmd.Flags().StringVar(&childArgs.Stdin, "stdin", "", "path to stdin file")
	childCmd.Flags().StringVar(&childArgs.Stdout, "stdout", "", "path to stdout file")
	childCmd.Flags().StringVar(&childArgs.Stderr, "stderr", "", "path to stderr file")
	childCmd.Flags().IntVar(&childArgs.TimeLimit, "time", 1000, "time limit in ms")
	childCmd.Flags().IntVar(&childArgs.MemoryLimit, "memory", 256<<10, "memory limit in KB")
	childCmd.Flags().IntVar(&childArgs.SolutionId, "sid", 0, "solution ID")
}
