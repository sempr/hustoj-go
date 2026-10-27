// wa_guess.cpp - 错误玩家：永远猜测 1，10 轮后 interactor 判定 WA。
#include <iostream>
#include <string>

int main() {
    for (;;) {
        std::cout << 1 << std::endl;
        std::string resp;
        if (!std::getline(std::cin, resp)) break;
    }
    return 0;
}