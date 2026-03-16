package subtask

import (
	"fmt"
	"testing"

	"github.com/sempr/hustoj-go/pkg/constants"
)

func TestExtractScoreFromFilename(t *testing.T) {
	tests := []struct {
		filename string
		expected float64
	}{
		{"1.in[10]", 10},
		{"2.in[20]", 20},
		{"1.1.in[5]", 5},
		{"test.in", 10},
		{"abc.in[100]", 100},
	}

	for _, tt := range tests {
		result := ExtractScoreFromFilename(tt.filename)
		if result != tt.expected {
			t.Errorf("ExtractScoreFromFilename(%s) = %v; want %v", tt.filename, result, tt.expected)
		}
	}
}

func TestGetSubtaskPrefix(t *testing.T) {
	tests := []struct {
		filename string
		expected string
	}{
		{"1.in", "1"},
		{"1.1.in", "1.1"},
		{"10.in", "10"},
		{"test", "test"},
	}

	for _, tt := range tests {
		result := GetSubtaskPrefix(tt.filename)
		if result != tt.expected {
			t.Errorf("GetSubtaskPrefix(%s) = %v; want %v", tt.filename, result, tt.expected)
		}
	}
}

func TestSameSubtask(t *testing.T) {
	tests := []struct {
		last     string
		cur      string
		expected bool
	}{
		{"1.in", "1.in", true},           // 主编号 1 == 1
		{"1.in", "1.1.in", true},         // 主编号 1 == 1
		{"1.in", "2.in", false},          // 主编号 1 != 2
		{"10.in", "10.in", true},         // 主编号 10 == 10
		 {"10.in", "10.1.in", true},       // 主编号 10 == 10
		{"1.1.in", "1.2.in", true},       // 主编号 1 == 1
		{"1.1.in", "1.3.in", true},       // 主编号 1 == 1
		{"1.1.in", "2.1.in", false},      // 主编号 1 != 2
		{"test.1.in", "test.2.in", true}, // 主编号 test == test
	}

	for _, tt := range tests {
		result := SameSubtask(tt.last, tt.cur)
		if result != tt.expected {
			t.Errorf("SameSubtask(%s, %s) = %v; want %v", tt.last, tt.cur, result, tt.expected)
		}
	}
}

