package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"text/template"

	"github.com/sempr/hustoj-go/pkg/constants"
	"github.com/sempr/hustoj-go/pkg/models"
)

func (jc *JudgeClient) runAndCompare(config RunConfig) (int, int, int) {
	stdinName := "/code/data.in"
	stdoutName := "/code/data.usr"

	if config.InName != "" {
		jc.copyFile(config.InFile, filepath.Join(config.Workdir, config.InName))
		stdinName = ""
	} else {
		jc.copyFile(config.InFile, filepath.Join(config.Workdir, "data.in"))
	}

	if config.OutName != "" {
		stdoutName = ""
	}

	langConfig, err := jc.langManager.GetLanguageConfig(config.Lang)
	if err != nil {
		slog.Error("Failed to get language config", "error", err)
		return constants.OJ_SE, 0, 0
	}

	runArgs := []string{
		"sandbox",
		fmt.Sprintf("--rootfs=%s", config.Rootdir),
		fmt.Sprintf("--cmd=%s", langConfig.Cmd.Run),
		fmt.Sprintf("--time=%d", config.Timelimit),
		fmt.Sprintf("--memory=%d", config.MemoryLimit<<10),
		fmt.Sprintf("--sid=%d", jc.solutionID),
		"--cwd=/code",
	}

	if stdinName != "" {
		runArgs = append(runArgs, fmt.Sprintf("--stdin=%s", stdinName))
	}
	if stdoutName != "" {
		runArgs = append(runArgs, fmt.Sprintf("--stdout=%s", stdoutName))
	}

	selfName, _ := os.Executable()
	cmd := exec.Command(selfName, runArgs...)
	if len(langConfig.Cmd.Env) > 0 {
		cmd.Env = append(cmd.Env, langConfig.Cmd.Env...)
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout

	r, w, err := os.Pipe()
	if err != nil {
		return constants.OJ_SE, 0, 0
	}

	cmd.ExtraFiles = append(cmd.ExtraFiles, w)
	slog.Info("Starting execution", "language", config.Lang, "work_dir", config.Rootdir)

	if err := cmd.Start(); err != nil {
		return constants.OJ_SE, 0, 0
	}

	w.Close()
	cmd.Wait()

	var output models.SandboxOutput
	if err := json.NewDecoder(r).Decode(&output); err != nil {
		slog.Error("Failed to decode run output", "error", err)
		return constants.OJ_SE, 0, 0
	}

	result := output.UserStatus
	timeUsed := output.Time
	memUsed := output.Memory

	if result != constants.OJ_AC {
		return result, timeUsed, memUsed
	}

	if config.Spj == 1 {
		return jc.handleSpecialJudge(config)
	}

	targetOutputName := "data.usr"
	if config.OutName != "" {
		targetOutputName = config.OutName
	}

	targetInputName := "data.in"
	if config.InName != "" {
		targetInputName = config.InName
	}
	_ = targetInputName

	res, err := jc.compareFiles(config.OutFile, filepath.Join(config.Rootdir, "code", targetOutputName))
	switch res {
	case 1:
		result = constants.OJ_PE
	case 2:
		result = constants.OJ_WA
	case 0:
		result = constants.OJ_AC
	}

	if err != nil {
		result = constants.OJ_RE
	}

	return result, timeUsed, memUsed
}

func (jc *JudgeClient) handleSpecialJudge(config RunConfig) (int, int, int) {
	sysDataFile := filepath.Join(config.Rootdir, "code/sysdata.out")
	jc.copyFile(config.OutFile, sysDataFile)
	defer os.Remove(sysDataFile)

	spjFile := filepath.Join(config.Rootdir, "code/spj")
	jc.copyFile(filepath.Join(filepath.Dir(config.OutFile), "spj"), spjFile)
	defer os.Remove(spjFile)

	runArgs := []string{
		"sandbox",
		fmt.Sprintf("--rootfs=%s", filepath.Join(config.Rootdir, "code")),
		fmt.Sprintf("--cmd=/spj %s %s %s", "data.in", "data.usr", "sysdata.out"),
		fmt.Sprintf("--time=%d", config.Timelimit),
		fmt.Sprintf("--memory=%d", config.MemoryLimit<<10),
		fmt.Sprintf("--sid=%d", jc.solutionID),
		"--cwd=/",
	}

	r, w, err := os.Pipe()
	if err != nil {
		slog.Error("Failed to create pipe for special judge", "error", err)
		return constants.OJ_SE, 0, 0
	}
	defer r.Close()

	selfName, _ := os.Executable()
	cmd := exec.Command(selfName, runArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.ExtraFiles = []*os.File{w}

	if err := cmd.Start(); err != nil {
		slog.Error("Failed to start special judge", "error", err)
		return constants.OJ_SE, 0, 0
	}

	w.Close()
	cmd.Wait()

	var output models.SandboxOutput
	if err := json.NewDecoder(r).Decode(&output); err != nil {
		slog.Error("Failed to decode special judge output", "error", err)
		return constants.OJ_SE, 0, 0
	}

	if output.ExitStatus == 0 {
		return constants.OJ_AC, 0, 0
	}
	return constants.OJ_WA, 0, 0
}

func (jc *JudgeClient) compareFiles(file1, file2 string) (int, error) {
	return compareFiles(file1, file2)
}

func (jc *JudgeClient) addRuntimeInfo(solutionID int, results models.TotalResults) error {
	details, err := jc.renderResults(results)
	if err != nil {
		return fmt.Errorf("failed to render results: %w", err)
	}

	return jc.db.AddRuntimeInfo(solutionID, details)
}

func (jc *JudgeClient) renderResults(results models.TotalResults) (string, error) {
	const tpl = `
filename|size|result|memory|time
 --|--|--|--|--
 {{- range .Results }}
 | {{ .Datafile }}|0|{{ getResult .Result }}/1.00|{{ .Mem }}KB|{{ .Time }}ms
 {{- end }}
`

	funcMap := template.FuncMap{
		"getResult": constants.GetOJResultName,
	}

	t, err := template.New("result").Funcs(funcMap).Parse(tpl)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, results); err != nil {
		return "", err
	}

	return buf.String(), nil
}
