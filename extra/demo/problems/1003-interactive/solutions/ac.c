/* ac.c — 交互题「猜数字」参考解（C）：每轮二分查找，20 次内必然猜中。 */
#include <stdio.h>

int main(void) {
    int t;
    if (scanf("%d", &t) != 1) return 0;
    while (t--) {
        int lo = 1, hi = 1000000;
        while (lo <= hi) {
            int mid = lo + (hi - lo) / 2;
            printf("%d\n", mid);
            fflush(stdout);  /* 必须刷新，否则交互器读不到猜测 */
            int r;
            if (scanf("%d", &r) != 1) return 0;
            if (r == 0) break;
            if (r == -1) lo = mid + 1;  /* 猜小了 → 往大找 */
            else hi = mid - 1;          /* 猜大了 → 往小找 */
        }
    }
    return 0;
}
