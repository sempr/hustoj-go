// 示例交互器：猜数字（testlib，适配本 OJ 的交互评测契约）。
//
// 本 OJ 的交互评测（internal/client/interactive.go）把交互器跑成：
//     /code/spj /code/data.in
// 交互器的 stdin 接选手 stdout、stdout 接选手 stdin（各经一个
// FIFO）。这与 testlib 的交互器模型一致（ouf 读 stdin、stdout
// 写给选手），唯一差别是 registerInteraction 要求 argc>=3 且会
// 立即以写方式打开 argv[2]（tout，诊断输出）。这里补一个 dummy
// 路径即可直接用标准 testlib 交互器；沙箱内 /code 可写，tout
// 写到那里不影响判题。
//
// 数据目录布局（交互题，problem.spj = 3）：
//     <OJHome>/data/<pid>/
//     ├── 0.in      交互用例（两行：secret 1..1000，limit 1..100）
//     ├── spj       本程序编译出的二进制（预编译，判题不编译）
//     └── spj.cc    源码存档
// （testlib.h 放数据根目录共享，编译时 -I 指向它）
//
// 编译：g++ -O2 -static -I<数据根目录> spj.cc -o spj
//
// 判题流程（每个 .in 一个交互会话）：
// 玩家问 "? x"，交互器答 ">"（再大）或 "<"（再小）；玩家用
// "! x" 作答，猜中且轮次不超限则 AC，否则 WA / 超限 WA。
#include "testlib.h"

#include <string>

int main(int argc, char* argv[]) {
    if (argc == 2) {
        static char* av[3] = {argv[0], argv[1], (char*)"/code/tout"};
        registerInteraction(3, av);
    } else {
        registerInteraction(argc, argv);
    }

    int secret = inf.readInt(1, 1000, "secret");
    int limit = inf.readInt(1, 100, "limit");

    for (int turn = 1; turn <= limit; turn++) {
        std::string op = ouf.readToken("[?!]", "expected '?' or '!'");
        int x = ouf.readInt(1, 1000, "guess");
        if (op == "?") {
            if (x == secret)
                quitf(_wa, "exact guess must be answered with '!', not '?'");
            printf("%s\n", x < secret ? ">" : "<");
            fflush(stdout);  // FIFO 通信必须 flush，否则选手阻塞
        } else {
            if (x == secret)
                quitf(_ok, "guessed %d in %d turn(s)", secret, turn);
            quitf(_wa, "wrong answer %d (secret is %d)", x, secret);
        }
    }
    quitf(_wa, "no answer within %d turns", limit);
}
