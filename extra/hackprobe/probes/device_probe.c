/*
 * 探针 4：设备节点与敏感文件。
 *
 * 逐个尝试打开宿主机的设备与敏感路径，报告哪些能打开。
 * 期望：全部失败（沙箱的 rootfs 里没有这些设备）。
 * 若 /dev/mem 之类能打开，说明 rootfs 里混进了宿主机的设备节点 —— 那就
 * 能直接读写宿主内存。
 */
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <unistd.h>

static const char *PATHS[] = {
    "/dev/mem", "/dev/kmem", "/dev/port", "/dev/kmsg", "/dev/sd*",
    "/dev/nvme0n1", "/dev/ttyS0", "/dev/tty0", "/dev/console",
    "/proc/sysrq-trigger", "/proc/kcore", "/proc/kallsyms",
    "/proc/self/mem", "/etc/shadow", "/root/.ssh/id_rsa",
    "/sys/kernel/uevent_seqnum", "/sys/firmware", NULL,
};

int main(void) {
    printf("== 设备节点与敏感文件 ==\n");
    int opened = 0;
    for (int i = 0; PATHS[i] != NULL; i++) {
        int fd = open(PATHS[i], O_RDONLY);
        if (fd >= 0) {
            struct stat st;
            char extra[128] = "";
            if (fstat(fd, &st) == 0)
                snprintf(extra, sizeof extra, "（设备号 %lu）", (unsigned long)st.st_rdev);
            printf("打开 %-28s 成功%s\n", PATHS[i], extra);
            close(fd);
            opened++;
        } else {
            printf("打开 %-28s 失败: %s\n", PATHS[i], strerror(errno));
        }
    }
    printf("可打开的敏感路径：%d 个\n", opened);
    return 0;
}
