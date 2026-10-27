// 示例题 1004 参考解（C++）：文件 IO（freopen）。
#include <cstdio>
int main() {
    freopen("sum.in", "r", stdin);
    freopen("sum.out", "w", stdout);
    long long a, b;
    if (scanf("%lld %lld", &a, &b) != 2) return 0;
    printf("%lld\n", a + b);
    return 0;
}
