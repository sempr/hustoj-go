/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"github.com/sempr/hustoj-go/internal/daemon"
	"github.com/sempr/hustoj-go/pkg/models"
	"github.com/spf13/cobra"
)

var daemonArgs models.DaemonArgs

// daemonCmd represents the daemon command
var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "A brief description of your command",
	Long: `A longer description that spans multiple lines and likely contains examples
and usage of using your command. For example:

Cobra is a CLI library for Go that empowers applications.
This application is a tool to generate the needed files
to quickly create a Cobra application.`,
	Run: func(cmd *cobra.Command, args []string) {
		daemon.Main(&daemonArgs)
	},
}

func init() {
	rootCmd.AddCommand(daemonCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// daemonCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// daemonCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	daemonCmd.Flags().StringVar(&daemonArgs.OJHome, "ojhome", "/home/judge", "online judge home")
	daemonCmd.Flags().BoolVar(&daemonArgs.Debug, "debug", false, "debug?")
	daemonCmd.Flags().BoolVar(&daemonArgs.Once, "once", false, "run once")
}
