# 示例题 1005 · 通讯题「奇偶子集的双射」

通讯题（`problem.spj = 4`）的完整资源：题面、4 组测试数据、校验器源码与参考解。
由 `extra/demo/install_demo.sh` 连同其它示例题一起装载。

## 目录结构

```
1005-communication-guess/
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
│   ├── 1.in … 4.in       # 4 组测试数据；每个 .in 是第一轮输入（X）
│   ├── 1.ans … 4.ans     # 每组对应的原集合 X（第二轮的期望输出）
│   └── validator         # 编译产物（打包时生成，未入库）
└── solutions/
    ├── ac.cpp                       # 参考解（toggle n）
    └── wa_same_parity.cpp           # 反例：输出奇偶性不变 → 第 1 轮 WA
```

## 通讯契约

与判题器 `internal/client/communication.go` 一致：

- 数据目录中**必须**有名为 `validator`（或 `output_validator`）的可执行文件。
- 每组数据的判定流程：
  1. 第 1 轮把 `<用例>.in` 喂给选手程序；
  2. 之后每轮把上一轮校验器写出的 `nextpass.in` 喂给选手；
  3. 选手的输出交给校验器：
     `/code/validator /code/data.in /code/jury.ans /code`。
- 校验器退出码：

  | 退出码 | 含义 |
  | --- | --- |
  | `0` 或 `42` | 本轮通过 |
  | 其它 | 答案错误（WA） |
  | 被信号杀死 / 超时 / `127` | 校验器崩溃（RE） |

- 本轮通过后，若工作目录存在 `nextpass.in`，则进入下一轮；不存在即本组数据通过。
- 每组数据最多 10 轮（`config.DefaultCommunicationMaxPasses`），可由 judge.conf 的
  `OJ_COMMUNICATION_MAX_PASSES` 调整，超过判 WA。
- 一个 `.in` = 一组数据 = 完成双射的两轮；4 组全过才 AC。

## 测试数据格式

`<用例>.in`（选手首轮看到的内容）与 `nextpass.in` 相同：

```
L R
```

表示隐藏整数 `x ∈ [L, R]`。`<用例>.ans` 只有一行，是隐藏整数 `x`。

| 用例 | n | 输入集合 X | 期望第一轮输出 Y |
| --- | --- | --- | --- |
| `1.in` | 5 | `1 3 5` | `1 3`（删 5，长度 2） |
| `2.in` | 5 | `2 4` | `2 4 5`（加 5，长度 3） |
| `3.in` | 1 | ∅ | `1`（加 1） |
| `4.in` | 10 | `1 2 3 4 5 6 7 8 9 10` | `1 … 9`（删 10） |

## 校验器行为

1. 第一轮：按 `toggle_n(X)` 验证第一轮输出；写 canonical 形式的 `Y` → `nextpass.in`；
   奇偶性不变（`parity(|Y|) == parity(|X|)`）→ 1（WA）；
   格式/越界 → 1（WA）。
2. 第二轮：选手输出必须等于 `.ans` 中的原集合 `X`；不等于 → 1（WA）；
   解析/格式错误 → 1（WA）；stderr 反馈失败原因。

## 编译

```bash
g++ -O2 -static data/validator.cpp -o data/validator
```

打包时由 `extra/demo/build_data.sh` 按 `data/BUILD` 交叉编译好 `data/validator`
放进部署包，目标机无需编译器。

## 本地验证（无需数据库）

```bash
go build -o hustoj .
./hustoj judge --source extra/demo/problems/1005-communication-guess/solutions/ac.cpp --lang 1 \
  --data extra/demo/problems/1005-communication-guess/data --time 2000 --memory 262144 \
  --spj 4 --ojhome extra --workbase /tmp
```

期望 `final_result = 4`（AC）、`pass_rate = 1.0`。
