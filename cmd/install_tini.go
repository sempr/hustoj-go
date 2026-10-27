package cmd

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/sempr/hustoj-go/pkg/tini"
	"github.com/spf13/cobra"
)

// installTiniCmd 把编译时嵌进本二进制的 tini 落到部署机上。
//
// 判题沙箱依赖 /usr/local/bin/tini，以前要单独分发这个文件；现在随主二进制
// 一起发，部署时跑一次本命令即可，md5 一致时自动跳过。
var installTiniCmd = &cobra.Command{
	Use:   "install-tini",
	Short: "把内置的 tini 落盘到部署机（校验 md5）",
	Long: `把编译时嵌进本二进制的 tini 静态程序写到 --path（默认 /usr/local/bin/tini）。

目标文件已存在且 md5 一致时跳过，--force 强制重写。落盘后会回读校验，
并打印 md5，便于与构建机上的 tini-static 对照。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, _ := cmd.Flags().GetString("path")
		force, _ := cmd.Flags().GetBool("force")

		sum, wrote, err := tini.InstallTo(path, force)
		if err != nil {
			return err
		}
		if wrote {
			fmt.Printf("已写入 %s（md5 %s）\n", path, sum)
		} else {
			fmt.Printf("%s 已是最新（md5 %s）\n", path, sum)
		}

		// 再问一次刚落盘的 tini：版本号 + tini.c 的源码 md5，用来确认
		// 内置的不是过期构建（拿它和仓库里 tini.c 的 md5 对一下即可）。
		if out, err := exec.Command(path, "--version").Output(); err == nil {
			fmt.Printf("%s\n", strings.TrimSpace(string(out)))
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(installTiniCmd)
	installTiniCmd.Flags().String("path", "/usr/local/bin/tini", "tini 落盘路径")
	installTiniCmd.Flags().Bool("force", false, "即使 md5 一致也重写")
}
