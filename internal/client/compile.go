package client

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sempr/hustoj-go/pkg/constants"
	"github.com/sempr/hustoj-go/pkg/language"
	"github.com/sempr/hustoj-go/pkg/models"
)

func (jc *JudgeClient) compile(langID int, rootfs string, langConfig *language.LangConfig) *models.SandboxOutput {
	codeDir := filepath.Join(rootfs, "code")
	os.Chmod(codeDir, 0o777)
	defer func() {
		// 编译阶段 /code 对选手可写，结束先恢复权限，再清掉选手可能留下的
		// 符号链接/FIFO/设备节点，避免后续 root 按路径打开时被引到宿主机。
		os.Chmod(codeDir, 0o755)
		sanitizeCompileTree(codeDir)
	}()
	selfName, _ := os.Executable()
	cmd := exec.Command(
		selfName,
		"run",
		fmt.Sprintf("--rootfs=%s", rootfs),
		fmt.Sprintf("--cmd=%s", langConfig.Cmd.Compile),
		fmt.Sprintf("--time=%d", 10000),
		fmt.Sprintf("--memory=%d", 256<<10),
		fmt.Sprintf("--sid=%d", jc.solutionID),
		"--cwd=/code",
	)

	// 必须带上父进程环境：OJ_CGROUP_ROOT 由 spawner 指向本次运行所属的
	// 委派 cgroup，run 子进程要靠它创建 per-run cgroup。
	cmd.Env = append(os.Environ(), langConfig.Cmd.Env...)

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	r, w, err := os.Pipe()
	if err != nil {
		return &models.SandboxOutput{
			UserStatus:     constants.OJ_SE,
			SystemError:    true,
			CombinedOutput: "failed to create pipe for compile",
		}
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, w)
	slog.Info("Starting compilation", "language", langID, "work_dir", rootfs)

	if err := cmd.Start(); err != nil {
		return &models.SandboxOutput{
			UserStatus:     constants.OJ_SE,
			SystemError:    true,
			CombinedOutput: "failed to start compile command",
		}
	}

	w.Close()
	defer cmd.Wait()

	var output models.SandboxOutput
	if err := json.NewDecoder(r).Decode(&output); err != nil {
		// 沙箱没有回传 JSON（如 run 进程 panic）：这是系统错误，绝不能被
		// 当成"编译成功"（否则产物缺失，后续所有用例都判 WA）。
		return &models.SandboxOutput{
			UserStatus:     constants.OJ_SE,
			SystemError:    true,
			CombinedOutput: fmt.Sprintf("failed to decode compile output: %v", err),
		}
	}

	slog.Debug("Compilation output", "output", output)
	return &output
}
