/*
 * 探针 1：身份与能力。
 *
 * 期望输出（安全状态）：
 *   Uid: 65534 65534 65534 65534   （已降到 nobody）
 *   Gid: 65534 ...
 *   CapEff: 0000000000000000        （没有残留能力）
 *   NoNewPrivs: 1                  （不可提权）
 *   Seccomp: 0                     （没装 seccomp 过滤 —— 这是可以加固的点）
 */
#include <stdio.h>
#include <string.h>

static void show(const char *key) {
    FILE *f = fopen("/proc/self/status", "r");
    if (f == NULL) { printf("%s: 打不开 /proc/self/status\n", key); return; }
    char line[512];
    size_t klen = strlen(key);
    while (fgets(line, sizeof line, f)) {
        if (strncmp(line, key, klen) == 0) {
            line[strcspn(line, "\n")] = '\0';
            printf("%s\n", line);
            fclose(f);
            return;
        }
    }
    fclose(f);
    printf("%s: (没有这一项)\n", key);
}

int main(void) {
    printf("== 身份与能力 ==\n");
    show("Uid:");
    show("Gid:");
    show("CapInh:");
    show("CapPrm:");
    show("CapEff:");
    show("CapBnd:");
    show("NoNewPrivs:");
    show("Seccomp:");
    show("Seccomp_filters:");
    return 0;
}
