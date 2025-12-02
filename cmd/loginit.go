package cmd

import (
	"log/slog"
	"os"
	"sync"
)

// 全局 logger，只会初始化一次
var (
	globalLogger *slog.Logger
	once         sync.Once
)

// Init 初始化全局 slog Logger
// 文本格式输出，如果在 systemd 下自动去掉时间戳
func Init() *slog.Logger {
	once.Do(func() {
		isSystemd := isRunningUnderSystemd()

		handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			AddSource:   true,
			ReplaceAttr: nil,
		})

		if isSystemd {
			// 去掉时间字段
			handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
				AddSource:   false,
				ReplaceAttr: removeTimeAttr,
				Level:       slog.LevelDebug,
			})
		}

		globalLogger = slog.New(handler)
		// 设置为全局默认 logger
		slog.SetDefault(globalLogger)
	})

	return globalLogger
}

// 判断是否在 systemd 下运行
func isRunningUnderSystemd() bool {
	_, ok := os.LookupEnv("INVOCATION_ID")
	return ok
}

// removeTimeAttr 用于删除时间字段
func removeTimeAttr(groups []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey {
		return slog.Attr{} // 删除时间字段
	}
	return a
}

func init() {
	Init()
}
