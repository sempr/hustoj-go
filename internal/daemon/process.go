package daemon

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
)

const STD_MB = 1048576

// RunClient executes the judge_client or a Docker container.
// It calls platform-specific helpers for child setup and resource limits.
func RunClient(cfg *DaemonConfig, solutionID, clientID int, done chan<- int) {
	defer func() {
		done <- clientID // Notify that the job has finished
	}()

	solutionIDStr := strconv.Itoa(solutionID)
	clientIDStr := strconv.Itoa(clientID)

	var cmd *exec.Cmd
	selfexe, _ := os.Executable()
	fmt.Printf("%s client %s %s %s\n", selfexe, solutionIDStr, clientIDStr, cfg.OJHome)
	cmd = exec.Command(selfexe, "client", solutionIDStr, clientIDStr, cfg.OJHome)

	// Pdeathsig 等只能在 Start 前设置；rlimit 必须在子进程启动后用
	// prlimit 设置，否则会改到 daemon 自己身上（历史上就是这么错的）。
	configureChildProcess(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		slog.Error("Failed to start client", "solution_id", solutionID, "err", err)
		return
	}
	if err := applyResourceLimits(cmd.Process.Pid, cfg); err != nil {
		slog.Warn("Failed to set resource limits", "solution_id", solutionID, "err", err)
	}
	err := cmd.Wait()
	output := stdout.String() + stderr.String()
	if err != nil {
		slog.Error("Failed to run client", "solution_id", solutionID, "err", err, "output", output)
	}
}
