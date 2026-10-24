package tini

import (
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ErrNotEmbedded 表示当前二进制没有内置 tini（构建时缺少 embed_tini 标签）。
var ErrNotEmbedded = errors.New("当前二进制没有内置 tini：构建时需加 -tags embed_tini")

// fileMD5 返回文件的 md5（十六进制）。
func fileMD5(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:]), nil
}

// InstallTo 把内置 tini 原子地写到 path，返回写入后的 md5 与是否真的写了。
//
// 目标文件已存在且 md5 一致时直接跳过（force=true 强制重写）。
// 先写同目录临时文件再 rename：避免中途失败留下半截的 tini，
// 也避免并发判题时读到写了一半的可执行文件。
func InstallTo(path string, force bool) (string, bool, error) {
	data := Binary()
	if len(data) == 0 {
		return "", false, ErrNotEmbedded
	}
	want := MD5()

	if !force {
		if cur, err := fileMD5(path); err == nil && cur == want {
			return want, false, nil
		}
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tini-*")
	if err != nil {
		return "", false, fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", false, fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", false, fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return "", false, fmt.Errorf("设置权限失败: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", false, fmt.Errorf("替换 %s 失败: %w", path, err)
	}

	// 回读校验：磁盘满、部分写这类问题只有落盘后才看得见。
	got, err := fileMD5(path)
	if err != nil {
		return "", false, fmt.Errorf("回读 %s 失败: %w", path, err)
	}
	if got != want {
		return "", false, fmt.Errorf("md5 不一致：写入 %s，期望 %s", got, want)
	}
	return got, true, nil
}
