/*
 * 探针 3：宿主机文件系统是否可达（最常见的逃逸面）。
 *
 * 依次尝试：
 *   1) /proc/1/root/...   写宿主根目录（沙箱里 /proc/1 是沙箱自己的 init，
 *                          应当只能写到沙箱的 rootfs 里）
 *   2) /proc/self/root/...
 *   3) 直接写 /tmp/hustoj_escape_probe（沙箱内的 /tmp）
 *   4) statfs("/") 看根目录是不是 overlayfs（是就说明 pivot_root 生效）
 *
 * 判读：若 statfs("/") 显示的不是 overlayfs，或 1)/2) 落到了宿主机上
 * （在宿主机上能看到那个标记文件），说明隔离有问题。
 */
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <sys/stat.h>
#include <sys/vfs.h>
#include <unistd.h>

static const char *MARK = "hustoj_escape_probe";

static void try_write(const char *path) {
    int fd = open(path, O_WRONLY | O_CREAT | O_TRUNC, 0644);
    if (fd < 0) {
        printf("写 %-40s 失败: %s\n", path, strerror(errno));
        return;
    }
    ssize_t n = write(fd, "escaped\n", 8);
    close(fd);
    printf("写 %-40s 成功（写了 %zd 字节）\n", path, n);
}

int main(void) {
    printf("== 宿主机文件系统可达性 ==\n");

    char path[256];
    struct statfs fs;

    if (statfs("/", &fs) == 0) {
        printf("statfs(\"/\") f_type = 0x%lx", (unsigned long)fs.f_type);
        if (fs.f_type == 0x794c7630UL) printf("  (overlayfs —— pivot_root 生效)");
        else if (fs.f_type == 0xEF53UL) printf("  (ext4 —— 可能是宿主机根目录！)");
        printf("\n");
    } else {
        printf("statfs(\"/\") 失败: %s\n", strerror(errno));
    }

    snprintf(path, sizeof path, "/proc/1/root/tmp/%s", MARK);
    try_write(path);
    snprintf(path, sizeof path, "/proc/self/root/tmp/%s", MARK);
    try_write(path);
    snprintf(path, sizeof path, "/tmp/%s", MARK);
    try_write(path);
    snprintf(path, sizeof path, "/var/tmp/%s", MARK);
    try_write(path);

    printf("（如果宿主机上出现了 %s 这个文件，说明隔离被突破）\n", MARK);
    return 0;
}
