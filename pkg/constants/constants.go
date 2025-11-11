package constants

const (
	OJ_WT0 = 0  // 提交排队
	OJ_WT1 = 1  // 重判排队
	OJ_CI  = 2  // 编译中
	OJ_RI  = 3  // 运行中
	OJ_AC  = 4  // 答案正确
	OJ_PE  = 5  // 格式错误
	OJ_WA  = 6  // 答案错误
	OJ_TL  = 7  // 时间超限
	OJ_ML  = 8  // 内存超限
	OJ_OL  = 9  // 输出超限
	OJ_RE  = 10 // 运行错误
	OJ_CE  = 11 // 编译错误
	OJ_CO  = 12 // 编译完成
	OJ_TR  = 13 // 测试运行结束
	OJ_MC  = 14 // 等待裁判手工确认
)

func GetOJResultName(status int) string {
	var names = []string{"WT0", "WT1", "CI", "RI", "AC", "PE", "WA", "TL", "ML", "OL", "RE", "CE", "CO", "TR", "MC"}
	if status < 0 || status >= len(names) {
		return "OT"
	}
	return names[status]
}
