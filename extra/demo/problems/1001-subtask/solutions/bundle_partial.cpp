// 示例题 1001 反例：同一子任务内只过一个测试点（演示捆绑测试）。
//
// 只在 n == 3 时算对：子任务 1 的 1_1[15].in（n=3）过、1_2[15].in（n=5）不过。
// 按"同一子任务必须全过"的规则，子任务 1 得 0 分（不是 15 分），
// 子任务 2 同样过不了，最终 0 分 —— 与只过子任务 1 的 partial.cpp（30 分）
// 形成对照。
#include <iostream>
int main() {
    int n;
    if (!(std::cin >> n)) return 0;
    long long s = 0;
    for (int i = 0; i < n; ++i) { long long x; std::cin >> x; s += x; }
    if (n != 3) s += 1;  // 只在 n=3 时算对：捆绑测试里"过一半"没有分
    std::cout << s << '\n';
    return 0;
}
