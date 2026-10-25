// 示例题 1001 反例（C）：只在小数据子任务正确（演示部分分）。
// 结果被截断为 32 位，子任务 2 的和超出范围即 WA，应得 30/100。
#include <stdio.h>

int main(void) {
    int n;
    if (scanf("%d", &n) != 1) return 0;
    long long sum = 0;
    for (int i = 0; i < n; ++i) {
        long long x;
        if (scanf("%lld", &x) != 1) return 0;
        sum += x;
    }
    printf("%d\n", (int)sum);
    return 0;
}
