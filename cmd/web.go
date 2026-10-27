/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"log/slog"
	"os"

	"github.com/sempr/hustoj-go/internal/web"
	"github.com/sempr/hustoj-go/internal/web/config"
	"github.com/sempr/hustoj-go/pkg/models"
	"github.com/spf13/cobra"
)

var webArgs models.WebArgs

// webCmd 代表 Web 服务子命令
var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Start the OJ web service",
	Long: `The web service renders the OJ frontend and writes to the same MariaDB
and judge data directory as the PHP frontend, so nginx can route traffic to
either side during the migration without any data synchronization.

Configuration is assembled from two sources, with environment variables taking
precedence:

  1. --ojhome/etc/web.toml  operational settings (no credentials)
  2. environment variables   injected from judge.conf by systemd
                             (EnvironmentFile=), source of DB credentials`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWeb(&webArgs)
	},
}

func runWeb(args *models.WebArgs) error {
	level := slog.LevelInfo
	if args.Debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	cfg, err := config.Load(args.OJHome, args.Config)
	if err != nil {
		return err
	}

	srv, err := web.New(cfg, log)
	if err != nil {
		return err
	}
	// 关闭连接池：Run 返回后必须释放，否则同一进程内若再跑一次 Run
	// （测试、将来的热重载）会泄漏连接，最终打满 max_connections。
	defer func() {
		if cerr := srv.Close(); cerr != nil {
			log.Error("关闭数据库连接失败", "err", cerr)
		}
	}()

	return srv.Run()
}

func init() {
	rootCmd.AddCommand(webCmd)

	webCmd.Flags().StringVar(&webArgs.OJHome, "ojhome", "/home/judge", "online judge home")
	webCmd.Flags().StringVar(&webArgs.Config, "config", "", "path to web.toml (default <ojhome>/etc/web.toml)")
	webCmd.Flags().BoolVar(&webArgs.Debug, "debug", false, "debug?")
}
