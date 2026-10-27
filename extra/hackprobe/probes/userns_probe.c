/*
 * 探针 6：用户命名空间与危险系统调用。
 *
 * 依次尝试：
 *   unshare(CLONE_NEWUSER)  —— 若内核允许非特权用户命名空间，进程会在新
 *                              命名空间里拿到满权限（CAP_SYS_ADMIN 等），
 *                              那是加固时该用 seccomp 挡掉的；
 *   mount / setns / ptrace / init_module —— 都应失败。
 *
 * 期望：unshare 返回 EPERM（内核禁用了非特权 userns），其余全部失败。
 */
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <sched.h>
#include <stdio.h>
#include <string.h>
#include <sys/mount.h>
#include <sys/ptrace.h>
#include <unistd.h>

static void try_unshare_userns(void) {
    printf("unshare(CLONE_NEWUSER): ");
    if (unshare(CLONE_NEWUSER) == 0) {
        printf("成功 —— 拿到了新的用户命名空间（满权限只限该命名空间，"
               "但仍值得用 seccomp 挡掉）\n");
        return;
    }
    printf("失败: %s\n", strerror(errno));
}

static void try_mount(void) {
    printf("mount(tmpfs): ");
    if (mount("none", "/tmp", "tmpfs", 0, "size=1M") == 0) {
        printf("成功 —— 能挂载文件系统，需要 seccomp 限制\n");
        return;
    }
    printf("失败: %s\n", strerror(errno));
}

static void try_ptrace(void) {
    printf("ptrace(PTRACE_TRACEME): ");
    long r = ptrace(PTRACE_TRACEME, 0, NULL, NULL);
    if (r == 0) {
        printf("成功 —— 进程可以被跟踪（注意：判题器正用 ptrace 管控沙箱）\n");
        return;
    }
    printf("失败: %s\n", strerror(errno));
}

static void try_setns(void) {
    printf("setns(CLONE_NEWUSER): ");
    int fd = open("/proc/self/ns/user", 0);
    if (fd < 0) {
        printf("打不开 /proc/self/ns/user: %s\n", strerror(errno));
        return;
    }
    if (setns(fd, CLONE_NEWUSER) == 0) {
        printf("成功 —— 能加入其它用户命名空间\n");
    } else {
        printf("失败: %s\n", strerror(errno));
    }
    close(fd);
}

int main(void) {
    printf("== 用户命名空间与危险系统调用 ==\n");
    try_unshare_userns();
    try_mount();
    try_ptrace();
    try_setns();
    printf("（可用的 syscall 白名单检查：见 internal/run 里的 seccomp 接入点）\n");
    return 0;
}
