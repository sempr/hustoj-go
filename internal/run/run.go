package run

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"slices"
	"strings"
	"syscall"
	"unsafe"

	"github.com/sempr/hustoj-go/pkg/models"
	"golang.org/x/sys/unix"
)

func RunMain(cfg *models.SandboxArgs) {
	outerWorkdir := path.Join(cfg.Rootfs, cfg.Workdir)
	inputfile := path.Join(outerWorkdir, "data.in")
	outputfile := path.Join(outerWorkdir, "data.usr")
	errorfile := path.Join(outerWorkdir, "data.err")

	pr, pw, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	defer pr.Close()
	defer pw.Close()
	fi, err := os.Open(inputfile)
	fo, err := os.Create(outputfile)
	fe, err := os.Create(errorfile)

	cmdline := strings.Split(cfg.Command, " ")

	cmds := []string{
		"--control-fd", "3",
		"--stdin-fd", "4",
		"--stdout-fd", "5",
		"--stderr-fd", "6",
		"--uid", "0",
		"--gid", "0",
		"--cwd", cfg.Workdir,
		"--rootfs", cfg.Rootfs,
		"--",
	}
	cmds = append(cmds, cmdline...)

	cmd := exec.Command(
		"/usr/local/bin/tini",
		cmds...,
	)
	cmd.ExtraFiles = append(cmd.ExtraFiles, pr, fi, fo, fe)
	cmd.Env = append(cmd.Env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin")
	cmd.SysProcAttr = &unix.SysProcAttr{
		Cloneflags: unix.CLONE_NEWNS | unix.CLONE_NEWNET | unix.CLONE_NEWUTS | unix.CLONE_NEWIPC | unix.CLONE_NEWPID,
		Setpgid:    true,
		Ptrace:     true,
	}

	if err := cmd.Start(); err != nil {
		slog.Error("cmd start failed", "err", err)
	}

	mainPid := cmd.Process.Pid
	processCnt := 1
	firstWait := true
	pids := make(map[int]int)
	pids[mainPid] = 1
	for {
		var ws unix.WaitStatus
		var ru unix.Rusage
		pidTmp, err := unix.Wait4(-mainPid, &ws, 0, &ru)
		if err != nil {
			slog.Debug("")
			break
		}

		if firstWait {
			firstWait = false
			unix.PtraceSetOptions(mainPid, unix.PTRACE_O_EXITKILL|unix.PTRACE_O_TRACECLONE|unix.PTRACE_O_TRACEFORK|unix.PTRACE_O_TRACEVFORK|unix.PTRACE_O_TRACEVFORKDONE|unix.PTRACE_O_TRACEEXIT|unix.PTRACE_O_TRACESYSGOOD|unix.PTRACE_O_TRACESECCOMP|unix.PTRACE_O_TRACEEXEC)
		}

		slog.Debug("tracing", "pid", pidTmp, "ws", fmt.Appendf(nil, "%X", ws), "ru", ru)
		if ws.Exited() {
			slog.Debug("process exit ", "pid", pidTmp, "exitCode", ws.ExitStatus())
			if pidTmp == mainPid {
				break
			}
			continue
		}
		if ws.Signaled() {
			slog.Debug("process signaled", "pid", pidTmp, "signal", ws.Signal(), "status", ws.ExitStatus(), "exit", ws.Exited())
			if ws.Signal()&0x7f == unix.SIGXFSZ {
				break
			}
			break
		}
		if ws.Stopped() {
			slog.Debug("process stopped", "pid", pidTmp, "signal", ws.StopSignal(), "signal", ws.StopSignal()&0x7f)
			stopsig := ws.StopSignal()
			if stopsig == unix.SIGSEGV {
				type PtracePeekSigInfoArgs struct {
					Off   uint64
					Flags uint64
					Nr    uint64
				}
				args := PtracePeekSigInfoArgs{
					Off:   0,
					Flags: 0,
					Nr:    1,
				}
				var siginfo unix.Siginfo

				ret, _, errno := unix.Syscall6(
					unix.SYS_PTRACE,
					uintptr(unix.PTRACE_PEEKSIGINFO),
					uintptr(pidTmp),
					uintptr(unsafe.Pointer(&args)),
					uintptr(unsafe.Pointer(&siginfo)),
					0, 0,
				)
				slog.Info("siginfo peeked", "sig", ret, "errno", errno)
				if ret == 0 || errno != 0 {
					slog.Debug("ptrace peek failed, skipping siginfo", "ret", ret, "errno", errno)
				}
			}

			if stopsig == (unix.SIGTRAP | 0x80) {
				slog.Info("got stopsig=85, just do Ptrace")
				unix.PtraceCont(pidTmp, 0)
				continue
			}

			if stopsig == unix.SIGTRAP {
				eventNumber := int(ws >> 16)
				if eventNumber != 0 {
					cloneEvents := []int{unix.PTRACE_EVENT_CLONE, unix.PTRACE_EVENT_FORK, unix.PTRACE_EVENT_VFORK}
					isCloneEvent := false
					if slices.Contains(cloneEvents, eventNumber) {
						isCloneEvent = true
					}
					if isCloneEvent {
						slog.Info("trap event, clone/fork/vfork", "stopsig", stopsig, "eventnumber", eventNumber)
						processCnt++
					} else if eventNumber == unix.PTRACE_EVENT_EXEC {
						slog.Info("trace event: exec", "eventNumber", eventNumber)
						data, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", mainPid))
						args := strings.Split(string(data), "\x00")
						slog.Info("exec argv:", "args", args)
					} else if eventNumber == unix.PTRACE_EVENT_VFORK_DONE {
						slog.Info("trace event: vfork-done", "eventNumber", eventNumber)
					} else if eventNumber == unix.PTRACE_EVENT_EXIT {
						slog.Info("trace event: exit", "eventNumber", eventNumber)
					} else {
						slog.Info("trace event: todo", "eventNumber", eventNumber)
					}
					msg, err := unix.PtraceGetEventMsg(pidTmp)
					slog.Info("get event msg", "msg", msg, "err", err, "pid", pidTmp)
				} else {
					slog.Info("eventNumber is 0")
				}
			}
			err := unix.PtraceCont(pidTmp, int(stopsig))
			if err != nil {
				slog.Error("ptraceCont failed: ", "err", err, "pid", pidTmp)
			}
			if ws.StopSignal() == unix.SIGURG {
				unix.Kill(pidTmp, syscall.SIGCONT)
			}
		}

	}
}
