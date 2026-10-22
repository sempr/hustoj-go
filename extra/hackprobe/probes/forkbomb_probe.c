/*
 * 探针 5：进程数上限（fork 炸弹 / 打表机拖垮判题机的经典手法）。
 *
 * 一直 fork 到失败为止，打印一共 fork 出多少个进程。
 * 期望：被 cgroup 的 pids.max 卡住（几十个），不会拖垮判题机。
 * 若数字很大（上千），说明 pids 限制没生效，判题机可能被拖垮。
 */
#include <errno.h>
#include <stdio.h>
#include <string.h>
#include <sys/wait.h>
#include <unistd.h>

int main(void) {
    printf("== 进程数上限 ==\n");
    fflush(stdout);
    int spawned = 0;
    for (int i = 0; i < 5000; i++) {
        pid_t pid = fork();
        if (pid == 0) {
            /* 子进程什么也不做，等父进程收尸；父进程会先被杀掉，
               于是子进程被 init(tini) 收养并立刻退出。 */
            _exit(0);
        }
        if (pid < 0) {
            printf("fork 失败（第 %d 次）：%s\n", spawned + 1, strerror(errno));
            break;
        }
        spawned++;
        /* 每 8 个打一次并立即 flush：中途被杀掉也能看出卡在哪。 */
        if (spawned % 8 == 0) {
            printf("已 fork %d 个\n", spawned);
            fflush(stdout);
        }
    }
    printf("共 fork 出 %d 个进程\n", spawned);
    fflush(stdout);

    /* 回收：尽量把僵尸收掉，避免自己先被打满 */
    for (int i = 0; i < spawned; i++) {
        int status;
        waitpid(-1, &status, WNOHANG);
    }
    return 0;
}
