// 通讯题校验器：多轮猜数
// argv[1] 本轮输入文件（L R），argv[2] 答案文件（隐藏整数），argv[3] 工作目录。
// 选手本轮的输出从 stdin 读入；猜测错误时把更新后的区间写入 nextpass.in，
// 退出码 42 表示本轮通过且继续下一轮；猜中后不写 nextpass.in。
#include <fstream>
#include <iostream>
#include <sstream>
#include <string>

int main(int argc, char** argv) {
    if (argc < 4) {
        std::cerr << "usage: validator <input> <answer> <workdir>\n";
        return 2;
    }
    std::ifstream fin(argv[1]);
    long long lo = 0, hi = 0;
    if (!(fin >> lo >> hi) || lo > hi) {
        std::cerr << "invalid input range\n";
        return 2;
    }

    std::ifstream fans(argv[2]);
    long long secret = 0;
    if (!(fans >> secret)) {
        std::cerr << "missing secret in answer file\n";
        return 2;
    }

    std::string line, out;
    while (std::getline(std::cin, line)) {
        if (!line.empty()) out = line;
    }
    std::istringstream ss(out);
    long long guess = 0;
    char extra = 0;
    if (!(ss >> guess) || (ss >> extra)) {
        std::cerr << "invalid output: expected one integer\n";
        return 1;
    }
    if (guess < lo || guess > hi) {
        std::cerr << "guess " << guess << " out of range [" << lo << ", " << hi << "]\n";
        return 1;
    }
    if (guess == secret) {
        return 42;
    }

    if (guess < secret) {
        lo = guess + 1;
    } else {
        hi = guess - 1;
    }
    if (lo > hi) {
        std::cerr << "internal: empty range after guess\n";
        return 2;
    }

    std::ofstream nf(std::string(argv[3]) + "/nextpass.in");
    nf << lo << " " << hi << "\n";
    if (!nf) {
        std::cerr << "failed to write nextpass.in\n";
        return 2;
    }
    return 42;
}
