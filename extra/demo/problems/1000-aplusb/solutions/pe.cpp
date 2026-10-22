// 示例题 1000 反例：格式错误（PE）。
//
// 答案本身是对的，但行首多了一个空格：逐行比较（忽略行尾空白）判不等，
// 忽略全部空白比较判相等 —— 落在"格式错误"这一档。
#include <iostream>
int main() {
    long long a, b;
    if (!(std::cin >> a >> b)) return 0;
    std::cout << ' ' << a + b << '\n';
    return 0;
}
