# phpweb → Go Web 迁移设计

本文记录把 `/root/gojudge/phpweb`（HUSTOJ PHP 前端）迁移到 Go Web 服务的方案。
迁移采用绞杀者模式：两侧共享同一 MariaDB 与 `/home/judge/data`，nginx 按路径
把流量分给 PHP-FPM 或 Go 服务，不存在数据同步问题，可以逐个路径切换并随时回退。

---

## 1. 现状：两个独立的目录

这是迁移中最容易搞错、也最容易造成事故的一点：

| 路径 | 性质 | 说明 |
|---|---|---|
| `/root/gojudge/phpweb` | git 工作树 | 开发用。`git ls-files phpweb` 当前为 0，`phpweb/` 整体未被 git 跟踪 |
| `/home/judge/src/web` | 生产部署树 | nginx `root` 指向此处。非 git 目录 |

**修改仓库树不会影响线上。** 生产部署树是独立副本，两者当前有 56 处差异。
本文所称"仓库配置"一律指前者；未经发布流程，两者不同步。

nginx 站点配置：`/etc/nginx/sites-enabled/0-default`（`root /home/judge/src/web`）、
`1-armoj`（`server_name armoj.uod.edu.kg`）。

---

## 2. 两条铁律

### 2.1 迁移期两侧行为必须逐条等价

用户不会接受"切到 Go 后题目列表少了几道""顺序变了""AC 标记消失"。
因此每条 SQL、每个渲染分支都对照 PHP 源码实现，并用对拍测试守住。

### 2.2 不解析 `db_info.inc.php`

Go 刻意不读 PHP 的配置文件（它含明文数据库口令，且是 PHP 侧的单一配置源）。
运营参数走 `web.toml` / 环境变量，数据库凭据只由 systemd 的
`EnvironmentFile=/home/judge/etc/judge.conf`（`root:root 0600`）注入。
systemd 由 PID 1 以 root 身份读取该文件，再降权执行进程，因此无需放宽权限。

代价是 `$OJ_FREE_PRACTICE` 这类开关两端各有一份，**发布时必须同步**，
否则题目列表可见范围会不一致。

---

## 3. 已完成的等价性差异清单

以下差异都是实查源码 + 对拍真实库发现的，不是推测。

### 3.1 `problem_id > 0` 只在无搜索分支

`problemset.php:72-93` 有三个分支：无搜索时 `$filter_sql = " problem_id > 0"`；
带 `search` 时被整体替换成 `( title like ? or source like ?)`，**不再含该条件**。

所以无搜索的列表/计数查询要有它，搜索查询不能有。加错或漏掉都不会报错，
只会安静地多列或少列题目。

> 期间我曾误判"管理员访问 `/problemset.php` 在生产上是坏的（SQL 1064）"——
> 那是我手写探针时把 `$filter_sql` 初始化成空串导致的误报，PHP 实际正常。
> 该结论已作废，勿据其改代码。

### 3.2 详情页比列表页多一条 `private='1'`

`problem.php:41-44` 的成员模式：

```sql
WHERE `problem_id`=? AND `defunct`='N' AND NOT EXISTS (
  SELECT 1 FROM `contest_problem` cp
  INNER JOIN `contest` c ON cp.contest_id=c.contest_id
  WHERE cp.problem_id=? AND (c.end_time>'$now' AND c.defunct='N' OR c.private='1')
)
```

列表页的 `problemset.php` 没有 `private` 条件。两页规则不一致是上游既有行为，
Go 侧照搬：放宽会让学生改 URL 提前看到私有比赛/考试题。

### 3.3 `MIN(result)` 必须 CAST

`schema/queries/problem.sql` 里 `MIN(result)` 直接写时 sqlc 无法确定 smallint
的 `MIN` 返回类型，会生成 `interface{}`，调用方无法与判罚码常量比较。
已改为 `CAST(MIN(result) AS SIGNED)`。

同时 `result>=4`（提交过，含部分分）与 `result=4`（AC）必须分开：
改成 `>4` 会把部分分用户误判为未提交，改成 `MAX(result)` 会把 AC 用户判成未通过。

