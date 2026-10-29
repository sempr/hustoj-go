# 示例题库（demo）

给全新安装的 OJ 附带的几道示例题，用来展示 OJ 的主要功能。由
`install_demo.sh` 装载进数据库：写入 `problem` / `contest` 表，并把判题数据
复制到 `<OJHome>/data/<题号>/`。

## 题目一览

| 题号 | 标题 | `spj` | 演示的功能 |
| --- | --- | --- | --- |
| 1000 | A+B Problem | 0 | 普通评测、多测试点、多语言提交 |
| 1001 | 子任务与部分分：数列求和 | 0 | OI 子任务、`[N]` 分值、部分分 |
| 1002 | 特殊评测：构造幻方 | 1 | SPJ（多解，退出码判定） |
| 1003 | 交互题：猜数字 | 3 | 交互评测（FIFO 双向通信） |
| 1004 | 文件 IO：读 sum.in 写 sum.out | 0 | 文件读写（`input.name`/`output.name`） |
| 1005 | 通讯题：多轮猜数 | 4 | 通讯评测（同一程序多轮运行，`nextpass.in` 传递下一轮输入） |
| 1006 | 两次通信：编码与定位 | 4 | run-twice 通讯（第一轮输出经 `nextpass.in` 传给第二轮） |

另有一个演示比赛 `contest_id=1000`，把上面 7 题全部挂进去。

## 演示提交：多语言 AC、部分分与各种判罚

`solutions/` 下有各语言的参考解和一批"反例"程序。`seed_samples.sh` 会写入：

- **多语言满分 AC**：1000~1004 每题每种已安装语言各一条，包括
  C / C++ / Python / Go / Java / Pascal / Bash / PHP / Perl / C# /
  TypeScript / Rust；
- **通讯题 AC**：1005、1006 的 C++ 参考解；
- **部分分**：1001 的 `partial.c` / `partial.cpp` / `partial.py`，均得 30 分；
- **捆绑测试 0 分**：1001 的 `bundle_partial.cpp`；
- **SPJ 反例**：1002 的 `wa.cpp`，输出不是幻方，判 WA；
- **通讯题反例**：1006 的 `wa_first.cpp`（第 1 轮 WA）、`wa_second.cpp`（第 2 轮 WA）；
- **各种非 AC 判罚**：PE / WA / TL / ML / OL / RE / CE 等。

```bash
sudo ./demo/seed_samples.sh --ojhome /srv/hustoj --pid-base 1739 --user demo
```

| 项 | 解法 | 预期判罚 |
| --- | --- | --- |
| `ac_1000_{c,cpp,py,go,java}` | `1000-aplusb/solutions/ac.{c,cpp,py,go,java}` | 满分 AC（4） |
| `ac_1001_{c,cpp,py}` | `1001-subtask/solutions/ac.{c,cpp,py}` | 满分 AC（4） |
| `ac_1002_{c,cpp,py}` | `1002-spj-magic/solutions/ac.{c,cpp,py}` | 满分 AC（4，SPJ） |
| `ac_1003_{c,cpp,py,java,go,pas,sh,php,pl,cs,ts,rs}` | `1003-interactive/solutions/ac.*` | 满分 AC（4，交互） |
| `ac_1004_{c,cpp,py}` | `1004-fileio/solutions/ac.{c,cpp,py}` | 满分 AC（4，文件 IO） |
| `ac_1005_cpp` | `1005-communication-guess/solutions/ac.cpp` | 满分 AC（4，通讯） |
| `ac_1006_cpp` | `1006-run-twice/solutions/ac.cpp` | 满分 AC（4，两次通信） |
| `wa_1006_first` | `1006-run-twice/solutions/wa_first.cpp` | 第 1 轮 WA(6)：编码串含大写字母 |
| `wa_1006_second` | `1006-run-twice/solutions/wa_second.cpp` | 第 2 轮 WA(6)：位置输出 0 |
| `partial_1001_{c,cpp,py}` | `1001-subtask/solutions/partial.{c,cpp,py}` | 部分分 **30/100**（`pass_rate=0.300`） |
| `wa_1002` | `1002-spj-magic/solutions/wa.cpp` | SPJ 答案错误 WA(6)：按行输出 1..n²，不是幻方 |
| `pe` | `1000-aplusb/solutions/pe.cpp` | 格式错误 PE(5)：答案对，行首多一个空格 |
| `wa` | `1000-aplusb/solutions/wa.cpp` | 答案错误 WA(6)：答案大 1 |
| `tl` | `1000-aplusb/solutions/tle.cpp` | 时间超限 TL(7)：死循环 |
| `ml` | `1000-aplusb/solutions/mle.cpp` | 内存超限 ML(8)：写满 400MB |
| `ol` | `1000-aplusb/solutions/ole.cpp` | 输出超限 OL(9)：输出 150MB |
| `re` | `1000-aplusb/solutions/re.cpp` | 运行错误 RE(10)：主动 abort |
| `ce` | `1000-aplusb/solutions/ce.cpp` | 编译错误 CE(11)：故意少一个括号 |
| `ml_first` | `1000-aplusb/solutions/mle_first.cpp` | 内存与输出都会超，**内存先撞上限** → ML(8) |
| `ol_first` | `1000-aplusb/solutions/ole_first.cpp` | 内存与输出都会超，**输出先撞上限** → OL(9) |
| `bundle` | `1001-subtask/solutions/bundle_partial.cpp` | 捆绑测试：子任务 1 只过 1/2 → 整组 **0 分**（不是 15 分） |

