// 示例题 1000 反例：答案错误（WA）。
//
// 算出的和比正确答案大 1。
#include <iostream>
int main() {
    long long a, b;
    if (!(std::cin >> a >> b)) return 0;
    std::cout << a + b + 1 << '\n';
    return 0;
}
