package client

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
)

// 限制常量
const (
	MaxFileSize   = 128 * 1024 * 1024 // 128MB
	MaxLineLength = 128 * 1024        // 128KB 单行最大长度
)

func compareFiles(file1Path, file2Path string) (int, error) {
	// 先检查文件大小，快速排除明显不同的情况
	info1, err := os.Stat(file1Path)
	if err != nil {
		return 2, fmt.Errorf("failed to stat file1: %w", err)
	}
	info2, err := os.Stat(file2Path)
	if err != nil {
		return 2, fmt.Errorf("failed to stat file2: %w", err)
	}

	// 检查文件大小是否超过限制
	if info1.Size() > MaxFileSize || info2.Size() > MaxFileSize {
		return 2, fmt.Errorf("file size exceeds limit (%dMB)", MaxFileSize/(1024*1024))
	}

	// 尝试以规则 0 比较文件
	sameRule0, err := compareFilesRule0(file1Path, file2Path)
	if err != nil {
		return 2, err
	}
	if sameRule0 {
		return 0, nil
	}

	// 尝试以规则 1 比较文件
	sameRule1, err := compareFilesRule1Stream(file1Path, file2Path)
	if err != nil {
		return 2, fmt.Errorf("rule 1 compare error: %w", err)
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
	buf := make([]byte, 0, 64*1024) // 初始 64KB 缓冲区
	scanner.Buffer(buf, MaxLineLength)
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
	lineNumber := 0
	for ls.scanner.Scan() {
		lineNumber++
		line := ls.scanner.Text()

		// 检查单行长度是否超过限制
		if len(line) > MaxLineLength {
			return "", false, fmt.Errorf("line %d exceeds maximum length (%d bytes)", lineNumber, MaxLineLength)
		}

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

// compareFilesRule1Stream 以流式方式比较文件，忽略所有空格、空行、tab，只保留可见字符。
func compareFilesRule1Stream(file1Path, file2Path string) (bool, error) {
	return compareFilesByStream(file1Path, file2Path, filterVisibleChars)
}

// filterVisibleChars 从字符串中过滤出可见字符（去除所有空白字符）。
func filterVisibleChars(s string) string {
	var result strings.Builder
	for _, r := range s {
		// unicode.IsSpace 检查是否为空白字符（包括空格、tab、换行等）
		if !unicode.IsSpace(r) {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// compareFilesByStream 通过流式读取比较两个文件的内容
func compareFilesByStream(file1Path, file2Path string, filterFunc func(string) string) (bool, error) {
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

	reader1 := bufio.NewReader(f1)
	reader2 := bufio.NewReader(f2)

	var buf1, buf2 []byte
	const chunkSize = 64 * 1024 // 64KB chunks

	for {
		// 读取块
		chunk1 := make([]byte, chunkSize)
		n1, err1 := reader1.Read(chunk1)
		chunk1 = chunk1[:n1]

		chunk2 := make([]byte, chunkSize)
		n2, err2 := reader2.Read(chunk2)
		chunk2 = chunk2[:n2]

		// 处理读取结果
		if err1 != nil && err1 != io.EOF {
			return false, fmt.Errorf("error reading file1: %w", err1)
		}
		if err2 != nil && err2 != io.EOF {
			return false, fmt.Errorf("error reading file2: %w", err2)
		}

		// 应用过滤函数
		filtered1 := filterFunc(string(chunk1))
		filtered2 := filterFunc(string(chunk2))

		// 添加到缓冲区
		buf1 = append(buf1, filtered1...)
		buf2 = append(buf2, filtered2...)

		// 比较缓冲区长度较小的部分（使用 min 函数）
		minLen := min(len(buf1), len(buf2))

		if minLen > 0 {
			s1 := string(buf1[:minLen])
			s2 := string(buf2[:minLen])
			if s1 != s2 {
				return false, nil
			}

			// 移除已比较部分
			buf1 = buf1[minLen:]
			buf2 = buf2[minLen:]
		}

		// 检查是否 EOF
		if err1 == io.EOF && err2 == io.EOF {
			return len(buf1) == len(buf2), nil
		}
		if err1 == io.EOF || err2 == io.EOF {
			return false, nil
		}
	}
}
