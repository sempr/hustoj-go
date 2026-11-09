#define _GNU_SOURCE
#include <unistd.h>
#include <signal.h>
#include <sys/wait.h>
#include <sys/prctl.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <fcntl.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <pwd.h>
#include <grp.h>
#include <sched.h>
#include <poll.h>
#include <errno.h>
#include <stddef.h>
#include <string.h>
#include <stdlib.h>
#include <linux/seccomp.h>
#include <linux/filter.h>
#include <linux/audit.h>
#include <sys/syscall.h>
#include <sys/signalfd.h>

/* 构建信息：由 makefile 通过 -D 注入，用来确认「这一份 tini 是哪个版本、
 * 用哪份 tini.c 编出来的」——嵌入式部署时最怕打进过期的旧构建。 */
#ifndef TINI_VERSION
#define TINI_VERSION "dev"
#endif
#ifndef TINI_SOURCE_MD5
#define TINI_SOURCE_MD5 "unknown"
#endif

static pid_t child = -1;
static int ctl_fd = -1;
static int nobody_uid = 65534;
static int nobody_gid = 65534;

/* SIGCHLD 用 signalfd 投递给主循环。
 *
 * 不再使用"信号处理函数写自唤醒管道"的写法：那种方式要么在读端阻塞排空时
 * 卡死（SIGCHLD 恰好在进入 poll 前到达），要么必须小心地把两端都设成
 * 非阻塞，容易再次踩坑。signalfd 把 SIGCHLD 变成可 poll 的 fd：
 *   - 信号被阻塞后由内核排队，poll 一定能看到，不存在信号/poll 竞态；
 *   - 读端本身是非阻塞的，排空到 EAGAIN 即止。
 */
static int sigchld_fd = -1;
static sigset_t sigchld_mask;

/* ===== kill namespace ===== */
static void kill_ns(int sig) {
    kill(-1, SIGKILL);
    _exit(128 + sig);
}

/* ===== signals ===== */
static void setup_signals() {
    signal(SIGINT, kill_ns);
    signal(SIGTERM, kill_ns);
    signal(SIGHUP, kill_ns);

    /* SIGCHLD 必须阻塞后才能交给 signalfd；子进程 exec 前会解除阻塞。 */
    sigemptyset(&sigchld_mask);
    sigaddset(&sigchld_mask, SIGCHLD);
    if (sigprocmask(SIG_BLOCK, &sigchld_mask, NULL) != 0) {
        perror("sigprocmask");
        return;      /* 退化回带超时的 poll，行为仍然正确，只是慢 */
    }
    sigchld_fd = signalfd(-1, &sigchld_mask, SFD_NONBLOCK | SFD_CLOEXEC);
    if (sigchld_fd < 0) {
        perror("signalfd");
    }
}

/* ===== seccomp: block obvious sandbox-escape syscalls =====
 *
 * Installed *after* drop_nobody()/PR_SET_NO_NEW_PRIVS and *before* execve.
 * The filter is inherited across exec, so it applies to the untrusted user
 * program. By default dangerous syscalls return EPERM; audit/kill modes can be
 * selected through OJ_SECCOMP_MODE (audit|enforce|kill).
 */
