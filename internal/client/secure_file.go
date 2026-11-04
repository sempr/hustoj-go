package client

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/sempr/hustoj-go/pkg/namefile"
	"golang.org/x/sys/unix"
)

// createNoFollow 打开（并可选创建/截断）dst，但不跟随最后一级符号链接。
//
// 判题器以 root 身份操作 /code 下的文件，而 /code 在编译阶段对选手可写。
// 选手可以留下 data.usr -> /etc/cron.d/xxx 这样的链接；普通 os.Create 会
// 跟随链接，把沙箱输出写到宿主机上。O_NOFOLLOW 让这类操作直接返回 ELOOP。
//
// 打开后还会校验目标确实是普通文件，避免 FIFO/设备节点把后续读写挂住。
func createNoFollow(dst string, flag int, perm os.FileMode) (*os.File, error) {
	f, err := os.OpenFile(dst, flag|unix.O_NOFOLLOW, perm)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%s 不是普通文件（mode=%s），拒绝操作", dst, info.Mode())
	}
	return f, nil
}

// openRegularNoFollow 以只读方式打开普通文件，拒绝符号链接和特殊文件。
//
// 带 O_NONBLOCK：万一目标被换成 FIFO，open 不会永久阻塞；随后 fstat 发现
// 不是普通文件就关闭并报错。对普通文件 O_NONBLOCK 没有影响。
func openRegularNoFollow(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("%s 不是普通文件（mode=%s），拒绝读取", path, info.Mode())
	}
	return f, nil
}

// copyFileWithMode 把 src 复制到 dst，并在需要时设置 dst 的权限。
//
// 目标用 createNoFollow 创建：dst 若是符号链接则失败，绝不会写到链接指向的
// 宿主机文件。源文件同样用 O_NOFOLLOW 打开：题目数据目录对 Web 用户可写，
// 不能让 root 顺着数据文件里的符号链接读取宿主机文件。
// mode 为 0 表示沿用 open 时的 0644（受 umask 影响）。
func copyFileWithMode(src, dst string, mode os.FileMode) error {
	sourceFile, err := openRegularNoFollow(src)
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer sourceFile.Close()

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	destinationFile, err := createNoFollow(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer destinationFile.Close()

	if _, err := destinationFile.ReadFrom(sourceFile); err != nil {
		return fmt.Errorf("failed to copy file contents: %w", err)
	}
	if mode != 0 {
		if err := destinationFile.Chmod(mode); err != nil {
			return fmt.Errorf("failed to chmod destination file: %w", err)
		}
	}
	return nil
}

// createEmptyWithMode 以 O_NOFOLLOW 创建/截断一个普通文件并设置权限。
func createEmptyWithMode(path string, mode os.FileMode) error {
	f, err := createNoFollow(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o666)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Chmod(mode)
}

// regularFileSize 返回普通文件大小；符号链接/设备/FIFO 一律拒绝。
func regularFileSize(path string) (int64, error) {
	f, err := openRegularNoFollow(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// sanitizeTestDataName 校验 input.name / output.name 的内容。
//
// 这两个文件由 Web 端上传、位于 www-data 可写的数据目录里，内容不能当成
// 可信路径。校验规则与 Web 上传入口共用 pkg/namefile，避免两边语义漂移：
// 只接受单个文件名，含目录分隔符、绝对路径、"."/".." 一律拒绝。
func sanitizeTestDataName(name string) string {
	return namefile.SanitizeName(name)
}

// sanitizeCompileTree 清理编译产物顶层的非普通文件。
//
// 合法编译产物只会在 /code 顶层留下普通文件/目录。编译阶段 /code 是 0777，
// 选手代码可以创建符号链接、FIFO、设备节点；这些文件随后会被 root 按路径
// 打开。这里在编译结束时统一删除，作为 O_NOFOLLOW 之外的第二道防线。
func sanitizeCompileTree(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		slog.Warn("读取编制产物目录失败，跳过清理", "dir", dir, "err", err)
		return
	}
	for _, entry := range entries {
		mode := entry.Type()
		if mode&os.ModeSymlink == 0 && (entry.IsDir() || mode.IsRegular()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.Remove(path); err != nil {
			slog.Warn("清理不安全的编译产物失败", "path", path, "err", err)
			continue
		}
		slog.Warn("已清理不安全的编译产物", "path", path, "mode", mode.String())
	}
}
