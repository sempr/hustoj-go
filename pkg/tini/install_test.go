package tini

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// 未用 embed_tini 构建时，install-tini 必须明确报错，而不是静默写个空文件。
func TestInstallToWithoutEmbedded(t *testing.T) {
	if len(Binary()) != 0 {
		t.Skip("当前是 embed_tini 构建")
	}
	path := filepath.Join(t.TempDir(), "tini")
	if _, _, err := InstallTo(path, false); !errors.Is(err, ErrNotEmbedded) {
		t.Fatalf("err = %v，期望 ErrNotEmbedded", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("报错时不应留下文件")
	}
}

// embed_tini 构建下：落盘内容、权限、幂等与 force 行为。
func TestInstallToEmbedded(t *testing.T) {
	if len(Binary()) == 0 {
		t.Skip("未用 embed_tini 构建")
	}
	path := filepath.Join(t.TempDir(), "tini")

	sum, wrote, err := InstallTo(path, false)
	if err != nil {
		t.Fatalf("首次安装失败: %v", err)
	}
	if !wrote {
		t.Fatal("首次安装应当写入")
	}
	if sum != MD5() {
		t.Errorf("返回 md5 = %s，期望 %s", sum, MD5())
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o755 {
		t.Errorf("权限 = %o，期望 755", st.Mode().Perm())
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != len(Binary()) {
		t.Errorf("写入长度 = %d，期望 %d", len(data), len(Binary()))
	}

	// md5 一致时不重写
	if _, wrote, err = InstallTo(path, false); err != nil || wrote {
		t.Errorf("重复安装: wrote=%v err=%v，期望跳过", wrote, err)
	}

	// force 强制重写
	if _, wrote, err = InstallTo(path, true); err != nil || !wrote {
		t.Errorf("force 安装: wrote=%v err=%v，期望重写", wrote, err)
	}
}
