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

另有一个演示比赛 `contest_id=1000`，把上面 5 题全部挂进去。

## 演示提交：各种判罚

`solutions/` 下除了参考解，还放了一批"反例"程序，每个对应一种判罚结果。
用 `seed_samples.sh` 把它们写成提交（进正常判题队列，判完由判题器回写），
就能在 status.php 上看到各种判罚：

```bash
sudo ./demo/seed_samples.sh --ojhome /srv/hustoj --pid-base 1739 --user demo
```

| 项 | 解法 | 预期判罚 |
| --- | --- | --- |
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

已有题库时，示例题固有序号 1000..1004 往往被占用。用 `--pid-base`/`--auto-pid`
把示例题整体顺延到末尾（`--only` 仍按原始序号 1000..1004 筛选）：

```bash
# 取库中最大题号 +1 作起始，追加到题库末尾（如库里到 1738，则装成 1739..1743）
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

若希望这 5 题同时出现在题库列表，把演示比赛设为**已结束**（`end_time` 早于当前
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

## SPJ / 交互器二进制

目标机没有编译器，所以 `data/BUILD` 列出的判题程序（如 1002 的 `spj.c`、
1003 的 `spj.cc`）在**打包机**由 `build_data.sh <goarch> <demo-dir>` 交叉编译，
产物随部署包分发。缺交叉编译器时会告警并跳过该产物（该题将无法判题）。

本地重新编译（当前架构）：

```bash
extra/demo/build_data.sh "$(go env GOARCH)" extra/demo
```

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
```

判题结果里 `final_result=4` 表示 AC。更多端到端脚本见 `tests/`。
