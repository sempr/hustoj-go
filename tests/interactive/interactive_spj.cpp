// interactive_spj.cpp - 样例交互评测程序 (spj=3, interactor)
//
// 契约（与 hustoj-go judge --spj 3 的实现一致）:
//   argv[1]  可选，为沙箱内 /code/data.in 路径（从中读取 secret，缺省 731）。
//   stdin    玩家程序输出流：逐行读取玩家的“猜测”。
//   stdout   向玩家程序返回的响应（too small / too large / correct），
//            每行必须 flush（endl 自带）。
//   verdict  退出码：0 = AC；非 0 = WA 或 RE。
//   stderr   可选的详细原因（写入沙箱内的 spj.err）。
//
// 编译：g++ -static -O2 -o data/spj interactive_spj.cpp
#include <fstream>
#include <iostream>
#include <string>

int main(int argc, char* argv[]) {
    int secret = 731;
    const int maxGuesses = 10;

    if (argc > 1) {
        std::ifstream fin(argv[1]);
        if (fin) fin >> secret;
    }

    std::string line;
    int guesses = 0;
    while (std::getline(std::cin, line)) {
        if (line.empty()) continue;
        int x;
        try {
            x = std::stoi(line);
        } catch (...) {
            std::cout << "invalid" << std::endl;
            continue;
        }
        guesses++;
        if (x < secret) {
            std::cout << "too small" << std::endl;
        } else if (x > secret) {
            std::cout << "too large" << std::endl;
        } else {
            std::cout << "correct" << std::endl;
            return 0;  // AC
        }
        if (guesses >= maxGuesses) {
            std::cerr << "limit reached" << std::endl;
            return 1;  // WA
        }
    }

    std::cerr << "user program exited before guessing the secret" << std::endl;
    return 1;  // WA
}