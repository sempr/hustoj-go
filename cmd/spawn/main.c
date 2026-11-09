#define _GNU_SOURCE
#include <dirent.h>
#include <errno.h>
#include <libgen.h>
#include <fcntl.h>
#include <grp.h>
#include <linux/sched.h>
#include <pwd.h>
#include <sched.h>
#include <signal.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <linux/capability.h>
#include <sys/mount.h>
#include <sys/prctl.h>
#include <sys/resource.h>
#include <sys/stat.h>
#include <sys/syscall.h>
#include <syscall.h>
#include <sys/wait.h>
#include <unistd.h>

#ifndef CLONE_INTO_CGROUP
#define CLONE_INTO_CGROUP (1ULL << 38)
#endif

static char g_cgroupProc[4096];
static char g_cgroupRun[4096];

static void die(const char *fmt, ...) {
    va_list ap;
    va_start(ap, fmt);
    vfprintf(stderr, fmt, ap);
    va_end(ap);
    if (fmt[strlen(fmt)-1] != '\n') fprintf(stderr, "\n");
    exit(1);
}

static int write_file(const char *path, const char *data) {
    int fd = open(path, O_WRONLY | O_CLOEXEC);
    if (fd < 0) return -errno;
    size_t len = strlen(data);
    ssize_t n = write(fd, data, len);
    close(fd);
    if ((size_t)n != len) return -EIO;
    return 0;
}

static int check_executable(const char *path) {
    struct stat st;
    if (stat(path, &st) != 0) return -errno;
    if (!S_ISREG(st.st_mode)) return -EINVAL;
    if ((st.st_mode & 0111) == 0) return -EACCES;
    return 0;
}

static void absolute_path(char *buf, size_t sz, const char *p) {
    if (p[0] != '/') {
        char cwd[4096];
        if (!getcwd(cwd, sizeof(cwd))) die("getcwd failed\n");
        snprintf(buf, sz, "%s/%s", cwd, p);
    } else {
        snprintf(buf, sz, "%s", p);
    }
}

static int mkdir_p(const char *path, mode_t mode) {
    char buf[4096];
    strncpy(buf, path, sizeof(buf)-1);
    buf[sizeof(buf)-1] = '\0';
    size_t len = strlen(buf);
    if (len == 0) return -EINVAL;
    if (buf[len-1] == '/') buf[--len] = '\0';
    for (size_t i = 1; i <= len; ++i) {
        if (i != len && buf[i] != '/') continue;
        char saved = buf[i];
        buf[i] = '\0';
        if (mkdir(buf, 0755) != 0 && errno != EEXIST) { perror("mkdir"); return -errno; }
        buf[i] = saved;
    }
    return 0;
}

