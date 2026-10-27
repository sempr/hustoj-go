/*
 * 探针 2：PID 命名空间。
 *
 * 期望输出（安全状态）：
 *   可见进程数：2~3      （只有沙箱里的 tini 和自己）
 *   /proc/1/cmdline: tini ...   （沙箱的 init，不是宿主机的 init）
 *   NSpid: <x> <x>       （只有一层，说明确实在新的 PID 命名空间里）
 *
 * 如果"可见进程数"很大、或 /proc/1/cmdline 是宿主机的进程，说明
 * PID 命名空间没生效，宿主进程能被枚举。
 */
#include <dirent.h>
#include <stdio.h>
#include <string.h>

static void print_cmdline(const char *path) {
    FILE *f = fopen(path, "r");
    if (f == NULL) { printf("%s: 打不开\n", path); return; }
    char buf[512];
    size_t n = fread(buf, 1, sizeof buf - 1, f);
    fclose(f);
    buf[n] = '\0';
    for (size_t i = 0; i < n; i++) if (buf[i] == '\0') buf[i] = ' ';
    printf("%s: %s\n", path, buf);
}

int main(void) {
    printf("== PID 命名空间 ==\n");

    DIR *d = opendir("/proc");
    if (d == NULL) { printf("打不开 /proc\n"); return 1; }
    int count = 0;
    struct dirent *e;
    while ((e = readdir(d)) != NULL) {
        if (e->d_name[0] >= '0' && e->d_name[0] <= '9') count++;
    }
    closedir(d);
    printf("可见进程数：%d\n", count);

    print_cmdline("/proc/1/cmdline");
    print_cmdline("/proc/self/cmdline");

    FILE *f = fopen("/proc/self/status", "r");
    if (f != NULL) {
        char line[512];
        while (fgets(line, sizeof line, f)) {
            if (strncmp(line, "NSpid:", 7) == 0) {
                line[strcspn(line, "\n")] = '\0';
                printf("%s\n", line);
                break;
            }
        }
        fclose(f);
    }
    return 0;
}