static void install_seccomp(void) {
    const char *mode = getenv("OJ_SECCOMP_MODE");
    unsigned int blocked = SECCOMP_RET_ERRNO | EPERM;
    if (mode && strcmp(mode, "audit") == 0) {
        blocked = SECCOMP_RET_LOG;
    } else if (mode && strcmp(mode, "kill") == 0) {
        blocked = SECCOMP_RET_KILL_PROCESS;
    }

#if defined(__x86_64__)
#define HUSTOJ_EXPECTED_ARCH AUDIT_ARCH_X86_64
#elif defined(__aarch64__)
#define HUSTOJ_EXPECTED_ARCH AUDIT_ARCH_AARCH64
#else
#define HUSTOJ_EXPECTED_ARCH 0
#endif

    const unsigned int clone_ns_flags =
        CLONE_NEWNS | CLONE_NEWCGROUP | CLONE_NEWUTS | CLONE_NEWIPC |
        CLONE_NEWUSER | CLONE_NEWPID | CLONE_NEWNET;

    struct sock_filter filter[] = {
        BPF_STMT(BPF_LD | BPF_W | BPF_ABS, offsetof(struct seccomp_data, arch)),
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, HUSTOJ_EXPECTED_ARCH, 1, 0),
        BPF_STMT(BPF_RET | BPF_K, SECCOMP_RET_KILL_PROCESS),
        BPF_STMT(BPF_LD | BPF_W | BPF_ABS, offsetof(struct seccomp_data, nr)),

#if defined(__x86_64__)
        /* x32 ABI 的 syscall 号带 0x40000000 位，直接查表会绕过下面的规则。 */
        BPF_JUMP(BPF_JMP | BPF_JGE | BPF_K, 0x40000000, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, SECCOMP_RET_KILL_PROCESS),
#endif

#ifdef __NR_clone
        /* clone 本身必须放行（线程/fork 都靠它），但带 CLONE_NEW* 的
         * clone 等价于 unshare，必须拒绝；否则可以在 no_new_privs 下
         * 新建 user namespace，拿到局部 CAP_SYS_ADMIN。 */
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_clone, 0, 4),
        BPF_STMT(BPF_LD | BPF_W | BPF_ABS, offsetof(struct seccomp_data, args[0])),
        BPF_STMT(BPF_ALU | BPF_AND | BPF_K, clone_ns_flags),
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, 0, 1, 0),
        BPF_STMT(BPF_RET | BPF_K, blocked),
        BPF_STMT(BPF_LD | BPF_W | BPF_ABS, offsetof(struct seccomp_data, nr)),
#endif

#ifdef __NR_clone3
        /* clone3 的参数在指针里，BPF 看不到 flags；返回 ENOSYS 让 glibc
         * 回退到 clone（线程创建仍可用），同时挡住直接调 clone3 建命名空间。 */
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_clone3, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, SECCOMP_RET_ERRNO | ENOSYS),
#endif

#ifdef __NR_mount
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_mount, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_umount2
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_umount2, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_pivot_root
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_pivot_root, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_chroot
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_chroot, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_unshare
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_unshare, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_setns
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_setns, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_ptrace
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_ptrace, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_bpf
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_bpf, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_reboot
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_reboot, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_kexec_load
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_kexec_load, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_kexec_file_load
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_kexec_file_load, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_init_module
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_init_module, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_finit_module
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_finit_module, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_delete_module
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_delete_module, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_open_by_handle_at
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_open_by_handle_at, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_name_to_handle_at
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_name_to_handle_at, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_userfaultfd
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_userfaultfd, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_open_tree
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_open_tree, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_move_mount
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_move_mount, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_fsopen
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_fsopen, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_fsconfig
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_fsconfig, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_fsmount
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_fsmount, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_fspick
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_fspick, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_mount_setattr
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_mount_setattr, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_io_uring_setup
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_io_uring_setup, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_io_uring_enter
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_io_uring_enter, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_io_uring_register
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_io_uring_register, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_keyctl
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_keyctl, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_add_key
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_add_key, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_request_key
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_request_key, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_perf_event_open
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_perf_event_open, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_pidfd_getfd
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_pidfd_getfd, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_process_vm_readv
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_process_vm_readv, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_process_vm_writev
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_process_vm_writev, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
#ifdef __NR_kcmp
        BPF_JUMP(BPF_JMP | BPF_JEQ | BPF_K, __NR_kcmp, 0, 1),
        BPF_STMT(BPF_RET | BPF_K, blocked),
#endif
        BPF_STMT(BPF_RET | BPF_K, SECCOMP_RET_ALLOW),
    };

    struct sock_fprog prog = {
        .len = (unsigned short)(sizeof(filter) / sizeof(filter[0])),
        .filter = filter,
    };
    /* PR_SET_NO_NEW_PRIVS has already been set by drop_nobody(). */
    if (prctl(PR_SET_SECCOMP, SECCOMP_MODE_FILTER, &prog) != 0) {
        perror("PR_SET_SECCOMP");
        _exit(126);
    }
}

