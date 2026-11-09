package models

type SandboxArgs struct {
	Command string
	Rootfs  string
	Workdir string
	Stdin   string
	Stdout  string
	Stderr  string
	// InheritIO 为 true 时不再按路径打开 Stdin/Stdout，而是直接使用调用方
	// 通过 ExtraFiles 传入的 fd 4（stdin）和 fd 5（stdout）。交互评测用它
	// 传递匿名管道，避免在 rootfs 里落 FIFO 文件。
	InheritIO   bool
	Stage       string
	TimeLimit   int
	MemoryLimit int
	SolutionId  int
}

type DaemonArgs struct {
	OJHome string
	Debug  bool
	Once   bool
}

// WebArgs 是 `hustoj web` 的启动参数。
type WebArgs struct {
	// OJHome 是 OJ 根目录，用于定位 etc/web.toml 与 data/。
	OJHome string
	// Config 是 web.toml 路径；为空时取 <OJHome>/etc/web.toml。
	Config string
	Debug  bool
}
