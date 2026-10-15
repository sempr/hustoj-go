// spj.cc — 交互题「猜数字」交互器（problem.spj = 3）
//
// 契约（与 internal/client/interactive.go 一致）：
//   调用：/code/spj /code/<用例>.in
//   选手 stdout → 本程序 stdin；本程序 stdout → 选手 stdin（各经一条 FIFO）。
//   退出码 0 = AC，非 0 = WA。
//   交付物必须编译成数据目录下名为 spj 的可执行文件。
//
// 用例文件 .in：首行 t（轮数），随后 t 行每行一个 secret ∈ [1, 1000000]。
//
// 协议：
//   1. 交互器先向选手输出一行 t。
//   2. 每轮选手反复输出整数猜测 x（1 ≤ x ≤ 1000000）；
//      交互器回答：-1 = 猜小了，0 = 刚好，1 = 猜大了。
//      收到 0 进入下一轮；单轮超过 20 次仍未中 → WA。
//   3. t 轮全部通过 → 退出码 0（AC）。
//
// 编译：g++ -O2 -static spj.cc -o spj
#include <cstdio>
#include <cstdlib>
#include <iostream>
#include <vector>

static const int MIN_V = 1;
static const int MAX_V = 1000000;
static const int MAX_GUESSES = 20;

[[noreturn]] static void wa(const char* reason) {
    std::cerr << "WA: " << reason << '\n';
    std::exit(1);
}

int main(int argc, char* argv[]) {
    if (argc < 2) {
        std::cerr << "SE: missing test data argument\n";
        return 2;
    }

    std::FILE* f = std::fopen(argv[1], "r");
    if (!f) {
        std::cerr << "SE: cannot open test data\n";
        return 2;
    }

    int t = -1;
    if (std::fscanf(f, "%d", &t) != 1 || t < 0) {
        std::fclose(f);
        std::cerr << "SE: bad test data header\n";
        return 2;
    }

    std::vector<int> secrets(static_cast<std::size_t>(t));
    for (int i = 0; i < t; ++i) {
        if (std::fscanf(f, "%d", &secrets[i]) != 1 ||
            secrets[i] < MIN_V || secrets[i] > MAX_V) {
            std::fclose(f);
            std::cerr << "SE: bad test data value\n";
            return 2;
        }
    }
    std::fclose(f);

    std::cout << t << '\n' << std::flush;

    for (int r = 0; r < t; ++r) {
        const int secret = secrets[r];
        bool solved = false;
        for (int g = 1; g <= MAX_GUESSES; ++g) {
            int x = 0;
            if (!(std::cin >> x)) wa("unexpected end of input");
            if (x < MIN_V || x > MAX_V) wa("guess out of range");
            if (x == secret) {
                std::cout << 0 << '\n' << std::flush;
                solved = true;
                break;
            }
            std::cout << (x < secret ? -1 : 1) << '\n' << std::flush;
        }
        if (!solved) wa("exceeded 20 guesses in a round");
    }

    return 0;
}
