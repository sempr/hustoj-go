package constants

// Judge result status codes.
//
// 取值必须与 phpweb/include/const.inc.php 的 $judge_result 数组逐位对齐，
// 该数组是 solution.result 的唯一权威定义（0..17）。
const (
	OJ_WT0 = 0  // Waiting in queue                  等待
	OJ_WT1 = 1  // Rejudge waiting                    等待重判
	OJ_CI  = 2  // Compiling                          编译中
	OJ_RI  = 3  // Running                            运行并评判
	OJ_AC  = 4  // Accepted                           正确
	OJ_PE  = 5  // Presentation Error                 格式错误
	OJ_WA  = 6  // Wrong Answer                       答案错误
	OJ_TL  = 7  // Time Limit Exceeded                时间超限
	OJ_ML  = 8  // Memory Limit Exceeded              内存超限
	OJ_OL  = 9  // Output Limit Exceeded              输出超限
	OJ_RE  = 10 // Runtime Error                      运行错误
	OJ_CE  = 11 // Compile Error                      编译错误
	OJ_CO  = 12 // Compile Complete                   编译成功
	OJ_TR  = 13 // Test Run Complete                  运行完成
	OJ_MC  = 14 // Manual Check Required              自动评测通过，等待人工确认
	OJ_ST  = 15 // Submitting                         提交中
	OJ_RP  = 16 // Remote Pending                     远程等待
	OJ_RJ  = 17 // Remote Judging                     远程判题

	// OJ_SE 不属于 $judge_result 数组，是 Go 侧用于表示判题机内部故障的
	// 扩展码，不会写入 solution.result（PHP 侧无对应文案）。
	OJ_SE = 99 // System Error
)

// ojResultNames 与 $judge_result 下标一一对应，下标即结果码。
var ojResultNames = [18]string{
	"WT0", "WT1", "CI", "RI", "AC", "PE", "WA", "TL",
	"ML", "OL", "RE", "CE", "CO", "TR", "MC", "ST",
	"RP", "RJ",
}

// GetOJResultName returns the string representation of judge status
func GetOJResultName(status int) string {
	if status == OJ_SE {
		return "SE"
	}
	if status < 0 || status >= len(ojResultNames) {
		return "OT"
	}
	return ojResultNames[status]
}

// Special judge modes
const (
	OJ_SPJ_MODE_NONE        = 0  // No special judge
	OJ_SPJ_MODE_SPJ         = 1  // Special judge program
	OJ_SPJ_MODE_RAWTEXT     = 2  // Raw text comparison judge
	OJ_SPJ_MODE_INTERACTIVE = 16 // Interactive judge (interactor + player via FIFO pipes)
)

// Special judge program variants (used when OJ_SPJ_MODE_SPJ is set)
const (
	OJ_SPJ_PROGRAM_SPJ = 1 // hustoj style: infile outfile userfile
	OJ_SPJ_PROGRAM_TPJ = 2 // testlib style: infile userfile outfile
	OJ_SPJ_PROGRAM_UPJ = 3 // hustoj style with score: infile outfile userfile, return 0-100
)
