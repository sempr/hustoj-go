// Package rootless 负责"非 root 判题"的入口切换。
//
// 判题器（hustoj judge / hustoj client）在普通用户下运行时，不能直接
// 挂载 tmpfs/overlay、创建 cgroup，也没有权限把沙箱切到 nobody。这里的
// 做法是把自己重新 exec 成 hustoj-spawn：它带一组很小的 file caps，
// 在初始 user namespace 里把进程放进委派好的 cgroup，然后创建新的
// user/mount namespace，映射 0→当前用户、65534→nobody，再 exec 真正的
// hustoj。此后所有判题动作都只拥有该 user namespace 内的权限。
package rootless

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/sempr/hustoj-go/internal/shared"
	"github.com/sempr/hustoj-go/pkg/config"
)

// Mode 是 --rootless 的取值。
type Mode string

const (
	// ModeAuto：root 直接运行；普通用户自动经 hustoj-spawn 进入 userns。
	ModeAuto Mode = "auto"
	// ModeOn：即使当前是 root 也走 hustoj-spawn（用于验证 rootless 路径）。
	ModeOn Mode = "on"
	// ModeOff：直接运行，仅允许 root。
	ModeOff Mode = "off"
)

// RootProcHelperPath is where the setuid helper that can mount host /proc
// into a sandbox root lives. It mirrors procMountHelperPath.
const DefaultSpawnerPath = "/usr/local/libexec/hustoj-spawn"

// ParseMode 解析 auto/on/off，并拒绝其他值。
func ParseMode(s string) (Mode, error) {
	switch Mode(strings.TrimSpace(strings.ToLower(s))) {
	case ModeAuto:
		return ModeAuto, nil
	case ModeOn:
		return ModeOn, nil
	case ModeOff:
		return ModeOff, nil
	default:
		return "", fmt.Errorf("非法的 rootless 模式 %q（可选 auto/on/off）", s)
	}
}

func (m Mode) String() string { return string(m) }

var (
	modeMu       sync.RWMutex
	modeOverride *Mode
)

// SetModeOverride 记录命令行显式给出的模式（优先级最高）。
func SetModeOverride(m Mode) {
	modeMu.Lock()
	defer modeMu.Unlock()
	modeOverride = &m
}

// ResolveMode 按"命令行 > 环境变量 > judge.conf > auto"确定运行模式。
func ResolveMode(homePath string) (Mode, error) {
	modeMu.RLock()
	override := modeOverride
	modeMu.RUnlock()
	if override != nil {
		return *override, nil
	}
	if v := strings.TrimSpace(os.Getenv("OJ_ROOTLESS")); v != "" {
		return ParseMode(v)
	}
	if v := strings.TrimSpace(config.LookupOption(homePath, "OJ_ROOTLESS")); v != "" {
		return ParseMode(v)
	}
	return ModeAuto, nil
}

// InUserNamespace 判断当前进程是否已经处在非初始 user namespace 中。
//
// 初始 user namespace 的 uid_map 固定是一行 "0 0 4294967295"；只要内容
// 不是这个（或环境变量已标记），就说明 userns 已经建好，不能再套一层。
func InUserNamespace() bool {
	if os.Getenv("HUSTOJ_IN_USERNS") == "1" {
		return true
	}
	data, err := os.ReadFile("/proc/self/uid_map")
	if err != nil {
		return false
	}
	// 文件里是右对齐的多空格格式，不能直接和单空格字符串比较。
	f := strings.Fields(string(data))
	if len(f) == 3 && f[0] == "0" && f[1] == "0" && f[2] == "4294967295" {
		return false
	}
	return true
}

