// 示例题 1000 反例：内存与输出都会超，但输出先撞上限（OL）。
//
// 每轮写 1MB 输出、再吃 1MB 内存：输出涨得比内存快，第 64 轮写满 64MB
// 触发输出上限（此时内存才 64MB，没超内存上限），判为输出超限。
#include <cstdio>
#include <cstdlib>
#include <cstring>
int main() {
    static char out[1 << 20];
    memset(out, 'x', sizeof out);
    for (int i = 0; i < 200; i++) {
        fwrite(out, 1, sizeof out, stdout);
        fflush(stdout);
        char *p = static_cast<char *>(malloc(1UL * 1024 * 1024));
        if (p == nullptr) return 1;
        memset(p, 1, 1UL * 1024 * 1024);
    }
    return 0;
}
