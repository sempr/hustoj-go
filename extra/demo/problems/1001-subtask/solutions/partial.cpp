// 示例题 1001 反例：只在小数据子任务正确（演示部分分）。
// 结果被截断为 32 位，子任务 2 的和超出范围即 WA，应得 30/100。
#include <iostream>
int main() {
    int n;
    if (!(std::cin >> n)) return 0;
    long long s = 0;
    for (int i = 0; i < n; ++i) { long long x; std::cin >> x; s += x; }
    std::cout << static_cast<int>(s) << '\n';
    return 0;
}
