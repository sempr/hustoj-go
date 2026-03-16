package subtask

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/sempr/hustoj-go/pkg/constants"
)

const (
	DefaultPoints = 10.0 // 默认每个测试点的分数
	MaxPoints = 100.0 // 满分
	NoPenaltyMark = -1.0 // 无扣分标记
)

// TestResult 表示单个测试文件的结果
type TestResult struct {
	Filename string  // 测试文件名
	Score    float64 // 该测试点的分值
	Subtask  string  // 子任务标签（已弃用，建议用Filename解析）
	Result   int     // 评测结果（使用 constants.OJ_AC, constants.OJ_WA 等常量）
	SpjMark  float64 // 自定义评分（0-1之间，用于部分正确）
	Time     int     // 运行时间（毫秒）
	Mem      int     // 运行内存（KB）
}

// SubtaskScore 表示子任务的最终得分
type SubtaskScore struct {
	GetMark     float64 // 实际得分
	TotalMark   float64 // 总分
	PassRate    float64 // 通过率（0-1）
	FinalResult int     // 最终结果（使用 constants.OJ_AC, constants.OJ_WA 等常量）
}

// ExtractScoreFromFilename 从文件名提取分数，格式如：test[10].in -> 10
func ExtractScoreFromFilename(filename string) float64 {
	re := regexp.MustCompile(`\[(\d+)\]`)
	matches := re.FindStringSubmatch(filename)
	if len(matches) > 1 {
		if score, err := strconv.ParseFloat(matches[1], 64); err == nil {
			return score
		}
	}
	return DefaultPoints
}

// ParseInt 安全地解析字符串为整数（替代原有的 ToInt 函数）
func ParseInt(s string) (int, error) {
	return strconv.Atoi(s)
}

// GetSubtaskPrefix 获取子任务前缀（第一个点号前的部分）
// 例如："1_1.in" -> "1_1", "test.in" -> "test"
func GetSubtaskPrefix(filename string) string {
	if idx := strings.Index(filename, "."); idx != -1 {
		return filename[:idx]
	}
	return filename
}

// SameSubtask 判断两个文件名是否属于同一个子任务
// 子任务定义：取主编号（下划线前的部分）进行比较
// 例如："1_1" 和 "1_2" 的主编号都是 "1"，属于同一子任务
// 例如："1_1" 和 "2_1" 的主编号分别是 "1" 和 "2"，不同子任务
func SameSubtask(last, cur string) bool {
	lastPrefix := GetSubtaskPrefix(last)
	curPrefix := GetSubtaskPrefix(cur)

	// 完全相同的文件名属于同一子任务
	if lastPrefix == curPrefix {
		return true
	}

	// 提取主编号（第一个下划线前的部分）
	lastMain := lastPrefix
	if idx := strings.Index(lastPrefix, "_"); idx != -1 {
		lastMain = lastPrefix[:idx]
	}

	curMain := curPrefix
	if idx := strings.Index(curPrefix, "_"); idx != -1 {
		curMain = curPrefix[:idx]
	}

	// 主编号相同则属于同一子任务
	return lastMain == curMain
}

// CalculateOIScore 计算 OI 模式的分数（子任务模式）
// OI 模式规则：
// 1. 每个子任务包含多个测试点
// 2. 同一子任务内，只有所有测试点都通过，才能获得该子任务的全部分数
// 3. 如果子任务内有一个测试点失败，则该子任务得分为 0
// 4. 使用 SpjMark 支持部分得分（0-1之间的分数比例）
func CalculateOIScore(results []TestResult) SubtaskScore {
	if len(results) == 0 {
		return SubtaskScore{
			GetMark:     0,
			TotalMark:   0,
			PassRate:    0,
			FinalResult: constants.OJ_AC,
		}
	}

	var (
		totalMark   float64
		getMark     float64
		passRate    float64
		minusMark   = NoPenaltyMark // 当前子任务的扣分标记
		finalResult = constants.OJ_AC // 初始化为AC
		lastName    string
	)

	// 遍历所有测试结果
	for i, r := range results {
		totalMark += r.Score

		// 更新最终结果（取最差结果）
		// 规则：只要出现非AC结果，FinalResult就是该结果
		// SpjMark>0只影响得分，不影响结果状态
		if r.Result != constants.OJ_AC && finalResult == constants.OJ_AC {
			finalResult = r.Result
		}

		// 测试通过（仅 AC）
		if r.Result == constants.OJ_AC {
			// 如果是同一子任务内的后续测试点
			if i > 0 && SameSubtask(lastName, r.Filename) {
				if minusMark >= 0 {
					// 已经记录了扣分，累加扣分
					minusMark += r.Score
				} else {
					// 没有扣分记录，从已得分数中减去
					getMark -= r.Score
				}
			} else {
				// 新的子任务开始，初始化扣分
				minusMark = r.Score
			}
			getMark += r.Score
			passRate += 1.0
		} else {
			// 测试未通过
			if i > 0 && SameSubtask(lastName, r.Filename) {
				if minusMark >= 0 {
					// 回退之前添加的分数
					getMark -= minusMark
				}
			}
			// 支持部分得分（SpjMark 为 0-1 的分数比例）
			getMark += r.Score * r.SpjMark
			passRate += r.SpjMark
			minusMark = NoPenaltyMark
		}

		lastName = r.Filename
	}

	// 检查是否在文件名中指定了分数
	hasMarkInName := false
	for _, r := range results {
		if r.Score != DefaultPoints {
			hasMarkInName = true
			break
		}
	}

	// 计算通过率
	if hasMarkInName {
		// 如果文件指定了分数，按实际总分比例计算
		if totalMark > 0 {
			passRate = getMark / totalMark
		}
	} else {
		// 平均分模式
		if len(results) > 0 {
			passRate = passRate / float64(len(results))
		}
	}

	return SubtaskScore{
		GetMark:     getMark,
		TotalMark:   totalMark,
		PassRate:    passRate,
		FinalResult: finalResult,
	}
}