注意：

- `--pid-base` 与 `install_demo.sh` 的 `--pid-base` 是同一套编号（A+B 题为
  N、子任务题为 N+1），对着已用 `--auto-pid` 装载过的题库继续用即可；
- `--user` 指定的账号必须已存在；
- 脚本写 `solution` / `source_code` / `source_code_user` 三张表，缺一张
  showsource.php 就会显示"没有可用代码"；
- 脚本按「用户 + 题目 + 语言 + 源码」去重，可重复执行；重跑只会补装新增项，
  确实要重复插入同一份代码时加 `--force`；
- 没有对应 `<id>.lang.toml` 的语言会自动跳过，不会写入判题器跑不了的提交；
- `ce.cpp` 故意编译不过，别拿它跑自检脚本；
- 内存用量（`memory` 列）是 **cgroup 匿名内存峰值**：多进程程序算总和，
  不含沙箱 tmpfs 上的输出；核对方法见 `tests/memprobe/`。

## 装载

安装时一并做：

```bash
sudo ./install.sh --init-db --db-password '...' --demo
```

或安装后单独做（数据库凭据取自 `<OJHome>/etc/judge.conf`）：

```bash
sudo ./demo/install_demo.sh --ojhome /home/judge
```

常用选项：`--only 1000,1002`、`--no-contest`、`--force`（覆盖已存在的题）、
`--dry-run`。脚本幂等，重复执行默认跳过已存在的题。

### 追加到已有题库末尾

已有题库时，示例题固有序号 1000..1006 往往被占用。用 `--pid-base`/`--auto-pid`
把示例题整体顺延到末尾（`--only` 仍按原始序号 1000..1006 筛选）：

```bash
# 取库中最大题号 +1 作起始，追加到题库末尾（如库里到 1738，则装成 1739..1745）
sudo ./demo/install_demo.sh --ojhome /srv/hustoj --auto-pid --no-contest

# 或显式指定起始序号
sudo ./demo/install_demo.sh --ojhome /srv/hustoj --pid-base 1739 --no-contest

# 需要演示比赛时用 --contest-id 指定一个未被占用的编号
sudo ./demo/install_demo.sh --ojhome /srv/hustoj --auto-pid --contest-id 1119
```

`--auto-pid` 只改示例题的题号（problem_id 与 `<ojhome>/data/<题号>/` 目录），
题目正文、判题数据与 SPJ/交互器二进制都不变。

### 演示比赛与题库可见性

演示比赛默认赛期是「昨天开始 → 一年后结束」，即**进行中**。按 HUSTOJ 规则，
挂在「进行中」比赛下的题目**不会出现在普通用户的题库列表**（`problemset.php`）
与题目页里，只能从比赛入口 `contest.php?cid=<编号>` 进入 —— 这是防泄题的既定
行为，不是装载失败。

若希望这 7 题同时出现在题库列表，把演示比赛设为**已结束**（`end_time` 早于当前
时间），或不创建比赛（`--no-contest`）。管理员登录后在题库列表能看到全部题目，
不受此限制。

## 目录结构

