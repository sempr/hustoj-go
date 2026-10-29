#include <iostream>
#include <string>

int main() {
    std::string mode;
    std::cin >> mode;
    if (mode == "first") {
        int n;
        std::cin >> n;
        // 第一次输出合法的小写编码，先通过第一轮。
        std::cout << "a" << std::endl;
    } else {
        // 第二次故意给出非法位置，导致第二轮 WA。
        std::cout << 0 << std::endl;
    }
    return 0;
}
