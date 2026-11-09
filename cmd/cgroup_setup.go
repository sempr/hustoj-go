package cmd

import (
	"fmt"
	"os"

	"github.com/sempr/hustoj-go/internal/shared"
	"github.com/spf13/cobra"
)

// cgroupSetupCmd 以 root 身份准备委派给判题用户的 cgroup v2 目录。
var cgroupSetupCmd = &cobra.Command{
	Use:   "cgroup-setup",
	Short: "Prepare cgroup v2 delegation for rootless judging",
	Long: `Create a cgroup v2 parent directory, enable the cpu/memory/pids
controllers on it, and chown it to the judge user.

Run this once as root (or via hustoj-cgroup.service):

  hustoj cgroup-setup --cgroup-root /sys/fs/cgroup/hustoj \
      --user judge --group judge

Afterwards the judge user can create per-run cgroups below that directory
without any root privileges.`,
	Run: func(cmd *cobra.Command, args []string) {
		err := shared.SetupDelegation(shared.DelegationOptions{
			Root:  cgroupSetupRoot,
			User:  cgroupSetupUser,
			Group: cgroupSetupGroup,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "cgroup-setup:", err)
			os.Exit(1)
		}
		fmt.Printf("cgroup delegation ready: %s (user=%s group=%s)\n",
			cgroupSetupRoot, cgroupSetupUser, groupOrDefault())
	},
}

var (
	cgroupSetupRoot  string
	cgroupSetupUser  string
	cgroupSetupGroup string
)

func groupOrDefault() string {
	if cgroupSetupGroup != "" {
		return cgroupSetupGroup
	}
	return cgroupSetupUser
}

func init() {
	rootCmd.AddCommand(cgroupSetupCmd)
	cgroupSetupCmd.Flags().StringVar(&cgroupSetupRoot, "cgroup-root", "/sys/fs/cgroup/hustoj", "delegated cgroup v2 parent directory")
	cgroupSetupCmd.Flags().StringVar(&cgroupSetupUser, "user", "judge", "judge user that will own the cgroup")
	cgroupSetupCmd.Flags().StringVar(&cgroupSetupGroup, "group", "", "judge group (default: same as --user)")
}
