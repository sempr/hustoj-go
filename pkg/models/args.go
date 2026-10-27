package models

type SandboxArgs struct {
	Command     string
	Rootfs      string
	Workdir     string
	Stdin       string
	Stdout      string
	Stderr      string
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
