package sandbox

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"github.com/sempr/hustoj-go/pkg/models"
	"golang.org/x/sys/unix"
)

type TraceResult struct {
	Err  error
	Sig  unix.Signal
	Code int
}

var processCnt int = 1

func newTracerRunner(cfg *models.SandboxArgs, b *bytes.Buffer, childMainPid *int, ws *unix.WaitStatus, ru *unix.Rusage, tracerReady chan<- bool, cgroupPathPtr *string) func() TraceResult {
	var runTracer = func() TraceResult {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		slog.Info("runTracer")

		defer func() {
			slog.Info("final clear")
			pidt, err := unix.Wait4(-*childMainPid, nil, unix.WALL, nil)
			slog.Info("final wait", "err", err, "pid", pidt)
		}()

		selfPath, err := os.Executable()
		if err != nil {
			panic(err)
		}

		var childArgs []string
		childArgs = append(childArgs, "child")
		childArgs = append(childArgs, os.Args[2:]...)
		cmd := exec.Command(selfPath, childArgs...)
		cmd.ExtraFiles = append(cmd.ExtraFiles, os.Stderr)

		cmd.SysProcAttr = &syscall.SysProcAttr{
			Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWNET | syscall.CLONE_NEWUTS | syscall.CLONE_NEWIPC,
			Setpgid:    true,
		}

		if cfg.Stderr == "" && cfg.Stdout == "" {
			cmd.Stdout = b
			cmd.Stderr = b
		}
		slog.Info("start cmd")

		err = cmd.Start()
		if err != nil {
			slog.Info("cmd start failed", "err", err)
			panic(err)
		}
		slog.Info("after start cmd")

		*childMainPid = cmd.Process.Pid
		slog.Info("start wait for the stop")
		pidTmp, err := unix.Wait4(-*childMainPid, ws, 0, nil)
		slog.Info("before to stop signal", "pidTmp", pidTmp, "ws", fmt.Sprintf("%0X", *ws))
		if err != nil {
			panic(err)
		}

		memoryLimit := cfg.MemoryLimit << 10
		*cgroupPathPtr, err = setupCgroup(cfg.SolutionId, *childMainPid, memoryLimit)
		if err != nil {
			panic(err)
		}

		unix.PtraceSetOptions(*childMainPid,
			unix.PTRACE_O_EXITKILL|
				unix.PTRACE_O_TRACECLONE|
				unix.PTRACE_O_TRACEFORK|
				unix.PTRACE_O_TRACEVFORK|
				unix.PTRACE_O_TRACEVFORKDONE|
				unix.PTRACE_O_TRACEEXIT|
				unix.PTRACE_O_TRACESYSGOOD|
				unix.PTRACE_O_TRACESECCOMP|
				unix.PTRACE_O_TRACEEXEC,
		)

		defer func() {
			err := unix.PtraceDetach(*childMainPid)
			slog.Info("ptrace detach", "err", err)
		}()

		slog.Info("before contnue child process", "sig", ws.StopSignal(), "pidMain", *childMainPid)
		tracerReady <- true
		err = unix.PtraceCont(pidTmp, int(ws.StopSignal()))
		if err != nil {
			slog.Info("ptrace cont error", "err", err)
		}

		processCnt = 1
		for {
			slog.Debug("new wait here....")
			pidTmp, err := unix.Wait4(-*childMainPid, ws, syscall.WUNTRACED, ru)
			if err != nil {
				return TraceResult{err, 0, 0}
			}
			slog.Debug("tracing", "pid", pidTmp, "ws", fmt.Appendf(nil, "%X", *ws), "ru", *ru)
			if ws.Exited() {
				slog.Debug("process exit ", "pid", pidTmp, "exitCode", ws.ExitStatus())
				if pidTmp == *childMainPid {
					return TraceResult{nil, 0, 0}
				}
				continue
			}
			if *ws == 0x85 {
				data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pidTmp))
				slog.Info("status info ", "data", data, "err", err)
			}
			if ws.Signaled() {
				slog.Debug("process signaled", "pid", pidTmp, "signal", ws.Signal(), "status", ws.ExitStatus(), "exit", ws.Exited())
				if ws.Signal()&0x7f == unix.SIGXFSZ {
					return TraceResult{ErrOutputLimitExceeded, 0, 0}
				}
				return TraceResult{ErrRuntimeError, ws.Signal(), 0}
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
					eventNumber := int(*ws >> 16)
					if eventNumber != 0 {
						cloneEvents := []int{unix.PTRACE_EVENT_CLONE, unix.PTRACE_EVENT_FORK, unix.PTRACE_EVENT_VFORK}
						isCloneEvent := false
						for _, e := range cloneEvents {
							if e == eventNumber {
								isCloneEvent = true
								break
							}
						}
						if isCloneEvent {
							slog.Info("trap event, clone/fork/vfork", "stopsig", stopsig, "eventnumber", eventNumber)
							processCnt++
						} else if eventNumber == unix.PTRACE_EVENT_EXEC {
							slog.Info("trace event: exec", "eventNumber", eventNumber)
							data, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", *childMainPid))
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
	return runTracer
}
