// 示例题 1000 反例：输出超限（OL）。
//
// 往 stdout 写 150MB，超过沙箱的输出上限（64MB，由 RLIMIT_FSIZE 约束），
// 进程收到 SIGXFSZ 被判为"输出超限"。
#include <cstdio>
#include <cstring>
int main() {
    static char buf[1 << 20];
    memset(buf, 'x', sizeof buf);
    buf[sizeof buf - 1] = '\n';
    for (int i = 0; i < 150; i++) fwrite(buf, 1, sizeof buf, stdout);
    return 0;
}
