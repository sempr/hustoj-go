// 示例题 1002 反例（C++）：输出 1..n² 的按行排列，不是幻方。
// 行和/列和/对角和都不相等，SPJ 应判 WA(6)。
#include <iostream>
int main() {
    int n;
    if (!(std::cin >> n)) return 0;
    int v = 1;
    for (int r = 0; r < n; ++r) {
        for (int c = 0; c < n; ++c) {
            if (c) std::cout << ' ';
            std::cout << v++;
        }
        std::cout << '\n';
    }
    return 0;
}