func MkNull(path string) bool {
	sock := "/run/hustoj-null.sock"
	if v := strings.TrimSpace(os.Getenv("OJ_NULL_SOCK")); v != "" {
		sock = v
	}
	c, err := net.Dial("unix", sock)
	if err != nil {
		return false
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintf(c, "bindproc %s\n", path)
	r := make([]byte, 64)
	n, _ := c.Read(r)
	return strings.TrimSpace(string(r[:n])) == "OK"
}

func MountProc(path string) error {
	sock := "/run/hustoj-null.sock"
	if v := strings.TrimSpace(os.Getenv("OJ_NULL_SOCK")); v != "" {
		sock = v
	}
	c, err := net.Dial("unix", sock)
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintf(c, "bindproc %d %s\n", os.Getpid(), path)
	r := make([]byte, 64)
	n, _ := c.Read(r)
	res := strings.TrimSpace(string(r[:n]))
	if res != "OK" {
		return fmt.Errorf("helper returned: %s", res)
	}
	return nil
}

func UnmountNull(path string) {
	sock := "/run/hustoj-null.sock"
	if v := strings.TrimSpace(os.Getenv("OJ_NULL_SOCK")); v != "" {
		sock = v
	}
	c, err := net.Dial("unix", sock)
	if err != nil {
		return
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintf(c, "unbind %s\n", path)
	_ = c.Close()
}

func UnmountProc(path string) {
	sock := "/run/hustoj-null.sock"
	if v := strings.TrimSpace(os.Getenv("OJ_NULL_SOCK")); v != "" {
		sock = v
	}
	c, err := net.Dial("unix", sock)
	if err != nil {
		return
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(2 * time.Second))
	fmt.Fprintf(c, "unbindproc %d %s\n", os.Getpid(), path)
}
func SpawnerPath(homePath string) (string, error) {
	explicit := strings.TrimSpace(os.Getenv("OJ_SPAWN_PATH"))
	if explicit == "" {
		explicit = strings.TrimSpace(config.LookupOption(homePath, "OJ_SPAWN_PATH"))
	}
	if explicit != "" {
		if !filepath.IsAbs(explicit) {
			return "", fmt.Errorf("OJ_SPAWN_PATH 必须是绝对路径: %q", explicit)
		}
		if err := checkExecutable(explicit); err != nil {
			return "", err
		}
		return explicit, nil
	}

	candidates := []string{DefaultSpawnerPath}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "hustoj-spawn"))
	}
	for _, p := range candidates {
		if err := checkExecutable(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("找不到 hustoj-spawn（尝试过 %s）；请安装 rootless 组件，或用 --rootless=off 以 root 运行",
		strings.Join(candidates, ", "))
}

func checkExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s 不可用: %w", path, err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%s 不是可执行文件", path)
	}
	return nil
}

// CgroupRoot 返回判题 cgroup 的父目录（委派给当前用户的那个目录）。
//
// 优先级：OJ_CGROUP_ROOT 环境变量 > judge.conf > <cgroup2 挂载点>/hustoj。
// 该目录应由 root 执行 `hustoj cgroup-setup` 创建并 chown 给判题用户。
func CgroupRoot(homePath string) (string, error) {
	root := strings.TrimSpace(os.Getenv("OJ_CGROUP_ROOT"))
	if root == "" {
		root = strings.TrimSpace(config.LookupOption(homePath, "OJ_CGROUP_ROOT"))
	}
	if root == "" {
		mount, err := shared.Cgroup2MountPoint()
		if err != nil {
			return "", err
		}
		root = filepath.Join(mount, "hustoj")
	}
	if !filepath.IsAbs(root) {
		return "", fmt.Errorf("cgroup 根目录必须是绝对路径: %q", root)
	}
	return filepath.Clean(root), nil
}

// maxRunningFromEnv 读取 daemon 传给 client 的并发数，用于设置 RLIMIT_NPROC。
func maxRunningFromEnv() int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv("OJ_MAX_RUNNING"))); err == nil && n > 0 {
		return n
	}
	return 3
}

// MaybeReexec 在需要时把当前进程替换成 hustoj-spawn。
//
// 返回 nil 表示应当继续按当前身份运行；返回错误表示无法进入安全的
// rootless 路径（调用方应退出，而不是悄悄以错误权限继续）。
func MaybeReexec(kind, homePath string) error {
	if InUserNamespace() {
		// 已经在 spawner 建好的 userns 里，直接运行。
		return nil
	}
	if homePath == "" {
		homePath = "/home/judge"
	}

	mode, err := ResolveMode(homePath)
	if err != nil {
		return err
	}
	switch {
	case mode == ModeOff:
		if os.Geteuid() != 0 {
			return fmt.Errorf("--rootless=off 需要 root 权限；普通用户请使用 auto/on")
		}
		return nil
	case mode == ModeAuto && os.Geteuid() == 0:
		// root 手工运行时保持旧行为；systemd 单元本身已改为 judge 用户。
		return nil
	}

	spawner, err := SpawnerPath(homePath)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("无法定位当前可执行文件: %w", err)
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return fmt.Errorf("无法解析当前可执行文件路径: %w", err)
	}
	cgroupRoot, err := CgroupRoot(homePath)
	if err != nil {
		return err
	}

	args := []string{
		spawner,
		"--home", homePath,
		"--cgroup-root", cgroupRoot,
		"--max-running", strconv.Itoa(maxRunningFromEnv()),
		"--label", kind,
		"--exe", exe,
		"--",
	}
	args = append(args, os.Args[1:]...)

	fmt.Fprintf(os.Stderr, "hustoj: 以非 root 身份运行，切换到 %s（%s 模式）\n", spawner, mode)
	return syscall.Exec(spawner, args, os.Environ())
}
