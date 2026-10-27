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
#include <poll.h>
#include <errno.h>
#include <sys/syscall.h>

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

/* SIGCHLD 自唤醒管道：写端在信号处理函数里写，读端进 poll 集合。
 *
 * 原来主循环是 poll(ctl_fd, 1000) 后才 waitpid：子进程退出不会唤醒 poll，
 * 只能等满 1 秒超时。判题时每个测试点因此固定多花约 1 秒（20 个点就是 20 秒），
 * 而程序本身常常只跑几毫秒。 */
static int sigchld_pipe[2] = {-1, -1};

/* ===== kill namespace ===== */
static void kill_ns(int sig) {
    kill(-1, SIGKILL);
    _exit(128 + sig);
}

/* ===== SIGCHLD: 唤醒主循环 ===== */
static void on_sigchld(int sig) {
    (void)sig;
    int saved = errno;   /* 信号处理里必须保住 errno */
    char c = 1;
    ssize_t n = write(sigchld_pipe[1], &c, 1);
    (void)n;             /* 管道满时丢弃即可，主循环只是需要被唤醒 */
    errno = saved;
}

/* ===== signals ===== */
static void setup_signals() {
    signal(SIGINT, kill_ns);
    signal(SIGTERM, kill_ns);
    signal(SIGHUP, kill_ns);

    if (pipe2(sigchld_pipe, O_CLOEXEC) < 0) {
        perror("pipe2");
        return;      /* 退化回带超时的 poll，行为仍然正确，只是慢 */
    }
    /* 写端非阻塞，避免信号处理里阻塞 */
    fcntl(sigchld_pipe[1], F_SETFL, fcntl(sigchld_pipe[1], F_GETFL) | O_NONBLOCK);

    struct sigaction sa;
    memset(&sa, 0, sizeof(sa));
    sa.sa_handler = on_sigchld;
    sa.sa_flags = SA_RESTART | SA_NOCLDSTOP;  /* 只关心退出，不关心 stop/continue */
    sigemptyset(&sa.sa_mask);
    sigaction(SIGCHLD, &sa, NULL);
}

/* ===== drop to nobody ===== */
static void drop_nobody(void) {
    /* drop to UID/GID 65534 (nobody) */
    if (nobody_uid>0) setgid(nobody_uid);
    if (nobody_gid>0) setuid(nobody_gid);
    /* prevent privilege regain */
    prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0);
}

/* ===== pivot_root ===== */
static void setup_rootfs(const char *rootfs) {
    /* ensure rootfs is a mount point */
    mount(NULL, "/", NULL, MS_REC | MS_PRIVATE, NULL);

    if (chdir(rootfs) < 0) {
        perror("chdir rootfs");
        exit(1);
    }

    /* pivot_root(".", ".old") */
    if (mkdir(".old", 0755) != 0 && errno != EEXIST) {
        perror("mkdir .old");
        exit(1);
    }

    if (syscall(SYS_pivot_root, ".", ".old") < 0) {
        perror("pivot_root");
        exit(1);
    }

    chdir("/");

    /* must be inside mount namespace */
    if (mount("proc", "/proc", "proc", 0, NULL) < 0){
	perror("mount proc");
	exit(1);
    }


    umount2("/.old", MNT_DETACH);
    rmdir("/.old");
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
        /* privilege drop must occur before exec */
        drop_nobody();

        /* ptrace compatibility point */
        /* ptrace(PTRACE_TRACEME, 0, 0, 0); */
        execve(argv[i], &argv[i], envp);
        perror("execve");
        _exit(127);
    }

    /* ===== PID1 loop ===== */
    for (;;) {
        struct pollfd pfds[2] = {
            { .fd = ctl_fd,                .events = POLLIN },
            { .fd = sigchld_pipe[0],       .events = POLLIN },
        };

        /* 无限等待：控制命令或子进程退出（SIGCHLD 自唤醒）都会立刻返回。
         * fd 为 -1 的项会被 poll 忽略，所以两路缺一也能工作。
         * 两个 fd 都不可用时退回带超时的轮询，避免永久阻塞。 */
        int timeout = (ctl_fd >= 0 || sigchld_pipe[0] >= 0) ? -1 : 1000;
        int r = poll(pfds, 2, timeout);

        if (r > 0 && (pfds[0].revents & POLLIN)) {
            handle_control();
        }
        if (r > 0 && (pfds[1].revents & POLLIN)) {
            /* 清空唤醒字节，否则 poll 会一直返回 */
            char buf[64];
            while (read(sigchld_pipe[0], buf, sizeof(buf)) > 0) { }
        }

        int st;
        pid_t p = waitpid(-1, &st, WNOHANG);

        if (p == child) {
            kill(-1, SIGKILL);
            _exit(WIFEXITED(st) ? WEXITSTATUS(st) : 128);
        }
    }

    return 0;
}
