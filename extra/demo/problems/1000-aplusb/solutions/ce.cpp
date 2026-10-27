// 示例题 1000 反例：编译错误（CE）。
//
// 故意少一个右花括号 —— 这个文件本来就编译不过，别拿去跑自检脚本。
#include <iostream>
int main() {
    long long a, b;
    if (!(std::cin >> a >> b)) return 0;
    std::cout << a + b << '\n';
    return 0;
