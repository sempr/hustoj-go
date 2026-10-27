/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sempr/hustoj-go/internal/langinfo"
	"github.com/spf13/cobra"
)

var (
	langinfoOJHome   string
	langinfoVersions string
	langinfoOut      string
)

// langinfoCmd 生成 FAQ 页的"受支持语言"信息片段。
//
// 版本号取自版本缓存（extra/check_versions.sh 的输出，默认
// <ojhome>/etc/lang-versions.txt），编译/运行命令取自语言注册表。
// 结果落盘到 <ojhome>/etc/faq-langs.html，由 Web 侧读取，避免每次请求
// 都去挂载语言 rootfs 或重跑 ver。
var langinfoCmd = &cobra.Command{
	Use:   "langinfo",
	Short: "Generate the FAQ language info fragment (versions/compile/run)",
	Long: `Read the language registry (<ojhome>/etc/langs/all.toml and <id>.lang.toml)
plus a version cache (the output of extra/check_versions.sh) and render an HTML
fragment listing each installed language's version, compile command and run
command. The fragment is written to <ojhome>/etc/faq-langs.html for the web FAQ
page to embed.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		versions := map[int]string{}
		if data, err := os.ReadFile(langinfoVersions); err == nil {
			versions = langinfo.ParseVersionsTxt(bytes.NewReader(data))
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("读取版本缓存 %s 失败: %w", langinfoVersions, err)
		} else {
			fmt.Fprintf(os.Stderr, "提示: 版本缓存 %s 不存在，将不显示版本号\n", langinfoVersions)
		}

		entries, err := langinfo.Build(langinfoOJHome, versions)
		if err != nil {
			return err
		}
		fragment := langinfo.RenderHTML(entries)
		if err := os.WriteFile(langinfoOut, []byte(fragment), 0o644); err != nil {
			return fmt.Errorf("写入 %s 失败: %w", langinfoOut, err)
		}
		fmt.Printf("已生成 %s（%d 种语言）\n", langinfoOut, len(entries))
		return nil
	},
}

func init() {
	rootCmd.AddCommand(langinfoCmd)
	langinfoCmd.Flags().StringVar(&langinfoOJHome, "ojhome", "/home/judge", "online judge home")
	langinfoCmd.Flags().StringVar(&langinfoVersions, "versions", "",
		"version cache file (default <ojhome>/etc/lang-versions.txt)")
	langinfoCmd.Flags().StringVar(&langinfoOut, "out", "",
		"output HTML fragment (default <ojhome>/etc/faq-langs.html)")

	// 把默认值延迟到解析 ojhome 之后再补，方便只改 --ojhome 时其余路径跟着变。
	langinfoCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if langinfoVersions == "" {
			langinfoVersions = filepath.Join(langinfoOJHome, "etc", "lang-versions.txt")
		}
		if langinfoOut == "" {
			langinfoOut = filepath.Join(langinfoOJHome, "etc", "faq-langs.html")
		}
		return nil
	}
}
