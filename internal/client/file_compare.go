package client

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

// --- 规则 0 的新辅助函数实现 ---

// lineScanner 结构体用于管理文件扫描，并支持忽略尾部空行的逻辑
type lineScanner struct {
	scanner *bufio.Scanner
	lines   []string // 存储所有已扫描的行（去除行尾空格/\t）
	cursor  int      // 当前读取到的行索引
	eof     bool
}

func newLineScanner(f *os.File) *lineScanner {
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 128*1024*1024)
	return &lineScanner{
		scanner: scanner,
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
