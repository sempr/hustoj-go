// 示例题 1001 满分参考解（C++）：64 位累加。
#include <iostream>
int main() {
    int n;
    if (!(std::cin >> n)) return 0;
    long long s = 0;
    for (int i = 0; i < n; ++i) { long long x; std::cin >> x; s += x; }
    std::cout << s << '\n';
    return 0;
}