// CalculateNormalScore 计算普通模式的分数
// 普通模式规则：所有测试点都必须通过才算通过
func CalculateNormalScore(results []TestResult) SubtaskScore {
	if len(results) == 0 {
		return SubtaskScore{
			GetMark:     0,
			TotalMark:   MaxPoints,
			PassRate:    0,
			FinalResult: constants.OJ_AC,
		}
	}

	// 检查是否所有测试都通过（仅 AC）
	finalResult := constants.OJ_AC
	for _, r := range results {
		if r.Result != constants.OJ_AC {
			finalResult = r.Result
			break
		}
	}

	// 通过率：全部通过为1，否则为0
	passRate := 0.0
	if finalResult == constants.OJ_AC {
		passRate = 1.0
	}

	return SubtaskScore{
		GetMark:     passRate * MaxPoints,
		TotalMark:   MaxPoints,
		PassRate:    passRate,
		FinalResult: finalResult,
	}
}

// Judge 根据是否启用 OI 模式计算最终得分
// oiMode: true=OI模式（子任务评分），false=普通模式（全部通过才算通过）
func Judge(results []TestResult, oiMode bool) SubtaskScore {
	if oiMode {
		return CalculateOIScore(results)
	}
	return CalculateNormalScore(results)
}

// TestResultSlice 用于排序的切片类型
type TestResultSlice []TestResult

