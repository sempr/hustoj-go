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

## 目录结构

```
demo/
├── README.md          # 本文件
├── install_demo.sh    # 装载脚本（目标机运行）
├── build_data.sh      # 打包机：按 arch 交叉编译各题 data/BUILD 里的判题程序
├── contest.env        # 演示比赛定义
└── problems/
    └── <题号-别名>/
        ├── meta.env          # PID/TITLE/TIME_LIMIT/MEMORY_LIMIT/SPJ/COIN/SOURCE
        ├── statement/        # description.md input.md output.md hint.md
        ├── sample/           # sample.in sample.out（可选）
        ├── data/             # 判题数据；内含 BUILD 的题需编译判题程序
        └── solutions/        # 多语言参考解（仅供自检/参考）
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