### 3.4 `parsePage` 要复刻 `intval`

`strconv.Atoi(" 2 ")` 和 `Atoi("2abc")` 都报错，而 PHP `intval` 分别得到 2 和 2。
这些输入在真实环境并不少见（手改 URL、从别处复制查询串），
静默当成第 1 页会让人以为分页失灵。已改为 `intval` 语义。

PHP 不夹下界，`?page=0` 会生成 `LIMIT -50,50` 直接 SQL 报错；Go 侧夹到 1。

### 3.5 AC/提交过标记的字面量

`problemset.php:170-176`：AC → `Y`，提交过未 AC → `N`，未提交 → 空白。
极易写反（两个都写 `Y`），代码与测试都显式断言字面值。

### 3.6 标签配色依赖 md5

`$color_theme[$hash_num % count($color_theme)]`，`$hash_num = hexdec(substr(md5($cat),0,7))`，
且 hash 的是 **`get_domain()` 处理之后**的值。改动 `colorTheme` 顺序会让所有标签变色。

### 3.7 上游缺陷（不复刻）

`problemset.php` 第 2 列把 `$row['title']` 直接拼进 HTML，无 `htmlentities`，
属存储型 XSS。当前题库 0 道标题含尖括号，线上未暴露，补题即可触发。
Go 侧全部经 `html/template` 转义。

`html/template` 会把 `+` 转义成 `&#43;`（防 UTF-7 型 XSS 的历史行为），
浏览器显示正常，非 bug。

### 3.8 评测结果文案与部署版 lang 不同（待确认）

Go 的 `judgeResultLabel`（`handler_status.go:511`）用的是
「通过 / 错误答案 / 超时」，而部署树 `phpweb/lang/cn.php` 里
`$jresult` 取的是「正确 / 答案错误 / 时间超限」（`lang/cn.php:83-88`）。
这不是本次迁移引入的：`status.php` 与 `userinfo.php` 已经用的是前者。
`problemstatus.php`（4.3）沿用同一份标签以保持 Go 页面间一致。若要求与
PHP 逐字一致，应把 `judgeResultLabel` 与 `resultLabels` 一起改成 lang
文案，并同步更新 `handler_status_test.go` 的断言。

---

## 4. 当前实现范围

### 4.1 已完成

- `/problemset.php` — 三模式列表 + 搜索 + 分页
- `/problem.php` — 练习模式（`?id=`）+ **比赛模式（`?cid=&pid=`）**
- `/healthz`（不触库）、`/readyz`（探连接池）

比赛模式的实现要点（方案 B，补齐切流回归）：

- **`/problem.php` 是两种模式的公共入口**。切流后才发现比赛排名页
  （`contestrank*.php`、`conteststatistics.php`）的链接全部指向
  `problem.php?cid=X&pid=Y`，handler 只认 `?id=` 会返回 400 —— 真实回归。
- **取题键是 `contest_problem.num`**（1-based 展示顺序），不是 problem_id
  （problem.php:95-97）。
- **比赛准入判定收拢在 `EvaluateContestAccess`**，顺序复刻 PHP：
  subnet（contest-check.php:43）→ defunct（:49）→ private（:60-72）→
  未开始（:85-96）；管理员/创建者强行置真（:77-78），实际绕过除 subnet 外
  的全部检查。
- **比赛模式不过滤 defunct**：已删除题目仍要能在比赛里打开
  （problem.php:95-97 无 defunct 条件），练习模式才过滤。
- **私有比赛**：session 桥落地前，Go 侧只能查 `privilege` 表的 `c<cid>`
  授权（contest-check.php:62-70）；无会话一律拒绝 —— 错放行等于泄露赛题。
- **个人限时赛（"限时"关键字）**：PHP 依赖 session + loginlog 计时
  （contest-check.php:101-140）。Go 侧未登录时跳过即与 PHP 一致
  （PHP 也对匿名跳过）；带会话时无法可靠计时 → 拒绝（fail-safe）。
  当前生产比赛（cid=1000）描述为 "Offline contest imported by admin"，
  未启用该功能。
