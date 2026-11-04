package shared

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCgroup2Mount(t *testing.T) {
	mountinfo := strings.Join([]string{
		"21 1 8:1 / / rw,relatime - ext4 /dev/sda1 rw",
		"36 21 0:32 / /sys/fs/cgroup rw,nosuid,nodev,noexec,relatime - cgroup2 cgroup2 rw,nsdelegate",
	}, "\n")
	got, err := parseCgroup2Mount(mountinfo)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/sys/fs/cgroup" {
		t.Fatalf("解析出的挂载点 = %q", got)
	}
}

func TestParseCgroup2MountContainer(t *testing.T) {
	// cgroup namespace 里挂载点常常不是 /sys/fs/cgroup；空格会被转义成 \040。
	mountinfo := `36 21 0:32 /docker/abc /run/cgroup\040v2 rw,nosuid - cgroup2 cgroup2 rw`
	got, err := parseCgroup2Mount(mountinfo)
	if err != nil {
		t.Fatal(err)
	}
	if got != "/run/cgroup v2" {
		t.Fatalf("解析出的挂载点 = %q", got)
	}
}

func TestParseCgroup2MountMissing(t *testing.T) {
	if _, err := parseCgroup2Mount("21 1 8:1 / / rw - ext4 /dev/sda1 rw"); err == nil {
		t.Fatal("没有 cgroup2 时应报错")
	}
}

func TestCgroupParentRejectsRelativeOverride(t *testing.T) {
	t.Setenv("OJ_CGROUP_ROOT", "relative/path")
	if _, err := cgroupParent("/sys/fs/cgroup"); err == nil {
		t.Fatal("相对路径的 OJ_CGROUP_ROOT 应被拒绝")
	}

	abs := filepath.Join(t.TempDir(), "delegated")
	t.Setenv("OJ_CGROUP_ROOT", abs)
	got, err := cgroupParent("/sys/fs/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Clean(abs) {
		t.Fatalf("OJ_CGROUP_ROOT 覆盖失败: %q", got)
	}
}

func TestCgroupParentDefaultIsAbsolute(t *testing.T) {
	t.Setenv("OJ_CGROUP_ROOT", "")
	got, err := cgroupParent("/sys/fs/cgroup")
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(got) || got != "/sys/fs/cgroup/hustoj" {
		t.Fatalf("默认父目录 = %q", got)
	}
}

func TestCgroupDirNameUniqueAndSafe(t *testing.T) {
	a := cgroupDirName(123, 456)
	b := cgroupDirName(123, 456)
	if a == b {
		t.Fatalf("两次生成的目录名相同: %q", a)
	}
	for _, name := range []string{a, b} {
		if strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
			t.Fatalf("目录名不安全: %q", name)
		}
		if !strings.HasPrefix(name, "run-123-456-") {
			t.Fatalf("目录名格式不对: %q", name)
		}
	}
}

func TestIsWithin(t *testing.T) {
	tests := []struct {
		parent, child string
		want          bool
	}{
		{"/sys/fs/cgroup/hustoj", "/sys/fs/cgroup/hustoj/run-1-2-abc", true},
		{"/sys/fs/cgroup/hustoj", "/sys/fs/cgroup/hustoj", false},
		{"/sys/fs/cgroup/hustoj", "/sys/fs/cgroup/hustoj-evil/x", false},
		{"/sys/fs/cgroup/hustoj", "/sys/fs/cgroup/hustoj/../evil", false},
		{"/abs", "relative", false},
	}
	for _, tc := range tests {
		if got := isWithin(tc.parent, tc.child); got != tc.want {
			t.Errorf("isWithin(%q, %q) = %v，期望 %v", tc.parent, tc.child, got, tc.want)
		}
	}
}

func TestEnableControllersAt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "cgroup.controllers"), []byte("cpu memory io\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	subtree := filepath.Join(dir, "cgroup.subtree_control")
	if err := os.WriteFile(subtree, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := enableControllersAt(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(subtree)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	// pids 不在可用列表里，不应该被写进去。
	if !strings.Contains(got, "+cpu") || !strings.Contains(got, "+memory") {
		t.Fatalf("subtree_control 未启用 cpu/memory: %q", got)
	}
	if strings.Contains(got, "+pids") {
		t.Fatalf("subtree_control 写入了不可用的 pids: %q", got)
	}
}

func TestEnableControllersRejectsRelative(t *testing.T) {
	if err := enableControllers("relative", "/abs"); err == nil {
		t.Fatal("相对 root 应被拒绝")
	}
	if err := enableControllers("/abs", "relative"); err == nil {
		t.Fatal("相对 dir 应被拒绝")
	}
}
