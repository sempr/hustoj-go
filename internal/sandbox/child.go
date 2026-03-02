package sandbox

import (
	"log/slog"
	"os"
	"runtime"
	"strings"
	"syscall"

	"github.com/sempr/hustoj-go/pkg/models"
	"golang.org/x/sys/unix"
)

var logger *slog.Logger

func ChildMain(cfg *models.SandboxArgs) {
	config = cfg
	runtime.LockOSThread()
	file3 := os.NewFile(uintptr(3), "fd3")
	logger = slog.New(slog.NewJSONHandler(file3, nil)).With("P", "child")
	chRoot()

	logger.Info("change to workdir")
	syscall.Chdir(config.Workdir)

	prepareMounts()
	changeFiles()

	logger.Info("before setrlimit")
	var rlim unix.Rlimit
	rlim.Max = 256 << 20
	rlim.Cur = rlim.Max
	unix.Setrlimit(unix.RLIMIT_FSIZE, &rlim)

	logger.Info("before setuid")
	unix.Setuid(65534)
	unix.Setgid(65534)

	logger.Info("before traceme")
	a, b, err := unix.Syscall(unix.SYS_PTRACE, uintptr(unix.PTRACE_TRACEME), 0, 0)
	logger.Info("traceme msg", "a", a, "b", b, "err", err)
	logger.Info("before stop myself")
	unix.Kill(os.Getpid(), unix.SIGSTOP)
	cmds := strings.Split(config.Command, " ")
	logger.Info("starting Exec", "cmds", cmds)
	if err := unix.Exec(cmds[0], cmds, os.Environ()); err != nil {
		logger.Error("exec error", "err", err)
	}
	logger.Info("maybe not used here")
}
