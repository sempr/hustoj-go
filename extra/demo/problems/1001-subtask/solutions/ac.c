/* 示例题 1001 满分参考解（C）。 */
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
    printf("%lld\n", sum);
    return 0;
}