static void setup_cgroups(const char *base, const char *home, int *procDirFD) {
    if (stat(base, (struct stat[1]){0}) != 0) {
        fprintf(stderr, "hustoj-spawn: cgroup 根 %s 不存在，请先执行 cgroup-setup\n", base);
        exit(1);
    }
    char prefix[64];
    snprintf(prefix, sizeof(prefix), "spawn-%d-%d", (int)getpid(), (int)getppid());
    snprintf(g_cgroupProc, sizeof(g_cgroupProc), "%s/%s-proc", base, prefix);
    snprintf(g_cgroupRun,   sizeof(g_cgroupRun),   "%s/%s-run", base, prefix);

    if (mkdir(g_cgroupProc, 0755) != 0) { perror("mkdir procDir"); exit(1); }
    if (mkdir(g_cgroupRun, 0755) != 0) { perror("mkdir runParent"); exit(1); }
    // enable controllers on runParent
    char ctrl[4096];
    snprintf(ctrl, sizeof(ctrl), "%s/cgroup.subtree_control", g_cgroupRun);
    (void)write_file(ctrl, "+cpu +memory +pids");

    int fd = open(g_cgroupProc, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
    if (fd < 0) die("打开 cgroup 目录 %s 失败: %s\n", g_cgroupProc, strerror(errno));
    *procDirFD = fd;
}

static void drop_caps(void) {
    struct __user_cap_header_struct hdr = { _LINUX_CAPABILITY_VERSION_3, 0 };
    struct __user_cap_data_struct data[2] = { {0,0,0},{0,0,0} };
    if (syscall(SYS_capset, &hdr, data) != 0) {
        perror("capset (drop)");
    }
}

static void apply_rlimits(int maxRunning) {
    if (maxRunning < 1) maxRunning = 1;
    struct rlimit rl;
    rl.rlim_cur = 800; rl.rlim_max = 800;
    setrlimit(RLIMIT_CPU, &rl);
    rl.rlim_cur = 1024 * 1048576ULL; rl.rlim_max = rl.rlim_cur;
    setrlimit(RLIMIT_FSIZE, &rl);
    rl.rlim_cur = (rlim_t)800 * maxRunning; rl.rlim_max = rl.rlim_cur;
    setrlimit(RLIMIT_NPROC, &rl);
    rl.rlim_cur = (rlim_t)1048576 << 15; rl.rlim_max = rl.rlim_cur;
    setrlimit(RLIMIT_AS, &rl);
}

static void cleanup_cgroups(void) {
    rmdir(g_cgroupProc);
    rmdir(g_cgroupRun);
}

static volatile pid_t g_child = -1;
static int g_fallback = 0;

static void handle_signal(int sig) {
    if (g_child > 0) {
        // Kill entire process group in child's PID namespace
        kill(-g_child, sig);
    }
}

int main(int argc, char **argv, char **envp) {
    fprintf(stderr, "DEBUG spawn main: argc=%d\n", argc); fflush(stderr);
    for (int k=0; k<argc; k++) {
        fprintf(stderr, "DEBUG spawn main: argv[%d]=%s\n", k, argv[k]); fflush(stderr);
    }
    const char *home = "/home/judge";
    const char *cgroupRootFlag = "";
    int maxRunning = 3;
    const char *label = "judge";
    const char *exe = "";
    int allowAny = (getenv("HUSTOJ_SPAWN_ALLOW_ANY") != 0);
    int i = 1;

    /* strip leading -- quickly */
    for (; i < argc; ++i) {
        if (!strcmp(argv[i], "--")) { i++; break; }
        if (!strcmp(argv[i], "--home")) home = argv[++i];
        else if (!strcmp(argv[i], "--cgroup-root")) cgroupRootFlag = argv[++i];
        else if (!strcmp(argv[i], "--max-running")) maxRunning = atoi(argv[++i]);
        else if (!strcmp(argv[i], "--label")) label = argv[++i];
        else if (!strcmp(argv[i], "--exe")) exe = argv[++i];
        else {
            fprintf(stderr, "hustoj-spawn: 未知参数 %s\n", argv[i]);
            return 125;
        }
    }

    if (exe[0] == '\0') {
        fprintf(stderr, "hustoj-spawn: 缺少 --exe\n");
        return 125;
    }
    char exeAbs[4096];
    absolute_path(exeAbs, sizeof(exeAbs), exe);
    char *bname = basename(exeAbs);
    if (!(allowAny || strcmp(bname, "hustoj") == 0)) {
        fprintf(stderr, "hustoj-spawn: 拒绝执行 %s：只允许 hustoj 本体（HUSTOJ_SPAWN_ALLOW_ANY=1 可放宽）\n", exeAbs);
        return 125;
    }
    /* check executable */
    if (check_executable(exeAbs) != 0) {
        fprintf(stderr, "hustoj-spawn: %s 不可执行\n", exeAbs);
        return 125;
    }

    int uid = getuid();
    int gid = getgid();
    if (uid == 0) fprintf(stderr, "hustoj-spawn[%s]: running as root; userns maps 0->0\n", label);

    /* capability check: need setuid/setgid/dac_override effective */
    /* (skip hard check; rely on systemd file caps) */

    /* resolve cgroup root */
    char cgroupRoot[4096];
    if (cgroupRootFlag[0]) strncpy(cgroupRoot, cgroupRootFlag, sizeof(cgroupRoot)-1);
    else {
        const char *envRoot = getenv("OJ_CGROUP_ROOT");
        if (envRoot && envRoot[0]) strncpy(cgroupRoot, envRoot, sizeof(cgroupRoot)-1);
        else {
            /* default <mount>/hustoj */
            /* read /proc/self/mountinfo quickly: find "cgroup2" fs */
            FILE *f = fopen("/proc/self/mountinfo", "r");
            if (!f) die("无法读取 mountinfo\n");
            char line[8192]; char mount[4096] = "";
            while (fgets(line, sizeof(line), f)) {
                if (!strstr(line, " - cgroup2 ")) continue;
                char *p = strstr(line, " - ");
                *p = 0;
                char *fields = line;
                /* fields: mnt_point is 5th space-separated */
                char tmp[4096];
                sscanf(fields, "%*s %*s %*s %*s %4095s", tmp);
                /* unescape not needed for typical */
                strcpy(mount, tmp);
                break;
            }
            fclose(f);
            if (!mount[0]) die("找不到 cgroup2 挂载点\n");
            snprintf(cgroupRoot, sizeof(cgroupRoot), "%s/hustoj", mount);
        }
    }
    cgroupRoot[sizeof(cgroupRoot)-1] = 0;

    /* stat base */
    struct stat st;
    if (stat(cgroupRoot, &st) != 0) die("委派 cgroup %s 不存在；请先 cgroup-setup\n", cgroupRoot);

    /* setup cgroups */
    int procDirFD = -1;
    setup_cgroups(cgroupRoot, home, &procDirFD);

    /* enable controllers already in setup */
    apply_rlimits(maxRunning);

    /* clear supplementary groups */
    setgroups(0, NULL);

    /* build child env */
    char **env = NULL;
    size_t envCap = 0, envCount = 0;
#define ENV_ADD(kv) do { \
        if (envCount+1 >= envCap) { envCap = envCap?envCap*2:64; env = realloc(env, envCap*sizeof(char*)); } \
        env[envCount++] = strdup(kv); env[envCount] = NULL; \
    } while(0)
    char buf[8192];
    snprintf(buf, sizeof(buf), "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"); ENV_ADD(buf);
    snprintf(buf, sizeof(buf), "HOME=%s", home); ENV_ADD(buf);
    snprintf(buf, sizeof(buf), "HUSTOJ_IN_USERNS=1"); ENV_ADD(buf);
    snprintf(buf, sizeof(buf), "OJ_CGROUP_ROOT=%s", g_cgroupRun); ENV_ADD(buf);
    static const char *wl[] = {"TZ","TMPDIR","LANG","LOG_LEVEL","OJ_SECCOMP_MODE","OJ_MAX_RUNNING"};
    for (int w=0; w<6; ++w) {
        const char *v = getenv(wl[w]);
        if (v) {
            snprintf(buf, sizeof(buf), "%s=%s", wl[w], v);
            ENV_ADD(buf);
        }
    }
#undef ENV_ADD

    /* clone into cgroup userns */
    int clone_flags = CLONE_NEWUSER;
    struct clone_args ca = {0};
    ca.flags = clone_flags | CLONE_INTO_CGROUP;
    ca.exit_signal = SIGCHLD;
    ca.cgroup = procDirFD;

    /* Two handshakes:
     *   readyfd: child -> parent, "I have created my user namespace"
     *   gofd:    parent -> child, "uid/gid maps are installed, continue"
     *
     * clone3(CLONE_NEWUSER) puts the child in the new namespace immediately,
     * so the parent can write the maps right away.  The fork fallback is
     * different: the child creates the namespace itself, and the parent MUST
     * wait for that before writing /proc/<pid>/uid_map.  Otherwise the write
     * races into the initial user namespace, fails silently, and the detached
     * child later gets EPERM from mount("tmpfs").  That was the intermittent
     * "failed to mount tmpfs: operation not permitted" failure.
     */
    int gofd[2], readyfd[2];
    if (pipe(gofd) != 0) die("pipe failed: %s\n", strerror(errno));
    if (pipe(readyfd) != 0) die("pipe failed: %s\n", strerror(errno));

    pid_t pid = syscall(SYS_clone3, &ca, sizeof(ca));
    if (pid < 0) {
        g_fallback = 1;
        pid = fork();
        if (pid < 0) die("fork failed: %s\n", strerror(errno));
        if (pid > 0) {
            char ppath[4096];
            snprintf(ppath, sizeof(ppath), "%s/cgroup.procs", g_cgroupProc);
            char pidbuf[64];
            snprintf(pidbuf, sizeof(pidbuf), "%d", (int)pid);
            (void)write_file(ppath, pidbuf);
        }
    }
    if (pid > 0) {
        close(gofd[0]);
        close(readyfd[1]);

        if (g_fallback) {
            char b;
            ssize_t rn;
            do { rn = read(readyfd[0], &b, 1); } while (rn < 0 && errno == EINTR);
            if (rn != 1) {
                fprintf(stderr, "hustoj-spawn: 子进程未创建 user namespace 就退出\n");
                close(gofd[1]);
                close(readyfd[0]);
                kill(pid, SIGKILL);
                waitpid(pid, NULL, 0);
                cleanup_cgroups();
                return 125;
            }
        }
        close(readyfd[0]);

        char ppath[4096], mapb[256];
        int werr;

        snprintf(ppath, sizeof(ppath), "/proc/%d/setgroups", (int)pid);
        int sfd = open(ppath, O_WRONLY | O_CLOEXEC);
        if (sfd >= 0) { (void)write(sfd, "allow\n", 6); close(sfd); }

        snprintf(ppath, sizeof(ppath), "/proc/%d/uid_map", (int)pid);
        snprintf(mapb, sizeof(mapb), "0 %d 1\n65534 65534 1\n", uid);
        werr = write_file(ppath, mapb);
        if (werr != 0) {
            fprintf(stderr, "hustoj-spawn: 写 %s 失败: %s\n", ppath, strerror(-werr));
            close(gofd[1]);
            kill(pid, SIGKILL);
            waitpid(pid, NULL, 0);
            cleanup_cgroups();
            return 125;
        }

        snprintf(ppath, sizeof(ppath), "/proc/%d/gid_map", (int)pid);
        snprintf(mapb, sizeof(mapb), "0 %d 1\n65534 65534 1\n", gid);
        werr = write_file(ppath, mapb);
        if (werr != 0) {
            fprintf(stderr, "hustoj-spawn: 写 %s 失败: %s\n", ppath, strerror(-werr));
            close(gofd[1]);
            kill(pid, SIGKILL);
            waitpid(pid, NULL, 0);
            cleanup_cgroups();
            return 125;
        }

        char c = 1;
        (void)write(gofd[1], &c, 1);
        close(gofd[1]);
    }
    g_child = pid;

    if (pid == 0) {
        /* child */
        close(gofd[1]);
        close(readyfd[0]);
        close(procDirFD);

        if (g_fallback) {
            if (unshare(CLONE_NEWUSER) != 0) { perror("unshare CLONE_NEWUSER"); _exit(126); }
            char c = 1;
            if (write(readyfd[1], &c, 1) != 1) _exit(126);
        }
        close(readyfd[1]);

        /* maps installed by parent; wait for them before touching mounts */
        char _syncb[4];
        do { ssize_t rn = read(gofd[0], _syncb, 1); if (rn == 1) break; if (rn < 0 && errno == EINTR) continue; _exit(126); } while (1);
        close(gofd[0]);

        /* unshare mount ns */
        if (unshare(CLONE_NEWNS) != 0) { perror("unshare CLONE_NEWNS"); _exit(126); }
        mount(NULL, "/", NULL, MS_REC | MS_PRIVATE, NULL);

        apply_rlimits(maxRunning);
        fprintf(stderr, "DEBUG spawn: argc=%d, i=%d, argc-i=%d\n", argc, i, argc-i);
        for (int k=i; k<argc; k++) {
            fprintf(stderr, "DEBUG spawn: argv[%d]=%s\n", k, argv[k]);
        }
        if (i >= argc) {
            /* 没有额外参数：仍 exec 目标二进制 */
            char *empty[] = { NULL };
            execve(exeAbs, empty, env);
        } else {
            char *newargv[256];
            int n=0;
            newargv[n++]=exeAbs;
            for (int j=i; j<argc && n<255; j++) newargv[n++]=argv[j];
            newargv[n]=NULL;
            fprintf(stderr, "DEBUG spawn: execve with %d args\n", n);
            for (int k=0; k<n; k++) {
                fprintf(stderr, "DEBUG spawn: newargv[%d]=%s\n", k, newargv[k]);
            }
            execve(exeAbs, newargv, env);
        }
        perror("execve");
        _exit(127);
    }

    close(procDirFD);

    /* parent: drop caps */
    drop_caps();

    /* install signal handlers */
    struct sigaction sa;
    memset(&sa, 0, sizeof(sa));
    sa.sa_handler = handle_signal;
    sigemptyset(&sa.sa_mask);
    sigaction(SIGTERM, &sa, NULL);
    sigaction(SIGINT, &sa, NULL);
    sigaction(SIGHUP, &sa, NULL);
    sigaction(SIGQUIT, &sa, NULL);

    /* wait child */
    g_child = pid;
    int status = 0;
    while (1) {
        int ret = waitpid(pid, &status, 0);
        if (ret < 0) {
            if (errno == EINTR) {
                continue;  // Interrupted by signal, retry
            }
            fprintf(stderr, "hustoj-spawn: waitpid failed: %s\n", strerror(errno));
            cleanup_cgroups();
            return 125;
        }
        break;
    }
    cleanup_cgroups();
    if (WIFEXITED(status)) return WEXITSTATUS(status);
    if (WIFSIGNALED(status)) return 128 + WTERMSIG(status);
    return 125;
}
