// 参考解：奇偶子集的双射——每次 toggling n（在集合内就删掉，不在就加上）。
// 第一轮把 X 映成 Y，第二轮把 Y 再 toggling 一次就能还原 X；两轮互为逆。
#include <algorithm>
#include <iostream>
#include <vector>

int main() {
    int n, k;
    if (!(std::cin >> n >> k)) return 0;

    std::vector<int> a(k);
    for (int i = 0; i < k; i++) std::cin >> a[i];

    bool have = false;
    for (int x : a) if (x == n) have = true;

    std::vector<int> out;
    out.reserve(k + 1);
    for (int x : a) if (x != n) out.push_back(x);
    if (!have) out.push_back(n);

    // out 已经升序：除 n 外原是升序，追加的 n 比其它都大
    std::cout << n << " " << out.size() << "\n";
    for (size_t i = 0; i < out.size(); i++) {
        if (i) std::cout << " ";
        std::cout << out[i];
    }
    if (!out.empty()) std::cout << "\n";
    return 0;
}