- **NOIP 锁榜**：`problem_locked($id, 28)` 命中时隐藏 AC 数与提交数
  （替换成问号），并按 `OJ_NOIP_HINT=false` 隐藏提示。对两种模式都生效
  （problem.php:150-168 在模式分支之外），管理员/创建者不受影响。
- **比赛错误页保留 HTTP 200**：PHP 一律 `exit(0)` 渲染 error.php
  （problem.php:66-68、:86-89，contest-check.php:172-173）。
  比赛未开始、密码错误都是正常状态，用 404 会让监控误判。
  密码输入表单不实现：它 POST 到 `contest.php?cid=`，该路径仍在 PHP 侧。

排名页与导出（已迁移）：

- `/contestrank.php` — 正式榜；`/contestrank4.php`、`/contestrank5.php` — 补题榜
  （`handler_contest_review.go`，解决集 × ACM/OI 两种计分）。
- `/contestrank.xls.php` — Excel 导出（`handler_contest_xls.go`）。
  输出是 `application/excel` 的 HTML table，**不是 XLSX**，不要"改成"真 xlsx。
- 取题键统一是 `contest_problem.num`（0-based 存储，1-based 展示）。
- XLS 的 mark 由排序后的名次做正态分布分配（`getMark`，mark_start=60、
  mark_end=100、sigma=5），**会覆盖**按题累加的 mark；单题比赛直接满分
  （`mark_base=100`）。WA 惩罚取整数秒，AC 惩罚是首个 AC 与上次 AC 的差值。
- XLS 无时间窗口：`contest_id=? AND num>=0 AND problem_id>0`，按
  `user_id, solution_id` 排序，逐条累加。封榜隐藏只看
  `in_date > end - 封榜比例*赛长`，豁免**仅管理员**（比 NOIP 豁免窄）。
- XLS 不复刻两个上游缺陷：`getMark` 的零除（PHP 会
  `DivisionByZeroError` 并截断页面，Go 输出完整空表）；NOIP/locked 走
  `error.php` 整页（Go 沿用站内 `errorPage`，外框不同、语义相同）。
- XLS 保留 `sec2str()` 的负数语义（`-3600 -> -1:00:00`），与
  contestrank.php 共用 `handler_rank.go` 里的同一个实现。
- XLS 的 Excel 公式注入防护按**实际行为**而非本意实现：PHP 写的
  `/^[=+-@]/` 里 `-` 未转义，PCRE 解释成 `+`(0x2B) 到 `@`(0x40) 的区间，
  因此 `,` `-` `.` `/` 数字 `:` `;` `<` `>` `?` 也都会命中。

### 4.2 关键实现约束

- **路径与 PHP 完全一致**，切流只改 nginx 路由，不改 URL、不做重定向。
- **每页 50 条**（`problemset.php:66`），不可配置，两端必须一致。
- **搜索是裸 LIKE**：用户输入的 `%` 和 `_` 就是通配符（PHP 的 `like ?` 没有转义）。
  Go 侧若擅自转义会与 PHP 不一致，要改必须两端同时改。
- **纯空白搜索词等同未搜索**（PHP 的 `trim()`），否则会走搜索分支却匹配不到任何东西。
- **`Server` 长期持有 `*sql.DB`**，不在每请求 Open/Close，否则打满 `max_connections`。
- **未登录时不查用户完成状态**（与 PHP 一致），否则每个匿名请求都会触发
  `solution` 表扫描。
- **管理员优先于自由练习开关**（`problemset.php:120` 的判断顺序）。
- **排名排序用 `sort.SliceStable`**：PHP 8 的 `usort` 已稳定，且
  `contestrank.xls.php` 的 `s_cmp` 字面返回 `$A->solved < $B->solved`
  （布尔被 usort 当"排在前"），实际语义是 **solved 降序 + time 升序**。
  别照字面抄，也别换成不稳定的 `sort.Slice`。

### 4.3 其余已迁移路径

