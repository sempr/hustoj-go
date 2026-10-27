// 示例题 1002 参考解（C++）：奇数阶幻方的 Siamese 构造法。
#include <iostream>
#include <vector>
int main() {
    int n;
    if (!(std::cin >> n)) return 0;
    std::vector<std::vector<int>> a(n, std::vector<int>(n, 0));
    int i = 0, j = n / 2;
    for (int v = 1; v <= n * n; ++v) {
        a[i][j] = v;
        int ni = (i - 1 + n) % n, nj = (j + 1) % n;
        if (a[ni][nj]) { ni = (i + 1) % n; nj = j; }
        i = ni; j = nj;
    }
    for (int r = 0; r < n; ++r) {
        for (int c = 0; c < n; ++c) { if (c) std::cout << ' '; std::cout << a[r][c]; }
        std::cout << '\n';
    }
    return 0;
}
