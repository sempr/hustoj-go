package shared

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
)

// DelegationOptions 描述要委派给判题用户的 cgroup 目录。
type DelegationOptions struct {
	// Root 是父 cgroup（空值表示 <cgroup2 挂载点>/hustoj）。
	Root string
	// User/Group 是接收委派的判题用户/组名；Group 为空时与 User 相同。
	User  string
	Group string
}

// SetupDelegation 创建并委派判题 cgroup（需要 root）。
//
// 它做四件事：
//  1. 从 cgroup2 挂载点逐级启用 cpu/memory/pids 控制器；
//  2. 在 Root 上启用控制器，使后续子目录能拿到 memory.max 等文件；
//  3. 把 Root 从挂载点开始递归 chown 给判题用户；
//  4. 保证 Root 内没有进程（否则 cgroup v2 的 no-internal-process 规则
//     会让子目录拿不到控制器）。
//
// 该命令是幂等的，可以在每次安装/升级时重复执行。
func SetupDelegation(opts DelegationOptions) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("cgroup-setup 需要 root 权限（请用 sudo 运行）")
	}
	mount, err := Cgroup2MountPoint()
	if err != nil {
		return err
	}

	root := strings.TrimSpace(opts.Root)
	if root == "" {
		root = filepath.Join(mount, "hustoj")
	}
	if !filepath.IsAbs(root) {
		return fmt.Errorf("cgroup 根目录必须是绝对路径: %q", root)
	}
	root = filepath.Clean(root)
	if !isWithin(mount, root) {
		return fmt.Errorf("cgroup 根目录 %q 必须位于 cgroup2 挂载点 %q 之下", root, mount)
	}

	u, err := user.Lookup(opts.User)
	if err != nil {
		return fmt.Errorf("找不到用户 %q: %w", opts.User, err)
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("用户 %q 的 uid 非法: %w", opts.User, err)
	}
	groupName := opts.Group
	if groupName == "" {
		groupName = opts.User
	}
	g, err := user.LookupGroup(groupName)
	if err != nil {
		return fmt.Errorf("找不到组 %q: %w", groupName, err)
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil {
		return fmt.Errorf("组 %q 的 gid 非法: %w", groupName, err)
	}

	if err := os.MkdirAll(root, 0o755); err != nil {
		return fmt.Errorf("创建 cgroup 根目录 %s 失败: %w", root, err)
	}

	// 先把已有进程移出 Root：root cgroup 可以容纳进程，子目录需要为空。
	if err := evacuateProcs(root, filepath.Join(mount, "cgroup.procs")); err != nil {
		return err
	}
	// 从挂载点开始逐级启用控制器（root 为空时才允许写 subtree_control）。
	if err := enableControllers(mount, root); err != nil {
		return err
	}
	// 显式再写一次 Root：enableControllers 会忽略 EBUSY，这里要给出明确错误。
	if err := enableControllersAt(root); err != nil {
		return fmt.Errorf("在 %s 启用控制器失败（目录里可能还有进程）: %w", root, err)
	}
	if err := verifyControllers(root); err != nil {
		return err
	}
	if err := evacuateProcs(root, filepath.Join(mount, "cgroup.procs")); err != nil {
		return err
	}

	// 递归 chown：目录与所有控制文件都归判题用户，spawner 才能创建子目录、
	// 写 cgroup.subtree_control 和 cgroup.procs。
	if err := filepath.WalkDir(root, func(path string, d os.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if err := os.Lchown(path, uid, gid); err != nil {
			return fmt.Errorf("chown %s 失败: %w", path, err)
		}
		if d.IsDir() {
			if err := os.Chmod(path, 0o755); err != nil {
				return fmt.Errorf("chmod %s 失败: %w", path, err)
			}
		}
		return nil
	}); err != nil {
		return err
	}

	// 最后确认委派结果可被目标用户写。
	for _, name := range []string{"cgroup.procs", "cgroup.subtree_control", "cgroup.controllers"} {
		p := filepath.Join(root, name)
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("委派后的 %s 不可用: %w", p, err)
		}
	}
	return nil
}

// verifyControllers 确认 dir 的 subtree_control 已包含所有可用控制器。
func verifyControllers(dir string) error {
	avail, err := os.ReadFile(filepath.Join(dir, "cgroup.controllers"))
	if err != nil {
		return fmt.Errorf("读取 %s/cgroup.controllers 失败: %w", dir, err)
	}
	sub, err := os.ReadFile(filepath.Join(dir, "cgroup.subtree_control"))
	if err != nil {
		return fmt.Errorf("读取 %s/cgroup.subtree_control 失败: %w", dir, err)
	}
	enabled := map[string]bool{}
	for _, n := range strings.Fields(string(sub)) {
		enabled[n] = true
	}
	for _, n := range strings.Fields(string(avail)) {
		for _, want := range cgroupControllers {
			if n == want && !enabled[n] {
				return fmt.Errorf("%s 未启用控制器 %s（目录里可能还有进程；请先停止判题服务再执行 cgroup-setup）", dir, n)
			}
		}
	}
	return nil
}

// evacuateProcs 把 dir 里的进程移到 target cgroup.procs。
func evacuateProcs(dir, target string) error {
	data, err := os.ReadFile(filepath.Join(dir, "cgroup.procs"))
	if err != nil {
		return fmt.Errorf("读取 %s/cgroup.procs 失败: %w", dir, err)
	}
	for _, pid := range strings.Fields(string(data)) {
		if err := os.WriteFile(target, []byte(pid), 0o644); err != nil {
			return fmt.Errorf("把进程 %s 移出 %s 失败: %w", pid, dir, err)
		}
	}
	return nil
}
