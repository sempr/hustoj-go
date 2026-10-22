// 示例题 1000 反例：时间超限（TL）。
//
// 死循环烧满 CPU 时间限额，由 cgroup 的 CPU 限制兜住。
int main() {
    volatile long long i = 0;
    for (;;) i++;
    return 0;
}