```
demo/
├── README.md          # 本文件
├── install_demo.sh    # 装载脚本（目标机运行）
├── seed_samples.sh    # 演示提交脚本：把 solutions/ 下的反例写成提交
├── build_data.sh      # 打包机：按 arch 交叉编译各题 data/BUILD 里的判题程序
├── contest.env        # 演示比赛定义
└── problems/
    └── <题号-别名>/
        ├── meta.env          # PID/TITLE/TIME_LIMIT/MEMORY_LIMIT/SPJ/COIN/SOURCE
        ├── statement/        # description.md input.md output.md hint.md
        ├── sample/           # sample.in sample.out（可选）
        ├── data/             # 判题数据；内含 BUILD 的题需编译判题程序
        └── solutions/        # 多语言参考解与反例（仅供自检/参考/演示）
```

## SPJ / 交互器 / 通讯校验器二进制

目标机没有编译器，所以 `data/BUILD` 列出的判题程序（如 1002 的 `spj.c`、
1003 的 `spj.cc`、1005 / 1006 的 `validator.cpp`）在**打包机**由
`build_data.sh <goarch> <demo-dir>` 交叉编译，产物随部署包分发。缺交叉编译器时
会告警并跳过该产物（该题将无法判题）。

本地重新编译（当前架构）：

```bash
extra/demo/build_data.sh "$(go env GOARCH)" extra/demo
```

## 通讯题（`spj=4`）

1005 / 1006 演示通讯评测：**同一个程序在一组数据上被反复运行**，每轮之间
内存与变量全部丢弃，只能通过评测器写出的 `nextpass.in` 传递信息。实现见
`internal/client/communication.go`，各题细节见
`problems/1005-communication-guess/README.md` 和
`problems/1006-run-twice/README.md`。

数据目录约定：

- 必须有可执行的 `validator`（旧数据可叫 `output_validator`），打包时由
  `data/BUILD` 编译；缺失时该提交判 RE，并提示 `missing output validator`。
- 每个 `<用例>.in` 是一组数据的**首轮**输入；之后各轮输入由校验器写
  `<工作目录>/nextpass.in` 产生。
- `<用例>.ans` 是给校验器的答案文件（argv[2]）；缺失时判题侧补一个空文件。
- 校验器调用方式：`/code/validator <本轮输入> <答案> <工作目录>`，选手本轮
  输出走它的 stdin，反馈走 stdout/stderr。
- 退出码：`0` / `42` = 本轮通过，其它 = WA，崩溃/超时 = RE；本轮通过后
  `nextpass.in` 存在就继续下一轮，不存在即本组通过。每组数据最多 10 轮
  （judge.conf 的 `OJ_COMMUNICATION_MAX_PASSES` 可调），超出判 WA。

## 本地自检（无需数据库）

```bash
go build -o hustoj .

# 1000 普通评测
./hustoj judge --source extra/demo/problems/1000-aplusb/solutions/ac.cpp \
  --lang 1 --data extra/demo/problems/1000-aplusb/data --time 1000 --memory 131072 \
  --spj 0 --ojhome extra --workbase /tmp

# 1002 SPJ（需先 build_data.sh 生成 data/spj）
./hustoj judge --source extra/demo/problems/1002-spj-magic/solutions/ac.cpp \
  --lang 1 --data extra/demo/problems/1002-spj-magic/data --time 1000 --memory 131072 \
  --spj 1 --ojhome extra --workbase /tmp

# 1003 交互（需先生成 data/spj）
./hustoj judge --source extra/demo/problems/1003-interactive/solutions/ac.cpp \
  --lang 1 --data extra/demo/problems/1003-interactive/data --time 2000 --memory 262144 \
  --spj 3 --ojhome extra --workbase /tmp

# 1005 通讯（需先生成 data/validator）
./hustoj judge --source extra/demo/problems/1005-communication-guess/solutions/ac.cpp \
  --lang 1 --data extra/demo/problems/1005-communication-guess/data --time 2000 --memory 262144 \
  --spj 4 --ojhome extra --workbase /tmp

# 1006 两次通信（同上；另有两份 WA 反例）
./hustoj judge --source extra/demo/problems/1006-run-twice/solutions/ac.cpp \
  --lang 1 --data extra/demo/problems/1006-run-twice/data --time 2000 --memory 262144 \
  --spj 4 --ojhome extra --workbase /tmp
```

判题结果里 `final_result=4` 表示 AC。更多端到端脚本见 `tests/`。
