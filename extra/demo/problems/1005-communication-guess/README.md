# 示例题 1005 · 通讯题「多轮猜数」

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
│   ├── 1.in … 4.in       # 4 组测试数据；每个 .in 是某一组的首轮输入
│   ├── 1.ans … 4.ans     # 每组隐藏整数（校验器只读，不给选手看）
│   └── validator         # 编译产物（打包时生成，未入库）
└── solutions/
    └── ac.cpp            # 参考解（二分）
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
- 最多 100 轮（`communicationMaxPasses`），超过判 WA。
- 一个 `.in` = 一组数据 = 从首轮猜到猜中的完整过程；4 组全过才 AC。

## 测试数据格式

`<用例>.in`（选手首轮看到的内容）与 `nextpass.in` 相同：

```
L R
```

表示隐藏整数 `x ∈ [L, R]`。`<用例>.ans` 只有一行，是隐藏整数 `x`。

| 用例 | 首轮区间 | 隐藏整数 | 用途 |
| --- | --- | --- | --- |
| `1.in` | `1 1000000000` | `20261028` | 最大区间，约 30 轮 |
| `2.in` | `1 1000` | `1` | 下边界 |
| `3.in` | `1 1000` | `1000` | 上边界 |
| `4.in` | `1 1000000` | `524288` | 2 的幂，二分轮数上限 |

## 校验器行为

1. 读 `argv[1]` 得到本轮区间 `[L, R]`，读 `argv[2]` 得到隐藏整数 `x`。
2. 从 stdin 读选手本轮的输出，要求恰好一个整数 `guess`，且 `L ≤ guess ≤ R`。
3. `guess == x`：不写 `nextpass.in`，退出码 42（本组通过）。
4. `guess < x`：写 `nextpass.in` 为 `guess+1 R`；
   `guess > x`：写 `nextpass.in` 为 `L guess-1`；退出码 42（继续下一轮）。
5. 输出非法或越界：退出码 1（WA）。

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
