/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sempr/hustoj-go/internal/rootless"
	"github.com/spf13/cobra"
)

// 全局 rootless 相关参数。
//
// 这些值最终都要落到环境变量里：hustoj daemon 会以 judge 用户启动
// hustoj client，子进程不会继承命令行参数，只能继承环境。
var (
	rootlessFlag   string
	spawnPathFlag  string
	cgroupRootFlag string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "hustoj",
	Short: "HUST Online Judge implementation in Go",
	Long: `HUSTOJ-Go is a high-performance online judge system written in Go.
It provides daemon services for judging submissions and client tools
for executing judge tasks in isolated environments.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	rootCmd.PersistentFlags().StringVar(&rootlessFlag, "rootless", "auto",
		"rootless mode: auto (spawner for non-root), on (always use spawner), off (root only)")
	rootCmd.PersistentFlags().StringVar(&spawnPathFlag, "spawn-path", "",
		"path to hustoj-spawn (default: $OJ_SPAWN_PATH, judge.conf, or /usr/local/libexec/hustoj-spawn)")
	rootCmd.PersistentFlags().StringVar(&cgroupRootFlag, "cgroup-root", "",
		"delegated cgroup v2 parent (default: $OJ_CGROUP_ROOT, judge.conf, or <cgroup2>/hustoj)")
	rootCmd.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		// 只有显式给了 --rootless 才覆盖配置；否则让 judge.conf/环境变量生效。
		if rootCmd.PersistentFlags().Changed("rootless") {
			mode, err := rootless.ParseMode(rootlessFlag)
			if err != nil {
				return err
			}
			rootless.SetModeOverride(mode)
		}
		// 路径参数同样只做“显式覆盖”：转成环境变量后，spawner 与 client
		// 子进程都能读到（daemon 启动 client 时只传环境）。
		for _, item := range []struct {
			flag string
			env  string
			val  string
		}{
			{"spawn-path", "OJ_SPAWN_PATH", spawnPathFlag},
			{"cgroup-root", "OJ_CGROUP_ROOT", cgroupRootFlag},
		} {
			if !rootCmd.PersistentFlags().Changed(item.flag) {
				continue
			}
			if !filepath.IsAbs(item.val) {
				return fmt.Errorf("--%s 必须是绝对路径: %q", item.flag, item.val)
			}
			if err := os.Setenv(item.env, filepath.Clean(item.val)); err != nil {
				return fmt.Errorf("设置 %s 失败: %w", item.env, err)
			}
		}
		return nil
	}
}
