// 示例题 1000 反例：内存超限（ML）。
//
// 连续申请并写满 8 块 50MB，超过题目的内存上限（128MB），
// 被 cgroup 的 OOM killer 干掉。
#include <cstdlib>
#include <cstring>
int main() {
    for (int i = 0; i < 8; i++) {
        char *p = static_cast<char *>(malloc(50UL * 1024 * 1024));
        if (p == nullptr) return 1;
        memset(p, 1, 50UL * 1024 * 1024);
    }
    return 0;
}