func TestCalculateOIScoreSubtask(t *testing.T) {
	results := []TestResult{
		{Filename: "1.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
		{Filename: "2.in[10]", Score: 10, Result: constants.OJ_WA, SpjMark: 0.5},
		{Filename: "3.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
		{Filename: "4.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
	}

	score := CalculateOIScore(results)
	fmt.Printf("GetMark: %.2f, TotalMark: %.2f, PassRate: %.2f, FinalResult: %s\n",
		score.GetMark, score.TotalMark, score.PassRate, GetResultName(score.FinalResult))

	if score.GetMark != 35 {
		t.Errorf("GetMark = %v; want 35", score.GetMark)
	}
	if score.FinalResult != constants.OJ_WA {
		t.Errorf("FinalResult = %v; want %v (WA)", score.FinalResult, constants.OJ_WA)
	}
}

func TestCalculateOIScoreAllAC(t *testing.T) {
	results := []TestResult{
		{Filename: "1.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
		{Filename: "1.1.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
		{Filename: "2.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
		{Filename: "2.1.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
	}

	score := CalculateOIScore(results)
	fmt.Printf("All AC - GetMark: %.2f, TotalMark: %.2f, PassRate: %.2f, FinalResult: %s\n",
		score.GetMark, score.TotalMark, score.PassRate, GetResultName(score.FinalResult))

	if score.FinalResult != constants.OJ_AC {
		t.Errorf("FinalResult = %v; want %v (AC)", score.FinalResult, constants.OJ_AC)
	}
	if score.GetMark != 40 {
		t.Errorf("GetMark = %v; want 40", score.GetMark)
	}
}

func TestCalculateOIScoreSubtaskFail(t *testing.T) {
	results := []TestResult{
		{Filename: "1.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
		{Filename: "1.1.in[10]", Score: 10, Result: constants.OJ_WA, SpjMark: 0}, // 同一子任务，导致子任务1失败
		{Filename: "2.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},  // 子任务2通过
	}

	score := CalculateOIScore(results)
	fmt.Printf("Subtask Fail - GetMark: %.2f, TotalMark: %.2f, PassRate: %.2f, FinalResult: %s\n",
		score.GetMark, score.TotalMark, score.PassRate, GetResultName(score.FinalResult))

	// 结果：子任务1得0分（因为1_1失败，回退1的10分），子任务2得10分，总分10分
	if score.GetMark != 10 {
		t.Errorf("GetMark = %v; want 10 (0 from subtask1 + 10 from subtask2)", score.GetMark)
	}
}

func TestCalculateNormalScore(t *testing.T) {
	results := []TestResult{
		{Filename: "1.in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
		{Filename: "2.in", Score: 10, Result: constants.OJ_WA, SpjMark: 0},
	}

	score := CalculateNormalScore(results)
	fmt.Printf("GetMark: %.2f, TotalMark: %.2f, PassRate: %.2f, FinalResult: %s\n",
		score.GetMark, score.TotalMark, score.PassRate, GetResultName(score.FinalResult))

	if score.FinalResult != constants.OJ_WA {
		t.Errorf("FinalResult = %v; want %v (WA)", score.FinalResult, constants.OJ_WA)
	}
	if score.PassRate != 0.0 {
		t.Errorf("PassRate = %v; want 0.0", score.PassRate)
	}
}

func TestCalculateOIScoreMixedMarking(t *testing.T) {
	// 场景：9个测试点，4个标注了10分，5个未标注（默认10分），通过了7个
	// 验证了混合分数标记场景的处理
	results := []TestResult{
		{Filename: "test1[10].in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},   // 标注，通过
		{Filename: "test2[10].in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},   // 标注，通过
		{Filename: "test3[10].in", Score: 10, Result: constants.OJ_WA, SpjMark: 0},   // 标注，失败
		{Filename: "test4[10].in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},   // 标注，通过
		{Filename: "test5.in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},       // 未标注，通过
		{Filename: "test6.in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},       // 未标注，通过
		{Filename: "test7.in", Score: 10, Result: constants.OJ_WA, SpjMark: 0},       // 未标注，失败
		{Filename: "test8.in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},       // 未标注，通过
		{Filename: "test9.in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},       // 未标注，通过
	}

	score := CalculateOIScore(results)
	fmt.Printf("Mixed Marking - 9 tests, 4 marked[10], 5 unmarked, 7 passed\n")
	fmt.Printf("GetMark: %.2f, TotalMark: %.2f, PassRate: %.2f, FinalResult: %s\n",
		score.GetMark, score.TotalMark, score.PassRate, GetResultName(score.FinalResult))

	// 验证结果
	// hasMarkInName = true（有标注分数），且 totalMark = 90
	// passRate = getMark / totalMark = 70 / 90 ≈ 0.777...
	if score.GetMark != 70 {
		t.Errorf("GetMark = %v; want 70 (7 tests passed, each 10 points)", score.GetMark)
	}
	if score.TotalMark != 90 {
		t.Errorf("TotalMark = %v; want 90 (9 tests, each 10 points)", score.TotalMark)
	}
	expectedPassRate := 70.0 / 90.0
	if score.PassRate != expectedPassRate {
		t.Errorf("PassRate = %v; want %.4f (70/90)", score.PassRate, expectedPassRate)
	}
	if score.FinalResult != constants.OJ_WA {
		t.Errorf("FinalResult = %v; want %v (WA, because test3 and test7 failed)", score.FinalResult, constants.OJ_WA)
	}
}

func TestCalculateOIScoreMixedMarkingAllPass(t *testing.T) {
	// 场景：9个测试点，4个标注了10分，5个未标注，全部通过
	// 验证了混合标注但全部通过的场景
	results := []TestResult{
		{Filename: "test1[10].in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},   // 标注，通过
		{Filename: "test2[10].in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},   // 标注，通过
		{Filename: "test3[10].in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},   // 标注，通过
		{Filename: "test4[10].in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},   // 标注，通过
		{Filename: "test5.in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},       // 未标注，通过
		{Filename: "test6.in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},       // 未标注，通过
		{Filename: "test7.in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},       // 未标注，通过
		{Filename: "test8.in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},       // 未标注，通过
		{Filename: "test9.in", Score: 10, Result: constants.OJ_AC, SpjMark: 0},       // 未标注，通过
	}

	score := CalculateOIScore(results)
	fmt.Printf("Mixed Marking All Pass - 9 tests (4 marked, 5 unmarked), all passed\n")
	fmt.Printf("GetMark: %.2f, TotalMark: %.2f, PassRate: %.2f, FinalResult: %s\n",
		score.GetMark, score.TotalMark, score.PassRate, GetResultName(score.FinalResult))

	// 验证结果
	// hasMarkInName = true（因为有标注）
	// totalMark = 90，getMark = 90
	// passRate = getMark / totalMark = 90 / 90 = 1.0
	if score.GetMark != 90 {
		t.Errorf("GetMark = %v; want 90 (all 9 tests passed, each 10 points)", score.GetMark)
	}
	if score.TotalMark != 90 {
		t.Errorf("TotalMark = %v; want 90 (9 tests, each 10 points)", score.TotalMark)
	}
	if score.PassRate != 1.0 {
		t.Errorf("PassRate = %v; want 1.0 (90/90)", score.PassRate)
	}
	if score.FinalResult != constants.OJ_AC {
		t.Errorf("FinalResult = %v; want %v (AC, because all tests passed)", score.FinalResult, constants.OJ_AC)
	}
}

func TestCalculateOIScoreTotalMarkExceeds100(t *testing.T) {
	// 场景：11个测试点，每个标注20分，总分220分（>100），通过了8个
	// 验证了当总分超过MaxPoints（100）时的处理
	results := []TestResult{
		{Filename: "test1[20].in", Score: 20, Result: constants.OJ_AC, SpjMark: 0},   // 通过
		{Filename: "test2[20].in", Score: 20, Result: constants.OJ_AC, SpjMark: 0},   // 通过
		{Filename: "test3[20].in", Score: 20, Result: constants.OJ_WA, SpjMark: 0},   // 失败
		{Filename: "test4[20].in", Score: 20, Result: constants.OJ_AC, SpjMark: 0},   // 通过
		{Filename: "test5[20].in", Score: 20, Result: constants.OJ_AC, SpjMark: 0},   // 通过
		{Filename: "test6[20].in", Score: 20, Result: constants.OJ_AC, SpjMark: 0},   // 通过
		{Filename: "test7[20].in", Score: 20, Result: constants.OJ_WA, SpjMark: 0},   // 失败
		{Filename: "test8[20].in", Score: 20, Result: constants.OJ_AC, SpjMark: 0},   // 通过
		{Filename: "test9[20].in", Score: 20, Result: constants.OJ_AC, SpjMark: 0},   // 通过
		{Filename: "test10[20].in", Score: 20, Result: constants.OJ_AC, SpjMark: 0},  // 通过
		{Filename: "test11[20].in", Score: 20, Result: constants.OJ_AC, SpjMark: 0},  // 通过
	}

	score := CalculateOIScore(results)
	fmt.Printf("TotalMark Exceeds 100 - 11 tests @ 20 pts each = 220 total, 8 passed -> 160 pts\n")
	fmt.Printf("GetMark: %.2f, TotalMark: %.2f, PassRate: %.2f, FinalResult: %s\n",
		score.GetMark, score.TotalMark, score.PassRate, GetResultName(score.FinalResult))

	// 验证结果
	// totalMark = 220 > MaxPoints = 100
	// passRate = getMark / totalMark = 160 / 220 = 0.727...
	if score.GetMark != 180 {
		t.Errorf("GetMark = %v; want 180 (9 tests passed, each 20 points)", score.GetMark)
	}
	if score.TotalMark != 220 {
		t.Errorf("TotalMark = %v; want 220 (11 tests, each 20 points)", score.TotalMark)
	}
	// Allow small floating point tolerance
	expectedPassRate := 180.0 / 220.0
	if score.PassRate != expectedPassRate {
		t.Errorf("PassRate = %v; want %.4f (180/220)", score.PassRate, expectedPassRate)
	}
	if score.FinalResult != constants.OJ_WA {
		t.Errorf("FinalResult = %v; want %v (WA, because test3 and test7 failed)", score.FinalResult, constants.OJ_WA)
	}
}

func TestJudge(t *testing.T) {
	results := []TestResult{
		{Filename: "1.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
		{Filename: "1.1.in[10]", Score: 10, Result: constants.OJ_AC, SpjMark: 0},
		{Filename: "2.in[10]", Score: 10, Result: constants.OJ_WA, SpjMark: 0},
	}

	oiScore := Judge(results, true)
	normalScore := Judge(results, false)

	fmt.Printf("OI Mode - GetMark: %.2f, PassRate: %.2f, FinalResult: %s\n",
		oiScore.GetMark, oiScore.PassRate, GetResultName(oiScore.FinalResult))
	fmt.Printf("Normal Mode - GetMark: %.2f, PassRate: %.2f, FinalResult: %s\n",
		normalScore.GetMark, normalScore.PassRate, GetResultName(normalScore.FinalResult))

	// 验证 OI 模式得分
	if oiScore.GetMark != 20 {
		t.Errorf("OI Mode GetMark = %v; want 20", oiScore.GetMark)
	}
	if oiScore.FinalResult != constants.OJ_WA {
		t.Errorf("OI Mode FinalResult = %v; want %v", oiScore.FinalResult, constants.OJ_WA)
	}

	// 验证普通模式得分
	if normalScore.GetMark != 0 {
		t.Errorf("Normal Mode GetMark = %v; want 0", normalScore.GetMark)
	}
	if normalScore.FinalResult != constants.OJ_WA {
		t.Errorf("Normal Mode FinalResult = %v; want %v", normalScore.FinalResult, constants.OJ_WA)
	}
}

func TestGenerateMarkdownReport(t *testing.T) {
	t.Run("generate empty test results", func(t *testing.T) {
		testResults := []TestResult{}
		score := SubtaskScore{GetMark: 0, TotalMark: 0, PassRate: 0, FinalResult: constants.OJ_AC}

		report := GenerateMarkdownReport("", testResults, score)

		if !contains(report, "无测试数据") {
			t.Errorf("Expected '无测试数据' in report, got: %s", report)
		}
	})

	t.Run("generate report with single test", func(t *testing.T) {
		testResults := []TestResult{
			{Filename: "test1.in", Score: 10, Result: constants.OJ_AC, Time: 45, Mem: 1024},
		}
		score := SubtaskScore{GetMark: 10, TotalMark: 100, PassRate: 0.1, FinalResult: constants.OJ_AC}

		report := GenerateMarkdownReport("TEST-1", testResults, score)

		if !contains(report, "测试报告: TEST-1") {
			t.Errorf("Expected '测试报告: TEST-1' in report, got: %s", report)
		}
		if !contains(report, "最终得分") {
			t.Errorf("Expected '最终得分' section in report, got: %s", report)
		}
	})

	t.Run("generate report with multiple tests in normal mode", func(t *testing.T) {
		testResults := []TestResult{
			{Filename: "1.in", Score: 10, Result: constants.OJ_AC, Time: 45, Mem: 1024},
			{Filename: "2.in", Score: 10, Result: constants.OJ_WA, Time: 32, Mem: 512},
			{Filename: "3.in", Score: 10, Result: constants.OJ_AC, Time: 50, Mem: 2048},
			{Filename: "4.in", Score: 10, Result: constants.OJ_AC, Time: 48, Mem: 1024},
			{Filename: "5.in", Score: 10, Result: constants.OJ_AC, Time: 52, Mem: 1536},
		}
		score := SubtaskScore{GetMark: 40, TotalMark: 100, PassRate: 0.8, FinalResult: constants.OJ_WA}

		report := GenerateMarkdownReport("Multi-Test", testResults, score)

		if !contains(report, "40.00 / 100.00 (80.00%)") {
			t.Errorf("Expected '40.00 / 100.00 (80.00%%)' in report, got: %s", report)
		}
		if !contains(report, "❌ WA") {
			t.Errorf("Expected '❌ WA' in report, got: %s", report)
		}
		if !contains(report, "✅ 通过") {
			t.Errorf("Expected '✅ 通过' in report, got: %s", report)
		}
	})

	t.Run("generate report with OI mode subtasks", func(t *testing.T) {
		testResults := []TestResult{
			{Filename: "1.in[10]", Score: 10, Result: constants.OJ_AC, Time: 45, Mem: 1024},
			{Filename: "2.in[20]", Score: 20, Result: constants.OJ_AC, Time: 32, Mem: 512},
			{Filename: "3.in[30]", Score: 30, Result: constants.OJ_WA, Time: 50, Mem: 2048},
			{Filename: "3.1.in[10]", Score: 10, Result: constants.OJ_AC, Time: 20, Mem: 1024, SpjMark: 0.5},
		}
		score := SubtaskScore{GetMark: 35, TotalMark: 105, PassRate: 35.0/105.0, FinalResult: constants.OJ_WA}

		report := GenerateMarkdownReport("OI-Mode", testResults, score)

		if !contains(report, "## 📊 总体概览") {
			t.Errorf("Expected '总体概览' section in report, got: %s", report)
		}
		if !contains(report, "## 📝 分组详情") {
			t.Errorf("Expected '分组详情' section in report, got: %s", report)
		}
	})

	t.Run("generate report with spj marks", func(t *testing.T) {
		testResults := []TestResult{
			{Filename: "a.in[10]", Score: 10, Result: constants.OJ_AC, Time: 45, Mem: 1024},
			{Filename: "b.in[10]", Score: 10, Result: constants.OJ_WA, Time: 32, Mem: 512, SpjMark: 0.3},
			{Filename: "c.in[10]", Score: 10, Result: constants.OJ_WA, Time: 50, Mem: 2048, SpjMark: 0.6},
		}
		score := SubtaskScore{GetMark: 19, TotalMark: 30, PassRate: 19.0/30.0, FinalResult: constants.OJ_WA}

		report := GenerateMarkdownReport("SPJ-Mode", testResults, score)

		if !contains(report, "19.00 / 30.00 (63.33%)") {
			t.Errorf("Expected '19.00 / 30.00 (63.33%%)' in report, got: %s", report)
		}
	})

	t.Run("generate report with duplicate subtask files", func(t *testing.T) {
		testResults := []TestResult{
			{Filename: "task1.1.in[10]", Score: 10, Result: constants.OJ_AC, Time: 45, Mem: 1024},
			{Filename: "task1.2.in[10]", Score: 10, Result: constants.OJ_AC, Time: 40, Mem: 1024},
			{Filename: "task1.3.in[10]", Score: 10, Result: constants.OJ_AC, Time: 42, Mem: 1024},
			{Filename: "task2.1.in[15]", Score: 15, Result: constants.OJ_AC, Time: 50, Mem: 2048},
			{Filename: "task2.2.in[15]", Score: 15, Result: constants.OJ_AC, Time: 48, Mem: 2048},
		}
		score := SubtaskScore{GetMark: 50, TotalMark: 60, PassRate: 50.0/60.0, FinalResult: constants.OJ_AC}

		report := GenerateMarkdownReport("多组子任务", testResults, score)

		if !contains(report, "### 组 task1") {
			t.Errorf("Expected '### 组 task1' in report, got: %s", report)
		}
		if !contains(report, "### 组 task2") {
			t.Errorf("Expected '### 组 task2' in report, got: %s", report)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && containsRune(s, substr)
}

func containsRune(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
