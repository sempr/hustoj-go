// Run-twice 通讯题校验器：第一次运行收编码串，第二次运行核对 n 的位置。
// argv[1]=本轮输入，argv[2]=答案文件（第二轮的期望位置），argv[3]=工作目录。
// 选手本轮输出从 stdin 读入。
#include <cctype>
#include <fstream>
#include <iostream>
#include <sstream>
#include <string>

static std::string readAll(std::istream& in) {
    std::ostringstream ss;
    ss << in.rdbuf();
    return ss.str();
}

static std::string trim(std::string s) {
    while (!s.empty() && std::isspace(static_cast<unsigned char>(s.back()))) s.pop_back();
    while (!s.empty() && std::isspace(static_cast<unsigned char>(s.front()))) s.erase(s.begin());
    return s;
}

int main(int argc, char** argv) {
    if (argc < 4) {
        std::cerr << "usage: validator <input> <answer> <workdir>\n";
        return 2;
    }

    std::ifstream fin(argv[1]);
    std::string mode;
    if (!(fin >> mode)) {
        std::cerr << "empty input\n";
        return 2;
    }

    std::string out = trim(readAll(std::cin));

    if (mode == "first") {
        long long n = 0;
        if (!(fin >> n) || n < 1 || n > 10000) {
            std::cerr << "invalid n\n";
            return 2;
        }
        if (out.empty() || out.size() > 100000) {
            std::cerr << "encoded string length out of range\n";
            return 1;
        }
        for (unsigned char c : out) {
            if (c < 'a' || c > 'z') {
                std::cerr << "encoded string must contain lowercase letters only\n";
                return 1;
            }
        }
        std::ofstream nf(std::string(argv[3]) + "/nextpass.in");
        nf << "second\n" << out << "\n";
        if (!nf) {
            std::cerr << "failed to write nextpass.in\n";
            return 2;
        }
        return 42;
    }

    if (mode == "second") {
        long long expected = 0;
        std::ifstream fans(argv[2]);
        if (!(fans >> expected)) {
            std::cerr << "missing answer\n";
            return 2;
        }
        std::istringstream ss(out);
        long long got = 0;
        std::string extra;
        if (!(ss >> got) || (ss >> extra)) {
            std::cerr << "invalid output: expected one integer\n";
            return 1;
        }
        if (got != expected) {
            std::cerr << "wrong position: got " << got << ", expected " << expected << "\n";
            return 1;
        }
        return 42;
    }

    std::cerr << "unknown mode\n";
    return 2;
}
