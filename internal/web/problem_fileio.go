package web

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// problemFileIONames 读取题目数据目录里的 input.name / output.name，
// 返回程序 freopen 应使用的输入/输出文件名；文件不存在则返回空串。
//
// 约定与判题侧一致（见 internal/client 的 findInputName/findOutputName）：
// 数据目录位于 <OJHome>/data/<problem_id>/，只要存在 input.name 或
// output.name，该题就强制文件读写 —— 判题时程序的标准输入/输出会被
// 重定向到这两个文件，而不是用 stdin/stdout。
func problemFileIONames(ojHome string, problemID int32) (in, out string) {
	dir := filepath.Join(ojHome, "data", strconv.Itoa(int(problemID)))
	return readTrimmedFile(filepath.Join(dir, "input.name")),
		readTrimmedFile(filepath.Join(dir, "output.name"))
}

func readTrimmedFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