func (p TestResultSlice) Len() int           { return len(p) }
func (p TestResultSlice) Less(i, j int) bool { return p[i].Filename < p[j].Filename }
func (p TestResultSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// SortResults 按文件名排序测试结果
func SortResults(results []TestResult) []TestResult {
	if len(results) == 0 {
		return []TestResult{}
	}
	sorted := make([]TestResult, len(results))
	copy(sorted, results)
	sort.Sort(TestResultSlice(sorted))
	return sorted
}

// GetResultName 返回评测结果的字符串表示
func GetResultName(result int) string {
	return constants.GetOJResultName(result)
}

// GenerateMarkdownReport 按测试分组生成 Markdown 格式的详细报告
// 报告包含：总体概览、每个分组的测试详情、得分汇总
// problemTitle: 题目名称（可留空）
// testResults: 所有测试点的结果
// score: 计算后的最终分数（包含 GetMark, TotalMark, PassRate, FinalResult）
func GenerateMarkdownReport(problemTitle string, testResults []TestResult, score SubtaskScore) string {
	if len(testResults) == 0 {
		return "# 测试报告\n\n无测试数据\n"
	}

	// 收集需要的数据
	getMark := score.GetMark
	totalMark := score.TotalMark
	passRate := score.PassRate
	finalResult := score.FinalResult

	// 按主编号（第一个点号前的部分）分组，如 taskb.8 -> taskb
	groups := make(map[string][]TestResult)
	for _, result := range testResults {
		var groupKey string
		filename := result.Filename

		// 移除 .in 扩展名
		if strings.HasSuffix(filename, ".in") {
			filename = filename[:len(filename)-3]
		}

		// 提取主编号（第一个点号前的部分）
		if idx := strings.Index(filename, "."); idx != -1 {
			groupKey = filename[:idx]
		} else {
			groupKey = filename
		}

		groups[groupKey] = append(groups[groupKey], result)
	}

	// 排序组名
	var groupNames []string
	for name := range groups {
		groupNames = append(groupNames, name)
	}
	sort.Strings(groupNames)

	// 构建 Markdown
	var report strings.Builder

	// 标题
	if problemTitle != "" {
		report.WriteString(fmt.Sprintf("# 测试报告: %s\n\n", problemTitle))
	} else {
		report.WriteString("# 测试报告\n\n")
	}

	// 总体概览
	report.WriteString("## 📊 总体概览\n\n")
	report.WriteString(fmt.Sprintf("| 项目 | 结果 |\n"))
	report.WriteString(fmt.Sprintf("|------|------|\n"))
	report.WriteString(fmt.Sprintf("| **最终得分** | %.2f / %.2f (%.2f%%) |\n", getMark, totalMark, passRate*100))
	report.WriteString(fmt.Sprintf("| **最终结果** | %s |\n", GetResultName(finalResult)))
	report.WriteString(fmt.Sprintf("| **测试点总数** | %d 个 |\n", len(testResults)))

	// 统计通过数
	passedCount := 0
	for _, r := range testResults {
		if r.Result == constants.OJ_AC {
			passedCount++
		}
	}
	report.WriteString(fmt.Sprintf("| **通过数** | %d 个 |\n", passedCount))
	report.WriteString(fmt.Sprintf("| **测试分组数** | %d 组 |\n", len(groups)))
	report.WriteString(fmt.Sprintf("| **测试模式** | %s |\n", map[bool]string{true: "OI模式（子任务评分）", false: "普通模式（全部通过才算）"}[score.TotalMark > DefaultPoints]))
	report.WriteString("\n")

	// 只有 OI 模式才显示分组详情
	if score.TotalMark > DefaultPoints || len(groups) > 1 {
		// 每个分组详情
		report.WriteString("## 📝 分组详情\n\n")

		// 计算总分
		var grandTotalGetMark float64
		var grandTotalMark float64

		for _, groupName := range groupNames {
			groupResults := groups[groupName]

			// 计算该组总分
			var groupTotalMark float64
			var groupGetMark float64
			var groupTests int
			var groupPassed int

			for _, r := range groupResults {
				groupTotalMark += r.Score
				if r.Result == constants.OJ_AC {
					groupGetMark += r.Score
					groupPassed++
				}
				groupTests++
			}

			report.WriteString(fmt.Sprintf("### 组 %s\n\n", groupName))
			report.WriteString(fmt.Sprintf("**小计**: %.2f / %.2f (%.2f%%) - %d/%d 通过\n\n",
				groupGetMark, groupTotalMark, groupGetMark/groupTotalMark*100,
				groupPassed, groupTests))

			// 测试详情表格
			report.WriteString("| 测试文件 | 分值 | 结果 | 耗时 | 内存 |\n")
			report.WriteString("|----------|------|------|------|------|\n")

			for _, r := range groupResults {
				resultName := GetResultName(r.Result)
				status := "✅ 通过"
				if r.Result != constants.OJ_AC {
					status = "❌ " + resultName
				}

				report.WriteString(fmt.Sprintf("| %s | %.0f | %s | %dms | %dKB |\n",
					r.Filename, r.Score, status, r.Time, r.Mem))
			}

			report.WriteString("\n")

			grandTotalGetMark += groupGetMark
			grandTotalMark += groupTotalMark
		}

		// 最终汇总
		if len(groups) > 1 {
			report.WriteString("---\n")
			report.WriteString(fmt.Sprintf("**总分**: %.2f / %.2f (%.2f%%)\n",
				grandTotalGetMark, grandTotalMark, grandTotalGetMark/grandTotalMark*100))
		}
	} else if len(groups) == 1 {
		// 只有一个分组，直接列出所有测试
		report.WriteString("## 📝 测试详情\n\n")
		report.WriteString("| 测试文件 | 分值 | 结果 | 耗时 | 内存 |\n")
		report.WriteString("|----------|------|------|------|------|\n")

		for _, r := range testResults {
			resultName := GetResultName(r.Result)
			status := "✅ 通过"
			if r.Result != constants.OJ_AC {
				status = "❌ " + resultName
			}

			report.WriteString(fmt.Sprintf("| %s | %.0f | %s | %dms | %dKB |\n",
				r.Filename, r.Score, status, r.Time, r.Mem))
		}
		report.WriteString("\n")
	}

	// 最终汇总
	report.WriteString("---\n")
	report.WriteString(fmt.Sprintf("**最终得分**: %.2f / %.2f (%.2f%%)\n",
		getMark, totalMark, passRate*100))
	report.WriteString(fmt.Sprintf("**最终结果**: %s\n", GetResultName(finalResult)))

	return report.String()
}
