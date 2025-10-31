package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

type Config struct {
	Command string
	Rootfs  string
	Workdir string
	Stdin   string
	Stdout  string
	Stderr  string
}

var config Config

func initConfig() {
	fmt.Printf("child: %v\n", os.Args)
	flag.StringVar(&config.Rootfs, "rootfs", "/tmp", "")
	flag.StringVar(&config.Command, "cmd", "/bin/false", "")
	flag.StringVar(&config.Workdir, "cwd", "/code", "")
	flag.StringVar(&config.Stdin, "stdin", "", "")
	flag.StringVar(&config.Stdout, "stdout", "", "")
	flag.StringVar(&config.Stderr, "stderr", "", "")
	if os.Args[1] == "child" {
		flag.CommandLine.Parse(os.Args[2:])
	} else {
		flag.Parse()
	}
}

func chRoot() {
	newRootPath := config.Rootfs
	fmt.Println("Shim：Marking root as private (MS_PRIVATE)...")
	if err := syscall.Mount("none", "/", "none", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
		fmt.Fprintf(os.Stderr, "Shim 错误: Failed to mark root private: %v\n", err)
		os.Exit(1)
	}
	// 3. 确保新的根是一个挂载点 (旧的第2步)
	fmt.Printf("Shim：Bind-mounting %s ...\n", newRootPath)
	if err := syscall.Mount(newRootPath, newRootPath, "bind", syscall.MS_BIND, ""); err != nil {
		fmt.Fprintf(os.Stderr, "Shim 错误: bind-mount %s 失败: %v\n", newRootPath, err)
		os.Exit(1)
	}
	// 3. 创建一个目录来存放旧的 root
	//    这个目录必须在 newRootPath 里面
	putOldPath := filepath.Join(newRootPath, ".old_root")
	fmt.Printf("Shim：创建旧 root 挂载点 %s ...\n", putOldPath)
	if err := os.MkdirAll(putOldPath, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "Shim 错误: 创建 %s 失败: %v\n", putOldPath, err)
		os.Exit(1)
	}

	// 4. 执行 PivotRoot
	fmt.Println("Shim：执行 syscall.PivotRoot...")
	if err := syscall.PivotRoot(newRootPath, putOldPath); err != nil {
		fmt.Fprintf(os.Stderr, "Shim 错误: PivotRoot 失败: %v newRootPath=%s\n", err, newRootPath)
		os.Exit(1)
	}

	// 5. 切换到新的根目录
	fmt.Println("Shim：Chdir 到新的 '/' ...")
	if err := syscall.Chdir("/"); err != nil {
		fmt.Fprintf(os.Stderr, "Shim 错误: Chdir('/') 失败: %v\n", err)
		os.Exit(1)
	}
	// 6. 卸载旧的 root
	//    这是 pivot_root 最关键的安全步骤！
	//    旧的 root 现在在 /.old_root (注意：/ 是新的根)
	fmt.Println("Shim：卸载 /.old_root ...")
	if err := syscall.Unmount("/.old_root", syscall.MNT_DETACH); err != nil {
		fmt.Fprintf(os.Stderr, "Shim 错误: Unmount '/.old_root' 失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Shim： 删除目录 /.old_root ...")
	// 7. (可选) 删除临时目录
	if err := os.RemoveAll("/.old_root"); err != nil {
		fmt.Fprintf(os.Stderr, "Shim 警告: RemoveAll '/.old_root' 失败: %v\n", err)
	}
}

