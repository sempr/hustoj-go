// Package testdata 提供题目测试数据目录里 input.name / output.name 的
// 校验规则。
//
// 这两个文件由 Web 端上传，内容会被判题器当成 /code 下的文件名使用。
// 上传入口和判题器必须共用同一套语义，否则 Web 放行的内容在判题侧会被
// 当成路径（历史漏洞：内容为 ../../etc/cron.d/pwn 时 root 直接越界写）。
package namefile

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	// InputNameFile / OutputNameFile 是 File-I/O 模式的文件名控制文件。
	InputNameFile  = "input.name"
	OutputNameFile = "output.name"

	// MaxNameContentSize 是控制文件内容允许的最大字节数。
	MaxNameContentSize = 4096
	// MaxNameLength 是单个文件名的最大字节数（Linux NAME_MAX）。
	MaxNameLength = 255
)

// IsNameFile 判断文件名是否是 input.name / output.name。
func IsNameFile(name string) bool {
	return name == InputNameFile || name == OutputNameFile
}

// SanitizeName 校验并返回控制文件里的文件名。
//
// 返回空串表示内容为空或非法：调用方应把空串当作"未指定 File-I/O 文件名"
// 处理（判题器就是这么做），绝不能拿原始内容去拼路径。
func SanitizeName(content string) string {
	name, err := ParseName(content)
	if err != nil {
		return ""
	}
	return name
}

// ParseName 校验控制文件内容，返回去掉首尾空白后的文件名。
// 空内容返回 ("", nil)，表示不启用 File-I/O 模式。
func ParseName(content string) (string, error) {
	if len(content) > MaxNameContentSize {
		return "", fmt.Errorf("内容超过 %d 字节", MaxNameContentSize)
	}
	name := strings.TrimSpace(content)
	if name == "" {
		return "", nil
	}
	if name == "." || name == ".." {
		return "", errors.New("不能是 . 或 ..")
	}
	if strings.ContainsAny(name, "/\\\x00") {
		return "", errors.New("只能是单个文件名，不能包含路径分隔符")
	}
	if strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", errors.New("文件名包含控制字符")
	}
	if len(name) > MaxNameLength {
		return "", fmt.Errorf("文件名超过 %d 字节", MaxNameLength)
	}
	if filepath.Base(name) != name {
		return "", errors.New("只能是单个文件名")
	}
	return name, nil
}

// ValidateFileContent 校验名为 name 的文件内容；只对 input.name /
// output.name 生效，其它文件直接通过。
func ValidateFileContent(name, content string) error {
	if !IsNameFile(name) {
		return nil
	}
	_, err := ParseName(content)
	return err
}
