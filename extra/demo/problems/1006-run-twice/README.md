# 示例题 1006 · 通讯题「两次通信：编码与定位」

run-twice 通讯题（`problem.spj = 4`）的完整资源：题面、3 组测试数据、校验器源码，
以及 AC / 首轮 WA / 次轮 WA 三份参考解。形式参考 Codeforces 2168A2 与 2168B。

由 `extra/demo/install_demo.sh` 连同其它示例题一起装载。

## 目录结构

```
1006-run-twice/
├── meta.env              # 题号/标题/时限/SPJ 模式等，供 install_demo.sh 读取
├── README.md             # 本文件
├── statement/            # 题面分段（注入 problem 表对应字段）
│   ├── description.md
│   ├── input.md
│   ├── output.md
│   └── hint.md
├── sample/               # 题面样例（注入 sample_input / sample_output）
│   ├── sample.in
│   └── sample.out
├── data/
│   ├── BUILD             # 打包时用 build_data.sh 据它编译校验器
│   ├── validator.cpp     # 校验器源码
│   ├── 1.in … 3.in       # 3 组测试数据；每个 .in 是第一轮输入
│   ├── 1.ans … 3.ans     # 每组答案（整数 n 在排列中的位置）
│   └── validator         # 编译产物（打包时生成，未入库）
└── solutions/
    ├── ac.cpp            # 26 进制编码位置，两轮都通过
    ├── wa_first.cpp      # 首轮输出大写字母 → 第 1 轮 WA
    └── wa_second.cpp     # 首轮合法、次轮输出 0 → 第 2 轮 WA
```

## 通讯契约

与 `1005` 相同，见 `internal/client/communication.go`：

- 数据目录中必须有可执行的 `validator`（或 `output_validator`）；
- 每轮把当前输入交给选手，再把选手输出交给
  `/code/validator /code/data.in /code/jury.ans /code`；
- 校验器退出码 `0` / `42` = 本轮通过，其它 = WA，崩溃/超时 = RE；
- 本轮通过且存在 `nextpass.in` → 进入下一轮，否则本组通过；
- 每组数据最多 10 轮（judge.conf 的 `OJ_COMMUNICATION_MAX_PASSES` 可调）；本题固定 2 轮。

本题固定两轮：第一轮校验器把选手输出存进 `nextpass.in`，第二轮核对位置。

## 测试数据格式

第一轮输入：

```
first
n
p1 p2 … pn
```

第二轮输入（由校验器写入 `nextpass.in`）：

```
second
s
```

其中 `s` 是第一轮选手输出的小写字符串。`<用例>.ans` 是整数，表示 `n` 在排列
中的位置（下标从 1 开始）。

| 用例 | n | n 的位置 | 用途 |
| --- | --- | --- | --- |
| `1.in` | 3 | 1 | 最小规模、下边界 |
| `2.in` | 5 | 5 | 上边界 |
| `3.in` | 10000 | 7777 | 多位 26 进制编码 |

## 校验器行为

- 第一轮（`argv[1]` 首行为 `first`）：
  - 读 `n` 与排列；
  - 选手输出必须是非空、仅含 `a`–`z`、长度不超过 `10^5` 的字符串；
  - 合法则写入 `nextpass.in`（`second` + 编码串），退出码 42。
  - 不合法（如含大写字母）→ 退出码 1，第 1 轮即 WA。
- 第二轮（首行为 `second`）：
  - 读选手输出，必须是一个整数；
  - 与 `argv[2]` 中的期望位置比较，相同退出码 42，不同退出码 1。

## 编译

```bash
g++ -O2 -static data/validator.cpp -o data/validator
```

打包时由 `extra/demo/build_data.sh` 按 `data/BUILD` 交叉编译。

## 本地验证（无需数据库）

```bash
go build -o hustoj .
for f in ac wa_first wa_second; do
  ./hustoj judge --source extra/demo/problems/1006-run-twice/solutions/$f.cpp --lang 1 \
    --data extra/demo/problems/1006-run-twice/data --time 2000 --memory 262144 \
    --spj 4 --ojhome extra --workbase /tmp
done
```

期望：`ac` → `final_result = 4`（AC，`pass_rate = 1.0`）；
`wa_first` / `wa_second` → `final_result = 6`（WA，`pass_rate = 0.0`）。
