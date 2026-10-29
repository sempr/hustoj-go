#include <iostream>
#include <string>

int main() {
    std::string mode;
    std::cin >> mode;
    if (mode == "first") {
        int n;
        std::cin >> n;
        // 第一次运行输出大写字母，不满足“只能包含小写字母”的编码要求。
        std::cout << "HELLO" << std::endl;
    } else {
        std::cout << 1 << std::endl;
    }
    return 0;
}
