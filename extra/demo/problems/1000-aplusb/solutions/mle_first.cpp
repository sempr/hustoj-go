// 示例题 1000 反例：内存与输出都会超，但内存先撞上限（ML）。
//
// 每轮写 8MB 输出、再吃 20MB 内存：内存涨得比输出快，写到第 7 轮时
// 内存已经越过上限（此时输出才 ~56MB，还没到输出上限），判为内存超限。
#include <cstdio>
#include <cstdlib>
#include <cstring>
int main() {
    static char out[8 << 20];
    memset(out, 'x', sizeof out);
    for (int i = 0; i < 20; i++) {
        fwrite(out, 1, sizeof out, stdout);
        fflush(stdout);
        char *p = static_cast<char *>(malloc(20UL * 1024 * 1024));
        if (p == nullptr) return 1;
        memset(p, 1, 20UL * 1024 * 1024);
    }
    return 0;
}
