package models

// SandboxOutput 是 sandbox 进程的输出结果，通过 JSON 格式在进程间传递。
type SandboxOutput struct {
	// UserStatus 是用户程序的状态，例如 OJ_AC, OJ_WA, OJ_RE 等。
	UserStatus int `json:"user_status"`
	// ExitStatus 是用户程序的退出码。
	ExitStatus int `json:"exit_status"`
	// Time 是用户程序消耗的时间（毫秒）。
	Time int `json:"time"`
	// Memory 是用户程序消耗的内存（KB）。
	Memory int `json:"memory"`
	// CombinedOutput 包含了用户程序的 stdout 和 stderr 的合并输出。
	// 主要用于编译错误信息的捕获。
	CombinedOutput string `json:"combined_output"`
	// ProcessCnt 是沙箱中创建的总进程数。
	ProcessCnt int `json:"process_cnt"`
	// ExitSignal 是导致程序终止的信号（如果有）。
	ExitSignal string `json:"exit_signal"`
}

// OneResult 存储单个测试点的判题结果。
type OneResult struct {
	Datafile string `json:"datafile"`
	Result   int    `json:"result"`
	Time     int    `json:"time"`
	Mem      int    `json:"mem"`
}

// TotalResults 聚合所有测试点的结果以及最终的判题结果。
type TotalResults struct {
	Results     []OneResult `json:"results"`
	FinalResult int         `json:"final_result"`
}
