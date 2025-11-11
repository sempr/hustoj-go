package daemon

import (
	"fmt"
	"os"
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
	selfexe, _ := os.Executable()
	fmt.Printf("%s client %s %s %s\n", selfexe, solutionIDStr, clientIDStr, cfg.OJHome)
	cmd = exec.Command(selfexe, "client", solutionIDStr, clientIDStr, cfg.OJHome)

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