func changeFiles() {
	slog.Info("redirect files")
	if config.Stdin != "" {
		fi, _ := os.OpenFile(config.Stdin, os.O_RDONLY, 0644)
		unix.Dup2(int(fi.Fd()), int(os.Stdin.Fd()))
	}
	if config.Stdout != "" {
		fo, _ := os.OpenFile(config.Stdout, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		unix.Dup2(int(fo.Fd()), int(os.Stdout.Fd()))
	}
	if config.Stderr != "" {
		fe, _ := os.OpenFile(config.Stderr, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		unix.Dup2(int(fe.Fd()), int(os.Stderr.Fd()))
	}
}

func prepareMounts() {
	slog.Info("mount paths")
	unix.Mount("proc", "/proc", "proc", 0, "")
	unix.Mount("tmpfs", "/dev", "tmpfs", 0, "")
	unix.Mount("devpts", "/dev/pts", "devpts", 0, "")
	unix.Mount("sysfs", "/sys", "sysfs", 0, "")

	slog.Info("prepare /dev/null")
	os.Remove(os.DevNull)
	unix.Mknod("/dev/null", syscall.S_IFCHR|0666, int(unix.Mkdev(1, 3)))
	unix.Chmod("/dev/null", 0666)
}

func runChild() {
	initConfig()
	chRoot()

	slog.Info("change to workdir")
	syscall.Chdir(config.Workdir)

	prepareMounts()
	changeFiles()
	// run command
	unix.Setuid(65534)
	unix.Setgid(65534)

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// start traceme then raise stop
	unix.Syscall(unix.SYS_PTRACE, uintptr(unix.PTRACE_TRACEME), 0, 0)
	unix.Kill(os.Getpid(), unix.SIGSTOP)

	cmds := strings.Split(config.Command, " ")
	if err := syscall.Exec(cmds[0], cmds, os.Environ()); err != nil {
		panic(err)
	}
	fmt.Println("compile ok")
}

func runParent() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	slog.SetLogLoggerLevel(slog.LevelDebug)
	selfPath, err := os.Executable()
	if err != nil {
		panic(err)
	}
	var childArgs []string
	childArgs = append(childArgs, "child")
	childArgs = append(childArgs, os.Args[1:]...)
	cmd := exec.Command(selfPath, childArgs...)

	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWNET | syscall.CLONE_NEWPID | syscall.CLONE_NEWUTS | syscall.CLONE_NEWIPC,
		Setpgid:    true,
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stdout
	// cmd.ExtraFiles = append(cmd.ExtraFiles, )
	cmd.Start()

	var ws unix.WaitStatus
	var childMainPid = cmd.Process.Pid
	unix.Wait4(-childMainPid, &ws, 0, nil)
	// TODO: prepare more here
	// prepare cgroups here

	// set options
	unix.PtraceSetOptions(childMainPid,
		unix.PTRACE_O_EXITKILL|
			unix.PTRACE_O_TRACECLONE|
			unix.PTRACE_O_TRACEFORK|
			unix.PTRACE_O_TRACEVFORK|
			unix.PTRACE_O_TRACEVFORKDONE|
			unix.PTRACE_O_TRACEEXIT|
			unix.PTRACE_O_TRACEEXEC|
			unix.PTRACE_O_TRACESYSGOOD|
			unix.PTRACE_O_TRACESECCOMP,
	)
	// continue
	unix.PtraceCont(childMainPid, int(ws.StopSignal()))
	var ru unix.Rusage
	for {
		pidTmp, err := unix.Wait4(-childMainPid, &ws, 0, &ru)
		if err != nil {
			panic(err)
		}
		slog.Debug("tracing", "pid", pidTmp, "ws", ws, "ru", ru)
		if ws.Exited() {
			slog.Debug("process exit ", "pid", pidTmp)
			if pidTmp == childMainPid {
				break
			}
			continue
		}
		if ws.Signaled() {
			slog.Debug("process signaled", "pid", pidTmp, "signal", ws.Signal())
			break
		}
		if ws.Stopped() {
			unix.PtraceCont(pidTmp, int(ws.StopSignal()))
		}
	}
	slog.Info("Time Used: ", "sys", ru.Stime, "user", ru.Utime, "st", ws.ExitStatus())
}

func main() {
	if os.Args[1] == "child" {
		runChild()
	} else {
		runParent()
	}
}
