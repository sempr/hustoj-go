package models

type SandboxArgs struct {
	Command     string
	Rootfs      string
	Workdir     string
	Stdin       string
	Stdout      string
	Stderr      string
	TimeLimit   int
	MemoryLimit int
	SolutionId  int
}

type DaemonArgs struct {
	OJHome string
	Debug  bool
	Once   bool
}
