# 交互题「猜数字」题目资源

本目录存放交互题（`problem.spj = 3`）**猜数字**的完整题目资源：题面、5 组测试数据、
交互器（interactor）源码与参考解，供后续补充到数据库时使用。

## 目录结构

```
guess_number/
├── README.md          # 本文件：编译、部署、入库说明
├── statement.md       # 题目描述（Markdown，粘贴到后台题目编辑框）
├── Makefile           # 编译交互器与参考解
├── data/
│   ├── spj.cc         # 交互器源码
│   ├── 1.in … 5.in    # 5 组测试数据（每组 = 一次交互会话）
│   └── spj            # 编译产物（make 生成，未入库）
└── solution/
    ├── ac.cpp         # 参考解（每轮二分）
    └── ac             # 编译产物（make 生成，未入库）
```

## 交互契约

与判题器 `internal/client/interactive.go` 一致（另见 `extra/spj/README.md`）：

- 数据目录中**必须**有名为 `spj` 的可执行文件；判题时它被拷入沙箱，
  以 `/code/spj /code/<用例>.in` 的形式运行。
- 选手 stdout → 交互器 stdin；交互器 stdout → 选手 stdin（各经一条 FIFO）。
- 交互器退出码 `0` = AC，非 `0` = WA；任一侧超时/崩溃时以该侧状态为准。
- **每个 `.in` 文件 = 一次独立交互会话**。本题目提供 5 个 `.in`，因此共 5 次会话，
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
make            # 生成 data/spj 与 solution/ac
# 或手动：
g++ -O2 -static data/spj.cc   -o data/spj
g++ -O2 -static solution/ac.cpp -o solution/ac
```

## 部署到判题服务器

假设新题目编号为 `<PID>`：

```bash
PID=<PID>
mkdir -p /home/judge/data/$PID
cp data/1.in data/2.in data/3.in data/4.in data/5.in /home/judge/data/$PID/
g++ -O2 -static data/spj.cc -o /home/judge/data/$PID/spj
chmod 0755 /home/judge/data/$PID/spj
chown -R judge:judge /home/judge/data/$PID 2>/dev/null || true
```

> 交互器可执行文件名必须恰为 `spj`（判题器按此名在数据目录中查找）。

## 入库 SQL

题面正文见 `statement.md`。把题面内容写入 `problem.description`，并将
`spj` 字段设为 `3`（`OJ_SPJ_MODE_INTERACTIVE`，见 `pkg/constants`）。示例：

```sql
SET @desc = LOAD_FILE('/use/absolute/path/statement.md');  -- 或直接粘贴题面文本

INSERT INTO problem
  (problem_id, title, description, input, output, sample_input, sample_output,
   spj, hint, source, time_limit, memory_limit, submit, solved)
VALUES
  (<PID>, '猜数字', @desc, '', '', '', '',
   3, '', 'interactive', 1, 128, 0, 0);
```

> 若走后台界面，直接把 `statement.md` 的内容粘贴进「题目描述」，
> 并在题目设置里将判题方式设为交互/`spj=3`。

## 本地验证（无需数据库）

用独立判题命令端到端验证（在仓库根目录执行）：

```bash
go build -o hustoj .
./hustoj judge --source extra/problems/guess_number/solution/ac.cpp --lang 1 \
  --data extra/problems/guess_number/data --time 2000 --memory 262144 \
  --spj 3 --ojhome extra --workbase /tmp
```

期望输出 JSON 中 `final_result = 4`（AC）、`pass_rate = 1.0`。
断言方式可参考 `tests/interactive/test.sh`。