- **PHP session 桥**（`session_bridge.go`、`session.go`）：只读解析 PHP
  会话文件，把 `UserID`、管理员标识、`m<cid>` 限时赛已用标记、`c<cid>`
  私有比赛受邀标记映射进 `Identity`；`main.go` 按 `cfg.SessionBridge`
  接线。Go 自建会话与 PHP 桥并存，注册/登录/登出写 Go 会话
  （`loginpage.php`/`login.php`/`registerpage.php`/`register.php`/
  `logout.php`/`vcode.php`）。
- **个人限时赛计时**：session 桥落地后，`Identity.ContestTimeLimitUsed`
  已由 `session.go:138` 与 `session_bridge.go:123` 填充，
  `EvaluateContestAccess`（`contest.go:130`）据此拒绝超时。
- **提交路径**：`submitpage.php`、`submit.php`。`submit.php` 的 token
  在一次提交与多次轮询间可重复使用，走 `csrf_check_ajax.php` 的非单次
  消费校验（已由 CSRF 回归覆盖）。
- **状态页**：`status.php`。
- **档案页**：`userinfo.php`（含名次重算、金币重算、登录日志仅管理员）。
- **排名页**：`ranklist.php`（scope/非 scope 两种模式）。
- **单题状态页**：`problemstatus.php`。展示提交/人数/AC 人数、各结果分布、
  每题每用户最优 AC 提交榜单与相关推荐；打开即回写 `problem.submit` /
  `problem.accepted`。`$OJ_AUTO_SHARE` 生产关闭，其分支（含 `s<id>` 授权）
  未实现。榜单查询因 sqlc 无法解析派生表而手写
  （`pkg/dbgen/problemstatus_manual.go`）。
- **源代码查看页**：`showsource.php`。三层权限：锁榜拦截（
  `problem_locked($id,1)`，source_browser/administrator 豁免）、考试模式
  `$OJ_EXAM_CONTEST_ID`（只豁免 source_browser）、以及 `s<problem_id>`
  / 本人 / source_browser 三者之一。锁榜错误里的比赛号，上游误写成
  `$result[0][0]`（实为 start_time 或 solution_id，见 3.7），Go 显示真正的
  比赛号。源码经 `html/template` 转义，注释头用 `judgeResultLabel`（见 3.8）。
- **比赛列表**：`contest.php`（含"我的比赛"三路来源）。
- **状态轮询接口**：`status-ajax.php`。`solution_id` 缺失即回显
  `<id>0, 0, 0,unknown,0`。公开（`$OJ_PUBLIC_STATUS`）/本人/source_browser
  可见时返回 `result,memory KB,time ms,judger,pass_rate*100,user_id`，其中
  judger 仅管理员返回真实值，否则字面量 `none`；进行中 NOIP 比赛（标题含
  `$OJ_NOIP_KEYWORD` 或 `contest_type & 16`）只回关键字。`tr` 分支在
  `$OJ_SHOW_DIFF` 且非锁榜时对本人返回编译/运行详情，spj==2 且
  `$OJ_HIDE_RIGHT_ANSWER` 时拒绝；读到详情后清 `custominput`。新增配置
  `show_diff`/`hide_right_answer`/`public_status`/`remote_judge`（默认
  true/true/true/false）。`t=json` 无页面调用，Go 用 `encoding/json` 序列化
  `dbgen.Solution`，字段细节与 PHP `json_encode` 不完全一致（见 3.7）。
  `trigger_judge`（依赖 `$OJ_REMOTE_JUDGE`）未实现。
- **命题人接口**：`problem-ajax.php`。按 `rightstr='p<pid>'` 查 `privilege`，
  有则回 `userinfo.php` 链接（转义），无则 `外部导入`。前端在 `problem.php`
  异步填入。
- **比赛列表**：`contest.php`（含"我的比赛"三路来源）。
- **后台**：目前仅 `admin/problem_batch_delete.php`（事务化重写）。

---

## 5. 测试与门禁

```bash
export PATH="$PATH:/root/go/bin"   # sqlc / staticcheck 在 ~/go/bin
bash extra/check_all.sh            # 统一门禁，必须全绿才可发布
```

覆盖：gofmt、build、vet、staticcheck、go test、php lint、PHP 插值、
CSRF 回归（65 项）、语言索引、schema 一致性、sqlc codegen 同步。

