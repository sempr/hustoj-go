package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode"
)

func compareFiles(file1Path, file2Path string) (int, error) {
	// 尝试以规则 0 比较文件
	sameRule0, err := compareFilesRule0(file1Path, file2Path)
	if err != nil {
		return 2, err
	}
	if sameRule0 {
		return 0, nil
	}

	// 尝试以规则 1 比较文件
	sameRule1, err := compareFilesRule1(file1Path, file2Path)
	if err != nil {
		return 2, nil // Rule 1 不应返回错误，只要文件能读
	}
	if sameRule1 {
		return 1, nil
	}

	return 2, nil
}

// compareFilesRule0 比较文件，忽略行尾的空格和\t，忽略文件尾部的多个空行。
func compareFilesRule0_(file1Path, file2Path string) (bool, error) {
	f1, err := os.Open(file1Path)
	if err != nil {
		return false, err
	}
	defer f1.Close()

	f2, err := os.Open(file2Path)
	if err != nil {
		return false, err
	}
	defer f2.Close()

	scanner1 := bufio.NewScanner(f1)
	scanner2 := bufio.NewScanner(f2)

	for {
		// 获取两个文件的下一行（特殊处理EOF和尾部空行）
		line1, eof1, err1 := scanNextLineStrict(scanner1)
		line2, eof2, err2 := scanNextLineStrict(scanner2)

		if err1 != nil || err2 != nil {
			return false, fmt.Errorf("error reading files: %v, %v", err1, err2)
		}

		// 如果两个都到达有效内容的末尾，则返回 true
		if eof1 && eof2 {
			return true, nil
		}

		// 如果一个结束，另一个还有有效内容，则不匹配
		if eof1 || eof2 {
			return false, nil
		}

		// 比较处理后的行内容
		if line1 != line2 {
			return false, nil
		}
	}
}

// scanNextLineStrict 扫描下一行有效内容。它会处理行尾的空格/\t，并标记是否已到达文件的有效内容末尾。
func scanNextLineStrict(scanner *bufio.Scanner) (string, bool, error) {
	// 存储所有扫描到的行，以便在尾部回退
	var lines []string

	for scanner.Scan() {
		line := scanner.Text()
		// 只去除行尾的空格和 \t
		trimmedLine := strings.TrimRightFunc(line, func(r rune) bool {
			return r == ' ' || r == '\t'
		})
		lines = append(lines, trimmedLine)
	}

	if err := scanner.Err(); err != nil {
		return "", false, err
	}

	// 现在处理文件尾部的空行
	// 从后往前查找第一个非空行
	for i := len(lines) - 1; i >= 0; i-- {
		// 如果找到非空行，返回该行内容，并标记仍有内容（非EOF）
		if lines[i] != "" {
			// 将未处理的行合并成一行（因为我们是逐行比较，所以这里逻辑需要调整）
			// 实际上，上面的循环需要放在 compareFilesRule0 中，逐行处理并比较。

			// 重新设计 scanNextLineStrict，使其只返回单行，并在 compareFilesRule0 中处理尾部逻辑。
			// 这个辅助函数的设计目标无法满足需求，回归到 compareFilesRule0 使用更精细的控制。
			break
		}
	}

	// 放弃上面的辅助函数，使用更直接的逻辑
	return "", true, nil // Placeholder
}

// --- 规则 0 的新辅助函数实现 ---

// lineScanner 结构体用于管理文件扫描，并支持忽略尾部空行的逻辑
type lineScanner struct {
	scanner *bufio.Scanner
	lines   []string // 存储所有已扫描的行（去除行尾空格/\t）
	cursor  int      // 当前读取到的行索引
	eof     bool
}

func newLineScanner(f *os.File) *lineScanner {
	return &lineScanner{
		scanner: bufio.NewScanner(f),
		cursor:  0,
		eof:     false,
	}
}

// nextLine 获取下一行有效内容。
// 返回 (行内容, 是否到达有效内容的EOF, 错误)
func (ls *lineScanner) nextLine() (string, bool, error) {
	if ls.cursor < len(ls.lines) {
		line := ls.lines[ls.cursor]
		ls.cursor++
		return line, false, nil
	}

	if ls.eof {
		return "", true, nil
	}

	// 扫描所有行到内存中，以便准确判断尾部空行
	for ls.scanner.Scan() {
		line := ls.scanner.Text()
		// 只去除行尾的空格和 \t
		trimmedLine := strings.TrimRightFunc(line, func(r rune) bool {
			return r == ' ' || r == '\t'
		})
		ls.lines = append(ls.lines, trimmedLine)
	}

	if err := ls.scanner.Err(); err != nil {
		return "", false, err
	}

	ls.eof = true

	// 标记文件内容结束点
	lastMeaningfulIndex := -1
	for i := len(ls.lines) - 1; i >= 0; i-- {
		if ls.lines[i] != "" {
			lastMeaningfulIndex = i
			break
		}
	}

	// 如果整个文件都是空的或者只有空行
	if lastMeaningfulIndex == -1 {
		return "", true, nil
	}

	// 截断尾部的空行
	ls.lines = ls.lines[:lastMeaningfulIndex+1]

	// 递归调用自身以返回第一行
	return ls.nextLine()
}

// compareFilesRule0 的新实现，使用 lineScanner
func compareFilesRule0(file1Path, file2Path string) (bool, error) {
	f1, err := os.Open(file1Path)
	if err != nil {
		return false, err
	}
	defer f1.Close()

	f2, err := os.Open(file2Path)
	if err != nil {
		return false, err
	}
	defer f2.Close()

	ls1 := newLineScanner(f1)
	ls2 := newLineScanner(f2)

	for {
		line1, eof1, err1 := ls1.nextLine()
		line2, eof2, err2 := ls2.nextLine()

		if err1 != nil || err2 != nil {
			return false, fmt.Errorf("error reading files: %v, %v", err1, err2)
		}

		if eof1 && eof2 {
			return true, nil
		}

		if eof1 || eof2 {
			return false, nil
		}

		if line1 != line2 {
			return false, nil
		}
	}
}

// compareFilesRule1 和 extractVisibleChars 保持不变
// compareFilesRule1 比较文件，忽略所有空格、空行、tab，只保留可见字符。
func compareFilesRule1(file1Path, file2Path string) (bool, error) {
	content1, err := os.ReadFile(file1Path)
	if err != nil {
		return false, err
	}
	content2, err := os.ReadFile(file2Path)
	if err != nil {
		return false, err
	}

	visibleChars1 := extractVisibleChars(string(content1))
	visibleChars2 := extractVisibleChars(string(content2))

	return visibleChars1 == visibleChars2, nil
}

// extractVisibleChars 从字符串中提取所有可见字符。
func extractVisibleChars(s string) string {
	var result strings.Builder
	for _, r := range s {
		// unicode.IsSpace 检查是否为空白字符（包括空格、tab、换行等）
		if !unicode.IsSpace(r) {
			result.WriteRune(r)
		}
	}
	return result.String()
}
