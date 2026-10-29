// 反例：第一轮原样输出，长度奇偶性不变， violates parity 约束 → 第 1 轮 WA。
#include <iostream>
int main() {
    int n, k;
    std::cin >> n >> k;
    std::cout << n << " " << k << "\n";
    for (int i = 0; i < k; i++) { int x; std::cin >> x; std::cout << x << " "; }
    std::cout << "\n";
    return 0;
}