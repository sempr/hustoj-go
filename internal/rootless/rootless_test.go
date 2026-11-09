package rootless

import (
	"github.com/sempr/hustoj-go/pkg/config"

	"os"
	"path/filepath"
	"testing"
)

func TestParseMode(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    Mode
		wantErr bool
	}{
		{"auto", ModeAuto, false},
		{"  On ", ModeOn, false},
		{"OFF", ModeOff, false},
		{"x", "", true},
	} {
		got, err := ParseMode(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("ParseMode(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("ParseMode(%q) = (%q, %v), want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestLookupOption(t *testing.T) {
	tmp := t.TempDir()
	conf := filepath.Join(tmp, "etc")
	if err := os.MkdirAll(conf, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(conf, "judge.conf"), []byte("OJ_ROOTLESS=on\nOJ_CGROUP_ROOT=/tmp/cg\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := config.LookupOption(tmp, "OJ_ROOTLESS"); got != "on" {
		t.Fatalf("got %q want on", got)
	}
	if got := config.LookupOption(tmp, "MISSING"); got != "" {
		t.Fatalf("got %q want empty", got)
	}
	if got := config.LookupOption(tmp, "OJ_CGROUP_ROOT"); got != "/tmp/cg" {
		t.Fatalf("got %q", got)
	}
}

func TestCgroupRootAuto(t *testing.T) {
	// 非存在的 home 走默认探测路径，不 panic。
	// 测试只确认函数可被调用（真正的挂载点在测试机上存在）。
	if _, err := CgroupRoot("/no/such/home"); err != nil {
		t.Fatalf("CgroupRoot: %v", err)
	}
}