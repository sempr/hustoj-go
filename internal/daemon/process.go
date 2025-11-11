package daemon

import (
	"fmt"
	"os/exec"
	"strconv"
)

const STD_MB = 1048576

// RunClient executes the judge_client or a Docker container.
// It calls a platform-specific setResourceLimits function.
func RunClient(cfg *Config, solutionID, clientID int, done chan<- int) {
	defer func() {
		done <- clientID // Notify that the job has finished
	}()

	solutionIDStr := strconv.Itoa(solutionID)
	clientIDStr := strconv.Itoa(clientID)

	var cmd *exec.Cmd

	if cfg.UseDocker {
		clientPath := "/usr/bin/judge_client"
		if !cfg.InternalClient {
			clientPath = "/home/judge/src/core/judge_client/judge_client"
		}

		dockerVolume := fmt.Sprintf("%s:/home/judge", cfg.OJHome)
		dataVolume := fmt.Sprintf("%s/data:/home/judge/data", cfg.OJHome)

		args := []string{
			"container", "run", "--pids-limit", "100", "--rm",
			"--cap-add", "SYS_PTRACE", "--cap-add", "CAP_SYS_ADMIN",
			"--net=host", "-v", dockerVolume, "-v", dataVolume,
			"hustoj", clientPath, solutionIDStr, clientIDStr,
		}
		cmd = exec.Command(cfg.DockerPath, args...)
	} else {
		cmd = exec.Command("/usr/bin/judge_client", solutionIDStr, clientIDStr, cfg.OJHome)
	}

	// This function call will be resolved at compile time to the correct
	// OS-specific implementation.
	if err := setResourceLimits(cmd, cfg); err != nil {
		AppLogger.Printf("WARN: Failed to set resource limits for solution %d: %v", solutionID, err)
	}

	output, err := cmd.CombinedOutput()
	if err != nil {
		AppLogger.Printf("ERROR: Failed to run client for sol %d: %v. Output: %s", solutionID, err, string(output))
	}
}
