package models

// JudgeTask 描述一次不依赖数据库的独立评测任务。
// 所有输入参数通过命令行或配置传入。
type JudgeTask struct {
	// Source 是用户源码内容（优先于 SourceFile）。
	Source string
	// SourceFile 是源码文件路径，"-" 表示从标准输入读取。
	SourceFile string
	// Language 是语言 ID，对应 etc/langs/<id>.lang.toml。
	Language int
	// DataDir 是测试数据目录，内含 .in/.out 测试用例以及可选的
	// spj/tpj/upj 特殊评测程序、input.name/output.name。
	DataDir string
	// TimeLimit 是单用例时间限制（毫秒）。
	TimeLimit int
	// MemLimitKB 是内存限制（KB）。
	MemLimitKB int
	// Spj 是题目类型，取值 OJ_SPJ_MODE_NONE(0)/OJ_SPJ_MODE_SPJ(1)/
	// OJ_SPJ_MODE_RAWTEXT(2)/OJ_SPJ_MODE_INTERACTIVE(16)。
	Spj int
	// OJHome 是 judge 家目录，用于加载 etc/langs 语言配置。默认 /home/judge。
	OJHome string
	// WorkBase 是 overlay/tmpfs 挂载的基础目录，使用时在其下创建独立子目录。默认 /tmp。
	WorkBase string
}