/* ===== drop to nobody ===== */
static void drop_nobody(void) {
    /* 先清补充组，再改 gid/uid；顺序反了会留下 root 的补充组。
     * 任何一步失败都必须退出，不能带着 root 身份继续 exec。 */
    if (nobody_gid > 0) {
        if (setgroups(0, NULL) != 0) {
            perror("setgroups");
            _exit(126);
        }
        if (setresgid(nobody_gid, nobody_gid, nobody_gid) != 0) {
            perror("setresgid");
            _exit(126);
        }
    }
    if (nobody_uid > 0) {
        if (setresuid(nobody_uid, nobody_uid, nobody_uid) != 0) {
            perror("setresuid");
            _exit(126);
        }
    }
    /* prevent privilege regain */
    if (prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) != 0) {
        perror("PR_SET_NO_NEW_PRIVS");
        _exit(126);
    }
}

/* 子进程 exec 前关闭从 PID 1 继承的辅助 fd。
 *
 * 尤其是 ctl_fd：如果留给选手程序，它可以抢先 read() 掉父进程发来的
 * SIGTERM/SIGKILL 控制命令，使超时清理失效，把判题卡死。 */
static void close_inherited_fds(int stdin_fd, int stdout_fd, int stderr_fd) {
    int fds[] = {ctl_fd, stdin_fd, stdout_fd, stderr_fd, sigchld_fd};
    for (size_t i = 0; i < sizeof(fds) / sizeof(fds[0]); i++) {
        if (fds[i] > 2) close(fds[i]);
    }
}

/* ===== pivot_root ===== */
static void setup_rootfs(const char *rootfs) {
    /* ensure rootfs is a mount point */
    mount(NULL, "/", NULL, MS_REC | MS_PRIVATE, NULL);

    if (chdir(rootfs) < 0) {
        perror("chdir rootfs");
        exit(1);
    }

    /* pivot_root(".", put_old)
     *
     * 交互题会同时启动选手和交互器两个 run，它们共享同一个 rootfs。
     * 固定名字的 ".old" 会被两个 tini 互相踩：一方 pivot_root 后把 ".old"
     * rmdir 掉，另一方的 pivot_root 就返回 ENOENT（表现为偶发 WA/RE）。
     * 用 mkdtemp 生成每个 tini 独有的 put_old 目录，避免共享路径竞争。 */
    char olddir[] = ".old.XXXXXX";
    if (mkdtemp(olddir) == NULL) {
        perror("mkdtemp .old");
        exit(1);
    }

    if (syscall(SYS_pivot_root, ".", olddir) < 0) {
        perror("pivot_root");
        exit(1);
    }

    chdir("/");

    /* Mount a fresh procfs for *this* PID namespace.
     *
     * tini is PID 1 of the sandbox PID namespace and has CAP_SYS_ADMIN in the
     * sandbox user namespace, so mounting procfs here is allowed even though
     * the environment is rootless.  Doing it here (rather than bind-mounting
     * the host /proc from a privileged helper) also means the sandbox only
     * sees its own PIDs, and it gives runtimes such as Mono the /proc they
     * require.  The seccomp filter is installed later, in the forked child,
     * so tini itself can still call mount(). */
    mkdir("/proc", 0555);
    if (mount("proc", "/proc", "proc", MS_NOSUID | MS_NOEXEC | MS_NODEV, NULL) != 0) {
        perror("mount proc");
    }


    umount2(olddir, MNT_DETACH);
    rmdir(olddir);
}

/* ===== control ===== */
static void handle_control() {
    int cmd;
    if (read(ctl_fd, &cmd, sizeof(cmd)) <= 0)
        return;

    switch (cmd) {
        case 1: kill(-1, SIGKILL); break;
        case 2: kill(-1, SIGTERM); break;
        case 3: kill(-1, SIGKILL); _exit(0); break;
    }
}

