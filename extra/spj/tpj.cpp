// clang-format off

#include "testlib.h"
#include <cmath>

int main(int argc, char *argv[]) {
  /*
   * inf：输入
   * ouf：选手输出
   * ans：标准输出
   */
  registerTestlibCmd(argc, argv);

  int t=inf.readInt();
  while (t--){
    int x=ouf.readInt(),y=ouf.readInt();
    int z=inf.readInt();
    if (x>0&&y>0&&x+y==z) continue;
    quitf(_wa, "wrong");
    break;
  }
  quitf(_ok, "yes");
}

