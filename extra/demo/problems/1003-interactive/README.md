# 示例题 1003 · 交互题「猜数字」

交互题（`problem.spj = 3`）的完整资源：题面、5 组测试数据、交互器源码与多语言
参考解。由 `extra/demo/install_demo.sh` 连同其它示例题一起装载。

## 目录结构

```
1003-interactive/
├── meta.env              # 题号/标题/时限/SPJ 模式等，供 install_demo.sh 读取
├── README.md             # 本文件
├── Makefile              # 本地编译交互器与参考解
├── statement/            # 题面分段（注入 problem 表对应字段）
│   ├── description.md
│   ├── input.md
│   ├── output.md
│   └── hint.md
├── data/
│   ├── BUILD             # 打包时用 build_data.sh 据它编译交互器
│   ├── spj.cc            # 交互器源码
│   ├── 1.in … 5.in       # 5 组测试数据（每组 = 一次交互会话）
│   └── spj               # 编译产物（打包/本地 make 生成，未入库）
└── solutions/            # 多语言参考解（ac.c/cpp/py/…；ac 为编译产物，未入库）
```

## 交互契约

与判题器 `internal/client/interactive.go` 一致（另见 `extra/spj/README.md`）：

- 数据目录中**必须**有名为 `spj` 的可执行文件；判题时它被拷入沙箱，
  以 `/code/spj /code/<用例>.in` 的形式运行。
- 选手 stdout → 交互器 stdin；交互器 stdout → 选手 stdin（各经一条 FIFO）。
- 交互器退出码 `0` = AC，非 `0` = WA；任一侧超时/崩溃时以该侧状态为准。
- **每个 `.in` 文件 = 一次独立交互会话**。本题提供 5 个 `.in`，共 5 次会话，
  全部 AC 时该提交才判 AC（OI 模式，任一非 AC 则整体非 AC）。

## 测试数据格式

每个 `.in` 文件：

```
t
x_1
x_2
…
x_t
```

其中 `t` 为轮数，`x_i ∈ [1, 1000000]` 为第 `i` 轮的隐藏数字。交互器会把 `t` 作为
第一行输出给选手。

| 文件 | 轮数 t | 隐藏数字 | 用途 |
| --- | --- | --- | --- |
| `1.in` | 1 | 1 | 最小值边界 |
| `2.in` | 1 | 1000000 | 最大值边界 |
| `3.in` | 3 | 500000 / 999999 / 2 | 常规 + 边界混合 |
| `4.in` | 5 | 123456 / 654321 / 1 / 1000000 / 777777 | 多轮含双边界 |
| `5.in` | 10 | 314159 / 271828 / 888888 / 123 / 999999 / 500000 / 1 / 1000000 / 424242 / 606060 | 最大轮数压力 |

## 交互协议

1. 交互器先输出一行 `t`。
2. 每轮选手反复输出整数猜测 `g`（`1 ≤ g ≤ 10^6`），交互器回答
   `-1`（偏小）/ `0`（猜中）/ `1`（偏大）。
3. 收到 `0` 进入下一轮；单轮超过 20 次未中 → WA。
4. `t` 轮全部通过 → 交互器退出码 0（AC）。

## 编译

```bash
make            # 生成 data/spj 与 solutions/ac
# 或手动：
g++ -O2 -static data/spj.cc      -o data/spj
g++ -O2 -static solutions/ac.cpp -o solutions/ac
```

打包时会由 `extra/demo/build_data.sh` 按 `data/BUILD` 交叉编译好 `data/spj`
放进部署包，目标机无需编译器。

## 本地验证（无需数据库）

```bash
go build -o hustoj .
./hustoj judge --source extra/demo/problems/1003-interactive/solutions/ac.cpp --lang 1 \
  --data extra/demo/problems/1003-interactive/data --time 2000 --memory 262144 \
  --spj 3 --ojhome extra --workbase /tmp
```

期望输出 JSON 中 `final_result = 4`（AC）、`pass_rate = 1.0`。
端到端脚本见 `tests/interactive/test.sh`。
