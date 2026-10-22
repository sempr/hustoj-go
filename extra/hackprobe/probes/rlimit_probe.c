/*
 * 探针 9：当前生效的资源限制。
 *
 * 打印各项 rlimit，并尝试调高 RLIMIT_AS（setrlimit 只能降低硬上限，
 * 调不上去说明硬限制已经很低）。
 *
 * 判读：
 *   * RLIMIT_FSIZE 应等于沙箱的输出上限（64MB）—— 那是判题器主动加的；
 *   * RLIMIT_AS 若为 unlimited，说明没有对地址空间设限（地址空间大不是
 *     问题，只要 cgroup 的 memory.max 兜住就行，但值得记一笔）；
 *   * RLIMIT_NPROC 若是 0 或很小，说明进程数只靠 cgroup 的 pids.max 管。
 */
#include <errno.h>
#include <stdio.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/time.h>

static void show(const char *name, int resource) {
    struct rlimit rl;
    if (getrlimit(resource, &rl) != 0) {
        printf("%-14s 读取失败: %s\n", name, strerror(errno));
        return;
    }
    if (rl.rlim_cur == RLIM_INFINITY && rl.rlim_max == RLIM_INFINITY) {
        printf("%-14s unlimited\n", name);
        return;
    }
    printf("%-14s cur=%lld max=%lld\n", name,
           (long long)rl.rlim_cur, (long long)rl.rlim_max);
}

int main(void) {
    printf("== 资源限制 ==\n");
    show("RLIMIT_FSIZE", RLIMIT_FSIZE);
    show("RLIMIT_AS", RLIMIT_AS);
    show("RLIMIT_DATA", RLIMIT_DATA);
    show("RLIMIT_STACK", RLIMIT_STACK);
    show("RLIMIT_CORE", RLIMIT_CORE);
    show("RLIMIT_NPROC", RLIMIT_NPROC);
    show("RLIMIT_NOFILE", RLIMIT_NOFILE);
    show("RLIMIT_MEMLOCK", RLIMIT_MEMLOCK);

    struct rlimit rl = {RLIM_INFINITY, RLIM_INFINITY};
    printf("尝试调高 RLIMIT_AS: ");
    if (setrlimit(RLIMIT_AS, &rl) == 0) {
        printf("成功（原本就是无限或硬限也允许调高）\n");
    } else {
        printf("失败: %s\n", strerror(errno));
    }
    return 0;
}