### 5.1 两层对拍（需显式开启）

```bash
export HUSTOJ_EQUIV_DSN='user:pass@tcp(127.0.0.1:3306)/jol?parseTime=true&loc=Local'
HUSTOJ_EQUIV_TEST=1  go test ./internal/web/ -run Equiv -v   # SQL 层
HUSTOJ_HTTP_EQUIV=1  go test ./internal/web/ -run HTTPEquiv -v # HTTP 渲染层
```

PHP 侧由 `php` 解释器执行 `problemset.php` 的原始拼装逻辑返回 `problem_id`，
而不是用 Go 重写一遍 —— 否则 PHP 源码一改，"权威实现"就悄悄过期了。

覆盖 3 模式 × 4 搜索词 × 3 页 = 36 组，外加计数与完成状态。

排名页/XLS 另有一条独立的对拍链路（oracle 是部署树
`/home/judge/src/web`，经 `/tmp/run_xls4.php` 输出，注意它必须
`include/memcache.php`，否则 PHP 8.5 报未定义
`mysql_query_cache()`）：

```bash
/tmp/cmp_xls.sh                      # 默认 cid 列表，逐行 diff
HUSTOJ_XLS_DUMP=/tmp/go_xls HUSTOJ_XLS_CIDS=1000 \
HUSTOJ_EQUIV_DSN='...' TZ=Asia/Shanghai \
  go test ./internal/web -run TestContestRankXLSOracleDump
```

导出 HTML 由 `TestContestRankXLSOracleDump` 落盘（`*.html` + `*.headers`），
常规测试运行自动跳过。合成数据在 `/tmp/xls_fixture.sql`，
清理用 `/tmp/xls_fixture_cleanup.sql`。

**fixture 与测试共享同一个影子库，两个坑：**

- 补题榜 fixture 用显式 `solution_id`（`Add` 累加依赖提交顺序），
  会把 `solution` 的 AUTO_INCREMENT 顶上去。所以清理**不能**用
  `DELETE ... WHERE solution_id >= 9900000` 这种阈值，会连带删掉别的用例
  自动生成的行。改为按 `contest_id` / `user_id` 精确删。
- **不要在常驻 fixture 里造"进行中的 NOIP 比赛"**：`ranklist.php` 会全局
  拦截任何进行中的 NOIP 比赛（非管理员），一个常驻的 980005 会让
  `TestRanklistRenderIntegration` 稳定失败。XLS 的 NOIP 分支用一次性 SQL
  `/tmp/xls_noip_probe.sql` 造，对拍完立刻 `/tmp/xls_noip_cleanup.sql` 删。

### 5.2 已知的对拍盲区

真实库当前**没有** `defunct='Y'` 的题目，`problem_id` 也全为正数，
所以 `defunct='N'` 与 `problem_id > 0` 这两个谓词对返回行没有影响。
已实测确认：给查询加一个多余的 `problem_id > 0`，对拍依然全绿。

这类漂移由 `pkg/dbgen/problem_predicates_test.go` 的 SQL 文本断言兜住。
将来题库真的出现已删除题目或非正 id，对拍会自然多一层覆盖。

（无 `CREATE DATABASE` 权限，无法建影子库做合成数据，这是当前只能用文本断言的原因。）

### 5.3 变异测试记录

确认上述测试不是空转：

| 变异 | 检出 |
|---|---|
| 成员搜索查询多加 `problem_id > 0` | 谓词测试 2 项失败 |
| 自由练习列表误加比赛排除子查询 | 谓词测试 1 项失败 |
| `result>=4` 改成 `>4` | 谓词测试 1 项失败 |
| `MIN(result)` 改成 `MAX` | 等价性测试 4 处差异 |
| 渲染层每 3 行漏 1 行 | HTTP 对拍 10 个子用例失败 |
| `parsePage` 退回 `Atoi` | 单元测试 3 项失败 |
| 成员详情去掉 `private` | 谓词测试 1 项失败 |
| 自由练习详情误加 `NOT EXISTS` | 谓词测试 2 项失败 |
| 管理员详情加 `defunct` 过滤 | 谓词测试 2 项失败 |
| 比赛查询塞进 defunct/private 过滤 | 谓词测试 1 项失败 |
| 比赛取题键改用 problem_id | 谓词测试 1 项失败 |
| 锁榜查询漏掉时间窗 | 谓词测试 1 项失败 |
| 自由练习绕过私有比赛校验 | 单元测试 1 项失败 |
| 匿名访问私有比赛放行 | 单元测试 2 项失败 |
| 比赛模式提交链接漏掉 langmask | 单元测试 1 项失败 |
| NOIP 锁榜不隐藏统计 | 单元测试 1 项失败 |

