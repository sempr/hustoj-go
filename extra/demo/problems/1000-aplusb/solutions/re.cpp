// 示例题 1000 反例：运行错误（RE）。
//
// 主动 abort：进程被信号杀死，退出码非 0，判为"运行错误"。
#include <cstdlib>
int main() {
    abort();
    return 0;
}
