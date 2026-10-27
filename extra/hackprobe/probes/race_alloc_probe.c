/*
 * 探针 7（有概率）：cgroup 生效前的内存竞态。
 *
 * 判题器是"先启动进程、再建 cgroup、最后把 pid 挪进去"的（internal/run/run.go
 * 的 cmd.Start() → NewCgroup() 顺序）。在这个窗口里，进程的内存不受
 * memory.max 约束 —— 也就是有机会在受限之前先吃掉一大块内存。
 *
 * 这个探针从第一条指令就开始猛分配内存，尽量抢这个窗口，最后报告实际
 * 申请到的总量。
 *
 * 判读：
 *   * 分配量明显超过题目内存上限、而进程没被立刻杀掉 → 竞态窗口存在，
 *     内存限制可以被绕过（虽然最终判定仍会按峰值给 ML，但"当场杀掉"这条
 *     兜底没生效）；
 *   * 分配量卡在上限附近 → 窗口存在但没抢到，cgroup 及时收紧了。
 */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define CHUNK (16UL * 1024 * 1024)

int main(void) {
    unsigned long long got = 0;
    /* 尽量少做别的事：只在每 64MB 打一次并立即 flush，
       这样即使中途被 OOM killer 干掉，runtimeinfo 里也能看到抢到了多少。 */
    for (int i = 0; i < 4096; i++) {
        char *p = (char *)malloc(CHUNK);
        if (p == NULL) break;
        memset(p, 1, CHUNK);
        got += CHUNK;
        if (got % (64ULL * 1024 * 1024) == 0) {
            printf("已分配 %llu MB\n", got / 1024 / 1024);
            fflush(stdout);
        }
    }
    printf("== 内存竞态 ==\n");
    printf("实际申请到：%llu MB（题目内存上限一般是 128MB）\n", got / 1024 / 1024);
    if (got > 128ULL * 1024 * 1024) {
        printf("=> 抢到了 cgroup 生效前的窗口：内存限制被绕过\n");
    } else {
        printf("=> 没抢到（cgroup 及时收紧了）\n");
    }
    fflush(stdout);
    return 0;
}