---

## 6. 发布流程

### 6.1 前提

- `bash extra/check_all.sh` 全绿
- 上述两层对拍全绿
- 已确认在**维护窗口**内

### 6.2 备份（不可跳过）

```bash
tar czf /root/backup/web-$(date +%Y%m%d-%H%M%S).tar.gz -C /home/judge/src web
cp /etc/nginx/sites-enabled/0-default /root/backup/nginx-0-default.$(date +%Y%m%d)
```

### 6.3 部署 Go 二进制

```bash
export PATH="$PATH:/root/go/bin"
go build -o /tmp/hustoj-go-web ./cmd
/tmp/hustoj-go-web web --ojhome /home/judge    # 先手工验证再装 systemd
```

systemd 单元需 `EnvironmentFile=/home/judge/etc/judge.conf`，
并加 `HUSTOJ_WEB_LISTEN=127.0.0.1:8080`（只绑回环，由 nginx 反代）。

### 6.4 同批上线项

以下必须与 Go 路由**同批**发布，否则两端行为不一致：

- **CSRF**：`/home/judge/src/web` 当前**没有** `$OJ_CSRF`，且缺
  `csrf_token.php`、`csrf_check_ajax.php`、`csrf_check_bootstrap.php`。
  仓库树已开启（`static $OJ_CSRF=true`）但未上线。
  线上实测无 token 的 POST 目前仍返回 200。
- **Cangjie 掩码**：`$OJ_LANGMASK=33356816`（`= 16579600 | (1<<24)`，新增屏蔽 id 24）。
  可见语言：`0 1 2 3 5 6 7 8 9 16 17 25`，Rust 25 保留。
- **collation**：PHP 四处 `SET NAMES utf8mb4_general_ci` 与 Go `repository.DSN()`。

### 6.5 切流

按路径逐个切，**先切只读路径**：

```nginx
location = /problemset.php { proxy_pass http://127.0.0.1:8080; }
location = /problem.php    { proxy_pass http://127.0.0.1:8080; }
```

切单个路径后立即比对两侧输出；出现差异就摘掉该 location 回退 PHP。

### 6.6 回滚

```bash
# 1. 恢复 nginx（最快，无需重启 Go）
cp /root/backup/nginx-0-default.<日期> /etc/nginx/sites-enabled/0-default
nginx -t && systemctl reload nginx

# 2. 必要时停掉 Go 服务
systemctl stop hustoj-go-web

# 3. 必要时恢复 PHP 部署树
tar xzf /root/backup/web-<时间戳>.tar.gz -C /home/judge/src
```

回滚只需 reload nginx，不需要重启 PHP-FPM（PHP 进程未受影响）。

---

## 7. 后续工作

1. **补齐后台路径** —— 目前仅 `admin/problem_batch_delete.php` 已迁移，
   其余 `admin/*` 页面（题目/比赛/用户管理）仍在 PHP 侧。
2. **MyISAM → InnoDB** —— 21 张表是 MyISAM（`online` 是 MEMORY，
   `solution_ai_answer` 是 InnoDB）。转换前必须在 MariaDB 11.8.6 影子环境验证
   锁、deadlock 与 judged-go 取件一致性。当前无影子环境，不要动。
3. **`admin/problem_batch_delete.php` 非原子** —— 涉及多表与文件删除回滚。
   Go 侧已事务化，但 PHP 侧仍是旧的逐条删除。
4. **数据库用户缺 `CREATE` 权限** —— 无法建影子库做数据级对拍。
