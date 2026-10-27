package client

import (
	"os"
	"testing"
)

// 测试规则0：忽略尾部空格和换行
func TestCompareFiles_Rule0(t *testing.T) {
	tests := []struct {
		name     string
		file1    string
		file2    string
		expected int
	}{
		{
			name:     "完全相同的文件",
			file1:    "testdata/file1_rule0.txt",
			file2:    "testdata/file1_rule0.txt",
			expected: 0,
		},
		{
			name:     "尾部空格不同（规则0应视为相同）",
			file1:    "testdata/file1_rule0.txt",
			file2:    "testdata/file1_rule0_spaces.txt",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compareFiles(tt.file1, tt.file2)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

// TestCompareFiles_Rule1 测试规则1：忽略所有空白字符
func TestCompareFiles_Rule1(t *testing.T) {
	tests := []struct {
		name     string
		file1    string
		file2    string
		expected int
	}{
		{
			name:     "仅空白字符不同的文件（规则1应视为相同）",
			file1:    "testdata/file3_rule1.txt",
			file2:    "testdata/file4_rule1.txt",
			expected: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := compareFiles(tt.file1, tt.file2)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

// TestCompareFiles_Boundary 测试边界情况
func TestCompareFiles_Boundary(t *testing.T) {
	// 空文件
	t.Run("空文件", func(t *testing.T) {
		result, err := compareFiles("testdata/empty1.txt", "testdata/empty2.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != 0 {
			t.Errorf("expected 0, got %d", result)
		}
	})

	// 仅含空行的文件
	t.Run("仅含空行的文件", func(t *testing.T) {
		result, err := compareFiles("testdata/only_newlines1.txt", "testdata/only_newlines2.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != 0 {
			t.Errorf("expected 0, got %d", result)
		}
	})

	// 文件不存在
	t.Run("文件不存在", func(t *testing.T) {
		_, err := compareFiles("testdata/nonexistent1.txt", "testdata/nonexistent2.txt")
		if err == nil {
			t.Error("expected error for non-existent files, got nil")
		}
	})
}

// TestCompareFiles_ExpectedFailure 测试预期的失败情况
func TestCompareFiles_ExpectedFailure(t *testing.T) {
	// 完全不同的文件
	t.Run("完全不同的文件", func(t *testing.T) {
		result, err := compareFiles("testdata/file1_rule0.txt", "testdata/file5_completely_different.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != 2 {
			t.Errorf("expected 2 (completely different), got %d", result)
		}
	})
}

// TestCompareFilesLargeFile 测试大文件处理（接近限制）
func TestCompareFilesLargeFile(t *testing.T) {
	t.Run("大文件处理", func(t *testing.T) {
		// 创建不超过限制的大文件
		largeFile1 := "testdata/large_file1.txt"
		largeFile2 := "testdata/large_file2.txt"

		// 创建内容相同的两个大文件
		createLargeTestFile(t, largeFile1, 5*1024*1024) // 5MB
		createLargeTestFile(t, largeFile2, 5*1024*1024) // 5MB

		// 确保清理
		defer os.Remove(largeFile1)
		defer os.Remove(largeFile2)

		result, err := compareFiles(largeFile1, largeFile2)
		if err != nil {
			t.Fatalf("unexpected error with large files: %v", err)
		}
		if result != 0 {
			t.Errorf("expected 0, got %d", result)
		}
	})
}

// createLargeTestFile 创建用于测试的大文件
func createLargeTestFile(t *testing.T, filePath string, size int) {
	t.Helper()

	f, err := os.Create(filePath)
	if err != nil {
		t.Fatalf("failed to create large test file: %v", err)
	}
	defer f.Close()

	line := "This is a test line with some content.\n"
	remaining := size
	for remaining > len(line) {
		_, err := f.WriteString(line)
		if err != nil {
			t.Fatalf("failed to write to file: %v", err)
		}
		remaining -= len(line)
	}
}
