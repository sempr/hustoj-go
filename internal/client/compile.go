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
	os.Chmod(filepath.Join(rootfs, "code"), 0777)
	defer os.Chmod(filepath.Join(rootfs, "code"), 0755)
	selfName, _ := os.Executable()
	cmd := exec.Command(selfName,
		"sandbox",
		fmt.Sprintf("--rootfs=%s", rootfs),
		fmt.Sprintf("--cmd=%s", langConfig.Cmd.Compile),
		fmt.Sprintf("--time=%d", 3000),
		fmt.Sprintf("--memory=%d", 256<<10),
		fmt.Sprintf("--sid=%d", jc.solutionID),
		"--cwd=/code",
	)

	if len(langConfig.Cmd.Env) > 0 {
		cmd.Env = append(cmd.Env, langConfig.Cmd.Env...)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout

	r, w, err := os.Pipe()
	if err != nil {
		return &models.SandboxOutput{
			UserStatus:     constants.OJ_SE,
			CombinedOutput: "failed to create pipe for compile",
		}
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, w)
	slog.Info("Starting compilation", "language", langID, "work_dir", rootfs)

	if err := cmd.Start(); err != nil {
		return &models.SandboxOutput{
			UserStatus:     constants.OJ_SE,
			CombinedOutput: "failed to start compile command",
		}
	}

	w.Close()
	defer cmd.Wait()

	var output models.SandboxOutput
	if err := json.NewDecoder(r).Decode(&output); err != nil {
		return &models.SandboxOutput{
			UserStatus:     constants.OJ_SE,
			CombinedOutput: fmt.Sprintf("failed to decode compile output: %v", err),
		}
	}

	slog.Debug("Compilation output", "output", output)
	return &output
}
