/* 示例题 1002 参考解（C）：奇数阶幻方的 Siamese 构造法。 */
#include <stdio.h>
#include <string.h>

int main(void) {
    int n;
    if (scanf("%d", &n) != 1 || n <= 0) return 0;
    static int a[512][512];
    memset(a, 0, sizeof(a));
    int i = 0, j = n / 2;
    for (int v = 1; v <= n * n; ++v) {
        a[i][j] = v;
        int ni = (i - 1 + n) % n, nj = (j + 1) % n;
        if (a[ni][nj]) {
            ni = (i + 1) % n;
            nj = j;
        }
        i = ni;
        j = nj;
    }
    for (int r = 0; r < n; ++r) {
        for (int c = 0; c < n; ++c) {
            if (c) putchar(' ');
            printf("%d", a[r][c]);
        }
        putchar('\n');
    }
    return 0;
}
