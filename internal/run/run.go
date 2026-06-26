package run

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/sempr/hustoj-go/internal/shared"
	"github.com/sempr/hustoj-go/pkg/models"
	"golang.org/x/sys/unix"
)

func RunMain(cfg *models.SandboxArgs) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
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
		// "--uid", "0",
		// "--gid", "0",
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
		return
	}

	stopFunc := func() error {
		// 写 int32 3 (little‑endian)
		b := make([]byte, 4)
		binary.LittleEndian.PutUint32(b, 2)
		n, err := pw.Write(b)
		if err != nil {
			slog.Warn("写 control‑fd 失败", "err", err)
			return err
		}
		if n != 4 {
			return fmt.Errorf("control‑fd 写入了 %d 字节，期望 4", n)
		}
		slog.Info("已写入 control‑fd，命令 3 (SIGTERM)")
		return nil
	}

	mainPid := cmd.Process.Pid
	processCnt := 1
	pids := make(map[int]int)
	pids[mainPid] = 1
	ptree := make(map[int][]int)
	ptreename := make(map[int]string)
	ptree[mainPid] = []int{}
	ptreename[mainPid] = "main"

	// first wait here
	var ws unix.WaitStatus
	pidTmp, err := unix.Wait4(-mainPid, &ws, 0, nil)
	slog.Debug("tracing(first wait)", "pid", pidTmp, "ws", fmt.Appendf(nil, "%X", ws))
	err = unix.PtraceSetOptions(mainPid, unix.PTRACE_O_EXITKILL|unix.PTRACE_O_TRACECLONE|unix.PTRACE_O_TRACEFORK|unix.PTRACE_O_TRACEVFORK|unix.PTRACE_O_TRACEVFORKDONE|unix.PTRACE_O_TRACEEXIT|unix.PTRACE_O_TRACESYSGOOD|unix.PTRACE_O_TRACESECCOMP|unix.PTRACE_O_TRACEEXEC)
	if err != nil {
		slog.Error("ptrace setoptions failed", "mainPid", mainPid, "pidTmp", pidTmp)
	}

	cg, err := shared.NewCgroup(cfg.SolutionId, mainPid, cfg.MemoryLimit*1024, slog.Default())
	if err != nil {
		slog.Error("cg create failed", "err", err)
		return
	}
	defer cg.Cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var ccerr error = nil
	go func() {
		if ccerr = cg.RunCPUChecker(
			ctx,
			time.Duration(cfg.TimeLimit*int(time.Millisecond)),
			time.Duration(3*cfg.TimeLimit*int(time.Millisecond)),
			slog.Default(),
			stopFunc,
		); ccerr != nil {
			slog.Info("ccerror", "ccerr", ccerr)
		}
	}()
	startTime := time.Now()
	if err := unix.PtraceCont(pidTmp, int(ws.StopSignal())); err != nil {
		slog.Error("ptraceCont error", "err", err)
	}

	for {
		var ws unix.WaitStatus
		var ru unix.Rusage
		pidTmp, err := unix.Wait4(-mainPid, &ws, 0, &ru)
		if err != nil {
			slog.Error("wait4 error", "pid", pidTmp, "err", err)
			break
		}

		slog.Debug("tracing", "pid", pidTmp, "ws", fmt.Appendf(nil, "%X", ws))
		if ws.Exited() {
			slog.Debug("process exit ", "pid", pidTmp, "exitCode", ws.ExitStatus())
			processCnt--
			if processCnt == 1 {
				stopFunc()
				continue
			}
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
			}
			if stopsig == (unix.SIGTRAP | 0x80) {
				slog.Info("got stopsig=85, just do Ptrace")
				unix.PtraceCont(pidTmp, 0)
				continue
			}

			if stopsig == unix.SIGTRAP {
				eventNumber := int(ws >> 16)
				if eventNumber != 0 {
					msg, err := unix.PtraceGetEventMsg(pidTmp)
					slog.Info("get event msg", "msg", msg, "err", err, "pid", pidTmp, "eventNumber", eventNumber)

					cloneEvents := []int{unix.PTRACE_EVENT_CLONE, unix.PTRACE_EVENT_FORK, unix.PTRACE_EVENT_VFORK}
					isCloneEvent := false
					if slices.Contains(cloneEvents, eventNumber) {
						isCloneEvent = true
					}
					if isCloneEvent {
						slog.Info("trap event, clone/fork/vfork", "stopsig", stopsig, "eventnumber", eventNumber)
						ptreename[int(msg)] = ptreename[pidTmp]
						ptree[int(msg)] = []int{}
						ptree[pidTmp] = append(ptree[pidTmp], int(msg))
						processCnt++
					} else if eventNumber == unix.PTRACE_EVENT_EXEC {
						slog.Info("trace event: exec", "eventNumber", eventNumber)
						data, _ := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", msg))
						args := strings.Split(string(data), "\x00")
						slog.Info("exec argv:", "args", args, "pidTmp", pidTmp, "msg", msg)
						ptreename[pidTmp] = strings.Join(args, " ")
					} else if eventNumber == unix.PTRACE_EVENT_VFORK_DONE {
						slog.Info("trace event: vfork-done", "eventNumber", eventNumber)
					} else if eventNumber == unix.PTRACE_EVENT_EXIT {
						slog.Info("trace event: exit", "eventNumber", eventNumber)
					} else {
						slog.Info("trace event: todo", "eventNumber", eventNumber)
					}
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

	var showPtree func(int, int)
	showPtree = func(pid int, depth int) {
		for range depth * 2 {
			fmt.Printf(" ")
		}
		fmt.Println(pid, " ", ptreename[pid])
		for _, v := range ptree[pid] {
			showPtree(v, depth+1)
		}
	}
	fmt.Printf("ccerr = %v\n", ccerr)
	// showPtree(mainPid, 0)
	tt, err1 := cg.ReadCPUtime()
	cg.ReadMemory()
	slog.Error("CPU: ", "time", tt, "error", err1, "walltime", time.Since(startTime))
}
