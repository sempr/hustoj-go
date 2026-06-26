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

static pid_t child = -1;
static int ctl_fd = -1;
static int nobody_uid = 65534;
static int nobody_gid = 65534;

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

    /* must be inside mount namespace */
    mount("proc", "/proc", "proc", 0, NULL);

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
        struct pollfd pfd = {
            .fd = ctl_fd,
            .events = POLLIN,
        };

        int r = poll(&pfd, 1, 1000);

        if (r > 0 && (pfd.revents & POLLIN)) {
            handle_control();
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
