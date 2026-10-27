/*
 * 探针 8（有概率）：cgroup 生效前的进程数竞态。
 *
 * 与探针 7 同一个窗口（判题器先启动进程、再把它挪进 cgroup），在窗口里
 * 尽可能多地 fork，报告实际拉起了多少个进程。
 *
 * 判读：
 *   * 进程数远超 pids.max（64）→ 窗口存在，pids 限制可以被绕过一瞬间，
 *     足以在判题机上打表；
 *   * 进程数卡在 64 附近 → cgroup 及时收紧了。
 */
#include <errno.h>
#include <stdio.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

int main(void) {
    int spawned = 0;
    for (int i = 0; i < 2000; i++) {
        pid_t pid = fork();
        if (pid == 0) _exit(0);
        if (pid < 0) break;
        spawned++;
        /* 每 16 个打一次并立即 flush：即使随后被 pids 限制或超时打断，
           runtimeinfo 里也能看到抢到了多少。 */
        if (spawned % 16 == 0) {
            printf("已拉起 %d 个进程\n", spawned);
            fflush(stdout);
        }
    }
    printf("== 进程数竞态 ==\n");
    printf("实际拉起 %d 个进程（pids.max 是 64）\n", spawned);
    if (spawned > 128) {
        printf("=> 抢到了 cgroup 生效前的窗口：进程数限制被绕过\n");
    } else {
        printf("=> 没抢到（cgroup 及时收紧了）\n");
    }
    fflush(stdout);
    for (int i = 0; i < spawned; i++) {
        int status;
        waitpid(-1, &status, WNOHANG);
    }
    return 0;
}