/* ===== main ===== */
int main(int argc, char *argv[], char *envp[]) {
    int stdin_fd = -1, stdout_fd = -1, stderr_fd = -1;
    const char *rootfs = NULL;
    const char *cwd = NULL;

    int i = 1;

    /* --version 只供部署侧核对构建信息，遇到 "--" 之前扫描即可。
     * 正常运行时判题器传的都是 --参数，不会误触。 */
    for (int k = 1; k < argc; k++) {
        if (!strcmp(argv[k], "--")) break;
        if (!strcmp(argv[k], "--version")) {
            printf("tini %s (tini.c md5 %s)\n", TINI_VERSION, TINI_SOURCE_MD5);
            return 0;
        }
    }

    while (i < argc) {
        if (!strcmp(argv[i], "--control-fd")) ctl_fd = atoi(argv[++i]);
        else if (!strcmp(argv[i], "--stdin-fd")) stdin_fd = atoi(argv[++i]);
        else if (!strcmp(argv[i], "--stdout-fd")) stdout_fd = atoi(argv[++i]);
        else if (!strcmp(argv[i], "--stderr-fd")) stderr_fd = atoi(argv[++i]);
        else if (!strcmp(argv[i], "--uid")) nobody_uid = atoi(argv[++i]);
        else if (!strcmp(argv[i], "--gid")) nobody_gid = atoi(argv[++i]);
        else if (!strcmp(argv[i], "--cwd")) cwd = argv[++i];
        else if (!strcmp(argv[i], "--rootfs")) rootfs = argv[++i];
        else if (!strcmp(argv[i], "--")) { i++; break; }
        else break;
        i++;
    }

    /* tini is only meant to run as PID 1 of a dedicated PID namespace (the Go
     * runner creates one with CLONE_NEWPID). Running it directly on the host is
     * dangerous: its cleanup path calls kill(-1, SIGKILL), which would take
     * down host processes. Refuse in that case. */
    if (getpid() != 1) {
        fprintf(stderr, "tini: refusing to run outside a PID namespace (pid=%d); use unshare --pid --fork --mount-proc\\n", (int)getpid());
        return 78;
    }

    if (i >= argc) {
        fprintf(stderr, "no program\n");
        return 1;
    }

    setup_signals();

    /* ===== IO ===== */
    if (stdin_fd >= 0)  dup2(stdin_fd, 0);
    if (stdout_fd >= 0) dup2(stdout_fd, 1);
    if (stderr_fd >= 0) dup2(stderr_fd, 2);

    /* ===== mount namespace safety ===== */
    if (rootfs) {
        setup_rootfs(rootfs);
    }

    if (cwd) chdir(cwd);
    raise(SIGSTOP);

    /* ===== fork tracee ===== */
    child = fork();

    if (child == 0) {
        /* SIGCHLD 在父进程里为 signalfd 而阻塞；选手程序需要正常的默认行为。 */
        sigprocmask(SIG_UNBLOCK, &sigchld_mask, NULL);
        /* privilege drop must occur before exec */
        drop_nobody();
        close_inherited_fds(stdin_fd, stdout_fd, stderr_fd);
        install_seccomp();

        /* ptrace compatibility point */
        /* ptrace(PTRACE_TRACEME, 0, 0, 0); */
        execve(argv[i], &argv[i], envp);
        perror("execve");
        _exit(127);
    }

    /* ===== PID1 loop ===== */
    for (;;) {
        struct pollfd pfds[2] = {
            { .fd = ctl_fd,       .events = POLLIN },
            { .fd = sigchld_fd,   .events = POLLIN },
        };

        /* 无限等待：控制命令或子进程退出（signalfd 可读）都会立刻返回。
         * fd 为 -1 的项会被 poll 忽略，所以两路缺一也能工作。
         * 两个 fd 都不可用时退回带超时的轮询，避免永久阻塞。 */
        int timeout = (ctl_fd >= 0 || sigchld_fd >= 0) ? -1 : 1000;
        int r = poll(pfds, 2, timeout);

        if (r > 0 && (pfds[0].revents & POLLIN)) {
            handle_control();
        }
        if (r > 0 && (pfds[1].revents & POLLIN)) {
            /* signalfd 是非阻塞的，排空到 EAGAIN 即止。 */
            struct signalfd_siginfo si;
            while (read(sigchld_fd, &si, sizeof(si)) == (ssize_t)sizeof(si)) { }
        }

        /* 一次 waitpid 只回收一个子进程；孤儿进程会 reparent 到 PID 1，
         * 可能有多个同时退出，必须循环回收到没有可回收的为止。 */
        for (;;) {
            int st;
            pid_t p = waitpid(-1, &st, WNOHANG);
            if (p <= 0) break;
            if (p == child) {
                kill(-1, SIGKILL);
                _exit(WIFEXITED(st) ? WEXITSTATUS(st) : 128);
            }
        }
    }

    return 0;
}
