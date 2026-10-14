# 判题程序 / 交互器示例

四个例子对应四种题目类型，都是**预编译二进制**放入题目数据目录
（`<OJHome>/data/<pid>/`），判题时不编译：

| 文件 | 题目类型 | 调用参数（沙箱内 /code 下） | 判分 |
| --- | --- | --- | --- |
| `spj.cpp` | 普通 SPJ（problem.spj=1） | `spj data.in sysdata.out data.usr` | 退出码 0=AC，非 0=WA |
| `tpj.cpp` | testlib SPJ（problem.spj=1） | `tpj data.in data.usr sysdata.out` | 同上；testlib 的 inf/ouf/ans 顺序 |
| `upj.cpp` | 带分值 SPJ（problem.spj=1） | `upj data.in sysdata.out data.usr` | 退出码即 0–100 分值；100=AC 其余 WA |
| `interactor.cpp` | 交互题（problem.spj=16） | `spj data.in`（stdin=选手 stdout，stdout=选手 stdin，各经 FIFO） | 退出码 0=AC，非 0=WA |

数据目录里同时存在 spj/tpj/upj 时，判题按 **upj → tpj → spj**
优先级取一个（`internal/client/run.go` `detectSpjType`）。

## 编译

```sh
# spj / upj（纯 C，无依赖）
gcc -O2 -static spj.cpp -o spj

# tpj / interactor（testlib；testlib.h 放数据根目录共享）
g++ -O2 -static -I<数据根目录> tpj.cpp -o tpj
g++ -O2 -static -I<数据根目录> interactor.cpp -o spj   # 交互器必须叫 spj
```

## 交互题补充（interactor.cpp）

- 每个 `.in` 是一个独立交互会话；目录里没有 `.in` 时系统注入
  合成用例（单会话）。
- testlib 的 `registerInteraction` 要求 argc>=3 且会立即以写方式
  打开 argv[2]（tout），本 OJ 只传一个参数——示例里补了 dummy
  路径 `/code/tout`（沙箱内可写，不影响判题）。
- 交互器与选手经两个 FIFO 双向通信：选手 stdout → 交互器 stdin
  （testlib 的 `ouf`），交互器 stdout → 选手 stdin（`printf`/
  `cout` 输出后记得 `fflush`，否则选手阻塞）。
- 任一侧超限或崩溃优先作为最终结果；交互器退出码 0=AC，非 0=WA。

## 安装成一道题

1. 数据目录放好 `0.in`、编译好的 `spj`（交互题）、`spj.cc`（存档）；
2. 数据库 `problem` 表对应行的 `spj` 字段置 `'16'`（交互题）或
   `'1'`（SPJ 题）；
3. 提交一次答案验证。
