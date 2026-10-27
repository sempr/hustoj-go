package main

import (
	"fmt"
	"io"
	"os"

	"github.com/pelletier/go-toml/v2" // 导入 TOML 库
)

type Config struct {
	Name string  `toml:"name"`
	Fs   FsInfo  `toml:"fs"`
	Cmd  CmdInfo `toml:"cmd"`
}

type FsInfo struct {
	Base    string `toml:"base"`
	Workdir string `toml:"workdir"` // 库会自动解析 ISO 8601 时间字符串
}

type CmdInfo struct {
	Compile string `toml:"compile"`
	Run     string `toml:"run"`
	Ver     string `toml:"ver"`
}

func main() {
	fname := os.Args[1]
	fmt.Println("file: ", fname)
	file, err := os.Open(fname)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	// 3. 创建一个 Config 变量来接收解析后的数据
	var config Config
	b, err := io.ReadAll(file)
	if err != nil {
		panic(err)
	}
	err = toml.Unmarshal(b, &config)
	if err != nil {
		panic(err)
	}
	// 5. 打印结果
	fmt.Println("--- 解析成功 ---")
	fmt.Printf("Title: %s\n", config.Name)
	fmt.Printf("Owner Fs: %s\n", config.Fs.Base)
	fmt.Printf("Owner Workdir: %s\n", config.Fs.Workdir)
	fmt.Printf("Cmd Compile: %s\n", config.Cmd.Compile)
	fmt.Printf("Cmd Run: %s\n", config.Cmd.Run)
	fmt.Printf("Cmd Ver: %s\n", config.Cmd.Ver)

	// 也可以使用 %+v 完整打印 struct
	fmt.Println("\n--- 完整 Struct ---")
	fmt.Printf("%+v\n", config)
}
