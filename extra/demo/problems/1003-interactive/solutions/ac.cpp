// ac.cpp — 交互题「猜数字」参考解：每轮二分查找，20 次内必然猜中。
#include <iostream>

int main() {
    std::ios::sync_with_stdio(false);
    std::cin.tie(nullptr);

    int t = 0;
    if (!(std::cin >> t)) return 0;

    while (t--) {
        int lo = 1, hi = 1000000;
        while (lo <= hi) {
            int mid = lo + (hi - lo) / 2;
            std::cout << mid << std::endl;  // endl 刷新缓冲区
            int r = 0;
            if (!(std::cin >> r)) return 0;
            if (r == 0) break;
            if (r == -1) lo = mid + 1;  // 猜小了 → 往大找
            else hi = mid - 1;          // 猜大了 → 往小找
        }
    }
    return 0;
}
