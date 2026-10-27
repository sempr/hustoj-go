/*
 * 内存峰值探针：把进程自己的 VmHWM / VmRSS 打出来。
 *
 * 用途：核对判题器报出的"内存用量"到底是多少。
 *
 *   * 作为普通提交跑一遍，runtimeinfo（showsource.php 的"各用例结果"）里
 *     看不到 stdout；用测试运行（custominput）跑，stdout 会写进 runtimeinfo；
 *   * 也可以在沙箱里手工跑：
 *       hustoj run --rootfs <rootfs> --cwd /code --cmd "/code/Main" ...
 *
 * 判题器报的内存来自 cgroup 的匿名内存峰值（所有进程堆栈之和），
 * 正常应与本程序打印的 VmHWM 同量级；差很多就说明口径出了问题。
 */
#include <stdio.h>
#include <string.h>

int main(void) {
    FILE *f = fopen("/proc/self/status", "r");
    if (f == NULL) {
        perror("/proc/self/status");
        return 1;
    }
    char line[256];
    while (fgets(line, sizeof line, f)) {
        if (strncmp(line, "Vm", 2) == 0) fputs(line, stdout);
    }
    fclose(f);
    return 0;
}
