# phpweb → Go Web 迁移设计

本文记录把 `/root/gojudge/phpweb`（HUSTOJ PHP 前端）迁移到 Go Web 服务的方案。
迁移采用绞杀者模式：两侧共享同一 MariaDB 与 `/home/judge/data`，nginx 按路径
把流量分给 PHP-FPM 或 Go 服务，不存在数据同步问题，可以逐个路径切换并随时回退。

---

## 1. 现状：两个独立的目录

这是迁移中最容易搞错、也最容易造成事故的一点：

| 路径 | 性质 | 说明 |
|---|---|---|
| `/root/gojudge/phpweb` | git 工作树 | 开发用。`phpweb/` 现已纳入 git 跟踪（约 1913 个文件） |
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

### 3.8 评测结果文案对齐 lang（已修正）

`judgeResultLabel`（`handler_status.go`）与结果筛选下拉原先用
「通过 / 错误答案 / 超时」，且 1/2/3 整体错位（1 显示成"编译中"），
连"等待重判"都不出现。现已按 PHP 的 `$judge_result`
（`include/const.inc.php:9`，文案取自 `lang/cn.php`）统一：

```
0 等待 1 等待重判 2 编译中 3 运行并评判 4 正确 5 格式错误 6 答案错误
7 时间超限 8 内存超限 9 输出超限 10 运行错误 11 编译错误
```

下拉选项顺序也与 `status.php` 一致（先 4..11，再 0..3；12..15 不列）。
12..15 保留 Go 判题器自己的语义（受限函数/非零退出/信号终止/内部错误），
因为 PHP 那份（编译成功/运行完成/人工确认/提交中）与本判题器不一致。

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
- **首页/门户/新闻详情**：`index.php`、`portal.php`、`viewnews.php`
  （`handler_homepage.go`，路由见 `registerHomepageRoutes`）。根路径 `/` 与 `index.php`
  同源渲染（PHP 的 nginx `index` 指令内部走 `index.php`，URL 保持 `/`），不再返回占位页。
  `handleIndex` 内含 `OnSiteContestID` 重定向，新闻列表/本月之星/最近题目/最近比赛/
  图表数据均对齐 PHP。此前三个 handler 已实现且有单测，却一直没有注册路由
  （页面停滞在根占位分支）——已补 `registerHomepageRoutes`，并加 mux 级回归测试
  `handler_homepage_routes_test.go` 钉住，避免再次"实现未接线"。
- **后台**：`admin/problem_batch_delete.php`（事务化重写）；公告管理
  `admin/news_list.php`、`news_add_page.php`、`news_add.php`、`news_edit.php`、
  `news_df_change.php`（`handler_admin_news.go`，仅 administrator 可用）。
  列表 25 条/页、搜索是裸 LIKE 且带关键词时**不加 LIMIT**（全量返回）——与 PHP 一致。
  CSRF 走 nosurf（表单嵌 csrf_token），不复刻 PHP 的 set_post_key/getkey。
  `setmsg.php` 依赖请求 Host 与 RemoveXSS，仍由 PHP 承担，未迁移。
- **后台用户/权限（自包含部分）**：`admin/privilege_list.php`、`privilege_add.php`、
  `privilege_delete.php`、`changepass.php`、`user_df_change.php`、`user_set_ip.php`
  （`handler_admin_user.go`）。要点：
  - `privilege_list` 只列可管理白名单内的 rightstr（与 PHP `$privileges` 逐字一致），
    搜索是 `user_id/rightstr LIKE`；`privilege_delete` 拒绝移除自己的 administrator。
  - `changepass` 的 UPDATE 带 `user_id NOT IN (SELECT ... administrator)`，返回行数判断是否命中。
  - `user_df_change` 拒绝封禁自己；`user_set_ip` 写 `loginlog`（password=`set ip by <管理员>`）。
  - 角色：`privilege_*`/`user_df_change` 仅 administrator；`user_set_ip` 用
    `hasAdminPrivilege`（PHP 只由 admin-header 把关）。**已知限制**：`Identity` 目前不建模
    `user_adder`/`password_setter`（见 `session_redis.go` 的 `GoPrivileges`），故 `changepass`
    暂按仅 administrator 处理，与 `adminMenuPage` 里 `IsPasswordSetter` 恒为 false 的现状一致。
  - 未迁移：`setmsg.php`（文件 `msg/<host>.txt` 位于 PHP web root 下，且依赖 `RemoveXSS`）。
- **后台批量建号 / 导入 / 导出（`admin/user_add.php`、`user_import.php`、`ranklist_export.php`）**：
  - `user_add`：每行 `userid password`（PHP 用 `explode(" ", trim())`，**连续空格会算格式错误**），
    非 ASCII 字节替换成空格（中文用户名会被打散）；已存在报错；建号后写一条 loginlog。
    到期日按 `OJ_EXPIRY_DAYS`（默认 365）在 Go 侧算好传入（config 无此值 → 用默认，需与 PHP 同步）。
  - `user_import`：支持 CSV（UTF-8 / GBK 自动识别，`学号` 表头）与 zip 内多份 CSV；
    昵称超 20 字符时动态 `ALTER TABLE users MODIFY nick`；逐行 upsert。
  - `ranklist_export`：带 UTF-8 BOM 的 CSV（非 XLSX）；NOIP 进行中整页拦截（对管理员也不放行）；
    scope=w 的周起点照抄 PHP 的 `date("d")-(date("w")+7)%8+1`；昵称以 `=+-@` 开头时前置 `'` 防公式注入；
    非 scope 5000/页，scope 分支外层硬编码 LIMIT 50。手写查询见 `pkg/dbgen/ranklist_export_manual.go`。
- **后台用户列表与内联编辑（`admin/user_list.php` + `admin/ajax.php`）**：
  - `user_list.php`：默认 defunct='N'（`?trash` 看 'Y'），按 accesstime DESC，25/页；
    搜索命中 6 列且**不加 LIMIT**、按 user_id DESC；到期日三色（>now+7d 绿 / >=now 蓝 / 否则红）；
    金币余额 = earned+bonus-spent。行内编辑字段渲染为 `span[fd][user_id]`，token 放 `data-csrf`。
  - `admin/ajax.php`：表格内联编辑公共端点。gate = administrator||problem_editor||contest_creator
    （**tag_adder 未建模**，纯 tag_adder 会被拒 —— 已知限制）；各 `*_update_*` 字段再叠加表级角色。
    `uniqueSource`（去重空白）、手机号 `/^1[3-9][0-9]{9}$/` 服务端兜底、`problem_get_title`
    回显（比 PHP 多做了转义）、`get_user_list_of_contest` 回显 `c<id>` 授权用户。
  - 内联 JS 用浏览器原生 `fetch`（layout 不含 jQuery），因此不再依赖 `$.post`。
    未复刻：`parent_phone` 悬浮二维码（依赖未迁移的 `xiaoke.php`/`qrcode.min.js`）、SaaS 子域名邮箱链接。
- **后台题目管理（第一批：`problem_list.php`、`problem_df_change.php`、`problem_del.php`、
  `problem_changeid.php`）**：
  - `problem_list`：计数 `COUNT('problem_id')`**不过滤** problem_id>0/defunct；50/页、每帧 25；
    搜索命中 5 列（`problem_id` 用 `CAST(... AS CHAR) LIKE`）且不加 LIMIT、按 problem_id ASC；
    来源标签复用 problemset 的 `colorTheme`（md5 取模）；金币 `span[fd=coin]` 内联编辑走已迁移的 `ajax.php`。
    权限 administrator||problem_editor||contest_creator。
  - `problem_df_change`：GET 单题切换 + POST 批量（`pid[]` / `hlist`，intval、去重、丢弃非正数；
    enable→'N'，disable→'Y'）；权限 administrator||problem_editor。
  - `problem_del`：删 `<OJHome>/data/<id>` 目录 + `problem`/`privilege(p<id>)` 并在 `solution` 里
    置 problem_id=0/result=13，再重置 AUTO_INCREMENT（≥1000）；仅 administrator。
  - `problem_changeid`：先挪数据目录再改库（失败回滚目录），并同步 solution/contest_problem/topic；
    仅 administrator。`dataDirWritable` 对应 PHP 的 `writable($OJ_DATA)`。
  - **第二批（1.3b）**：`problem_add_page.php`/`problem_add.php`、`problem_edit.php`。
    `addproblem` 以 defunct='Y' 落库（新题先隐藏），随后写 `coin`、建 `<OJHome>/data/<pid>/`
    并写 sample/test 数据文件、可选挂到未来比赛、给创建者 `p<pid>` 权限。
    `problem_add` 仅对 title 做 `,→&#44;`（复刻 PHP）；description/input/output/hint 改由
    Markdown 编辑器原样存取，不再做 HTML 转义；对 sample/test 做 `normalizeSpaces`
    （仅 Unicode 空格 + 零宽字符，ASCII 空格不动）。编辑页 UPDATE 带
    `in_date=NOW()`；仅当 `sample.in` 已存在且新样例非空时才重写样例文件。
    表单用轻量 Markdown 编辑器（源码 + `/admin/markdown_preview.php` 实时预览 + 上传按钮），
    取代已停更多年的 KindEditor。
  - **第三批（1.3c）**：`problem_export.php`/`problem_export_xml.php` —— 产物是 FPS XML
    （可选 zip 打包），含题面字段 + 测试用例 + SPJ 源码。**有意简化**：不复刻 PHP 的
    临时文件 + session 进度 + `?last_file` 二次下载，改为一次性生成并流式返回；不嵌入图片
    base64、不导出 solution 与 prepend/template/append（依赖语言扩展名表）。`problem_copy.php`
    为静态入口，表单 POST 到各导入适配器（仍在 PHP 侧）。
  - `problem_judge.php` **不迁移**：上游第 2 行即 `exit(1)` 停用，且 judged-go 不走 HTTP 判题
    （`internal/daemon/fetcher.go` 的 HTTP 分支未实现）。
  - 未迁移（后续）：各导入适配器（`problem_import_*`/`problem_add_page_*`）。
- **后台系统页（第一批：`rejudge.php`、`solution_statistics.php`）**：
  - `rejudge`：把 `solution.result` 置 1、`pass_rate=0`（judged-go 轮询 `result<2` 自动取件，
    因此**不复刻** PHP 的 Redis lpush / `trigger_judge` UDP）。四种入口：题目、单提交、
    按结果手动改（`result→to`）、比赛（整场或单个 num）。仅 administrator。
  - `solution_statistics`：取回 `(user, problem, min(result))` 明细后在 Go 侧透视
    （PHP 用动态 `case` 列）；比赛模式按 `contest_id`，否则按 `ulist` 用户列表；
    AC/WA/空白三态。administrator 或 contest_creator。
  - **仍未迁**（各有依赖，暂不迁）：`settings.php`（改写 `db_info.inc.php` 配置）、
    `backup.php`（DB dump + 打包全部数据）、`watch.php`（监视）、`phpfm.php`（文件管理器）、
    `update_db.php`（库结构升级，且写死 MyISAM，勿在生产运行）、`aidba.php`。
    `admin/ajax.php` 已迁；`admin/check.php`/`msg.txt`/`help.php` 中 help 已迁、check 为旧 frameset 壳。
- **后台比赛管理（`contest_list.php`、`contest_df_change.php`、`contest_pr_change.php`、
  `contest_add.php`、`contest_edit.php`）**：
  - 权限 administrator 或 contest_creator（Identity 未建模 session 里的 `m<cid>`）。
  - `contest_list`：计数不过滤、25/页、每帧 5；搜索 `title/description LIKE` 不加 LIMIT、
    按 contest_id DESC；行内给出 公开/状态 切换与 编辑/复制/导出/日志/查重 链接。
  - `contest_df_change`/`contest_pr_change`：按 cid 切换 `defunct`/`private`。
  - `contest_add`/`contest_edit`：langmask 由 Go 侧算 `((1<<len(languageExt))-1) & ^selected`；
    `contest_type` 是复选框 `1<<i` 的按位或；`description` 由 Markdown 编辑器原样存取，
    不再做 `<p>` 去除、`</p>→<br />`、`,`→`&#44;` 等 HTML 预处理；
    题号列表逐题校验存在性后写 `contest_problem`（编辑还会重算 `c_accepted/c_submit` 与
    `solution.num`）；写 `c<cid>`（受邀用户）与 `m<cid>`（创建者，仅 add）。依赖 session 的
    "从比赛复制用户"下拉未复刻。
  - 未迁移（后续）：`team_generate*.php`、`offline_import.php`、
    `problem_import_*`/`problem_add_page_*`、`settings.php`、`backup.php`、`watch.php`、
    `phpfm.php`、`update_db.php`、`aidba.php`、`setmsg.php`。
- **后台零散页（`help.php`、`suspect_list.php`）**：
  - `help.php`：后台落地页（admin frameset 的 main frame 正指向它）。显示平均判题延迟
    （最近 10 条 `result>=4` 的 `avg(judgetime-in_date)`）、负载、内存/磁盘、题目/用户/提交计数，
    以及各功能入口（按角色显示）。**用原生读 `/proc/loadavg`、`/proc/meminfo` 与 `unix.Statfs`
    替代 PHP 的 `system("free/df/top")`**；系统状态 iframe 仍指向 `watch.php`（未迁移，混合模式可用）。
  - `suspect_list.php`：查重 —— ①同一 IP 被多个用户使用（`solution` 按 IP 聚合）；②同一用户在
    比赛时段内用多个 IP 登录（`loginlog` 按用户聚合）。权限同 admin-header 角色集合
    （PHP 里该权限检查被注释掉，这里用 `hasAdminPrivilege`）。
  - **不再迁**：`check.php`（旧 frameset 壳，已被 `admin/index.php`+`menu.php` 取代，无独立价值）。
- **系统负荷监控（`watch.php`）**：只读，从 `/proc/stat`（两次采样求 CPU%）、`/proc/meminfo`、
  `/proc/net/sockstat`、`unix.Statfs("/")` 取指标，历史点写入 `/dev/shm/watchlog`（JSON，保留 1 周、
  每分钟一点）；`?json` 返回 flot 的四组曲线。用原生 `/proc` 采样**替代 PHP 的 `top -bn1`/exec**；
  前端图表沿用 PHP 的 jQuery + flot 静态资源。
- **不建议迁到 Go（有意保留在 PHP，或直接停用）**：
  - `settings.php`：编辑的是 PHP 的 `include/db_info.inc.php`。Go 侧配置已改为 `web.toml`/环境变量
    （铁律"不解析 db_info.inc.php"），由 Go 改写该文件会**违背迁移模型**——页面级设置应走
    `web.toml`，不属于本迁移范围。
  - `backup.php`：需要 `mysqldump` 式的整库导出 + 打包 `data/` 与**PHP web root 下的 `upload/`**。
    Go 侧没有 web root 配置，只备份 `data/` 会**静默漏掉上传资源**；备份工具一旦不完整比不做更危险。
    保留在 PHP（其 web root 依赖天然满足），或后续用外部 `mysqldump` 脚本替代。
  - `update_db.php`：旧版结构升级脚本，`CREATE TABLE` 写死 `ENGINE=MyISAM`，**在生产运行有风险**，
    建议废弃（见 §7 第 3 条）。
  - `aidba.php`：依赖外部 AI 服务，非 OJ 核心。
  - `phpfm.php`：在线文件管理器，攻击面大，价值有限。
  - 远程导入适配器（`problem_import_*`/`problem_add_page_*`、`problem_copy.php` 的目标）：依赖对
    第三方 OJ 的抓取/解析，工作量大、易失效；`problem_copy.php` 入口已迁，指向的适配器仍在 PHP。

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

> 逐路径切流的完整路径清单与回滚步骤见 [`web-cutover-checklist.md`](./web-cutover-checklist.md)。

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
- **编辑器实时预览路由**：新增的 `/admin/markdown_preview.php` 必须加进 `oj.uod.edu.kg`
  的 nginx `/admin/` Go location 正则（`/assets/editor/upload.php` 已在 `^~ /assets/` 内），
  否则预览请求落到 PHP-FPM 得到 404。

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

> 已完成（Phase 0）：首页/门户/新闻详情接线（`index.php`/`portal.php`/`viewnews.php`，
> 含 mux 级回归测试），根路径 `/` 不再是占位页。
> 已完成（Phase 1.1）：后台公告管理（`admin/news_*.php`）。
> 已完成（Phase 1.2a）：后台用户/权限自包含页面（`admin/privilege_*`、`changepass`、
> `user_df_change`、`user_set_ip`）。
> 已完成（Phase 1.2b）：`admin/user_list.php` + 内联编辑端点 `admin/ajax.php`。
> 已完成（Phase 1.2c）：`admin/user_add.php`、`user_import.php`、`ranklist_export.php`。
> 已完成（Phase 1.3a）：`admin/problem_list.php`、`problem_df_change.php`、
> `problem_del.php`、`problem_changeid.php`。
> 已完成（Phase 1.3b）：`admin/problem_add_page.php`、`problem_add.php`、`problem_edit.php`。
> 已完成（Phase 1.3c）：`admin/problem_export.php`、`problem_export_xml.php`、`problem_copy.php`
> （`problem_judge.php` 上游停用，不迁移）。
> 已完成（Phase 1.4a）：`admin/rejudge.php`、`solution_statistics.php`。
> 已完成（Phase 2）：判题侧 `custominput`/test_run 支持 —— 测试运行进队列（`result=0`、
> `problem_id=0`），judged-go 检测到 `custominput` 行后走"只跑自定义输入、不比对"路径，
> 程序 stdout 写入 `runtimeinfo`（status-ajax 的 `tr` 分支展示），结果计入 `solution`、
> 不计入任何 `problem_id>0` 统计。HTTP fetcher / `trigger_judge` 有意不实现。
> 已完成（Phase 1.5）：后台比赛管理（`admin/contest_list.php`、`contest_df_change.php`、
> `contest_pr_change.php`、`contest_add.php`、`contest_edit.php`）。
> 已完成（Phase 3）：核实生产库引擎 —— 22 张 InnoDB + 1 张 MEMORY（`online`），
> **MyISAM→InnoDB 已落地，无需迁移**；唯 `admin/update_db.php` 仍写死 MyISAM（勿在生产运行）。
> 已完成（Phase 1.6）：后台零散页 `admin/help.php`、`suspect_list.php`。
> 已完成（Phase 1.7）：系统负荷监控 `admin/watch.php`（原生 /proc 采样替代 exec）。
> 有意保留在 PHP / 停用：`settings.php`（改写 PHP 配置，违背 Go 配置模型）、`backup.php`
> （web root 依赖，只备份 data/ 会漏 upload/）、`update_db.php`（旧结构升级+写死 MyISAM）、
> `aidba.php`、`phpfm.php`、远程导入适配器；`check.php` 为死页不再迁。
> 后台迁移至此收口。

1. **补齐后台用户管理** —— 已迁 `user_list`/`ajax`/`privilege_*`/`changepass`/
   `user_df_change`/`user_set_ip`/`user_add`/`user_import`/`ranklist_export`。仅剩
   `setmsg.php` 未迁（依赖 PHP web root 下的 msg/ 与 RemoveXSS）。
   再往后是题目/比赛管理（`problem_list` 等用的 `ajax.php` 已迁移）。
2. **判题侧** —— `custominput`/test_run 已支持（见下）；HTTP fetcher 与
   远程评测 `trigger_judge` **有意不实现**：`admin/problem_judge.php` 上游停用，
   judged-go 走数据库轮询（`result<2` 取件），无需 UDP/HTTP 触发。
3. **MyISAM → InnoDB（已落地 / 无需迁移）** —— 本文原述"21 张表是 MyISAM
   （`online` 是 MEMORY、`solution_ai_answer` 是 InnoDB）"。**实测作废**：直接查生产库
   （MariaDB 11.8.6）`information_schema.TABLES`，结果为
   **22 张 InnoDB + 1 张 MEMORY（`online`，按设计保留）**，**没有任何 MyISAM 表**。
   `schema/schema.sql` 声明与 `check_schema.py` 的引擎一致性检查一致（`online`=MEMORY，其余 InnoDB）。
   结论：引擎转换**已经完成，不需要 `00002` 迁移**；原"先在影子环境验证锁 / deadlock /
   judged-go 取件一致性"的预研前提随之不成立（表已是事务型引擎）。
   - **遗留隐患（待处理）**：`phpweb/admin/update_db.php`（PHP 旧版结构升级脚本）里的
     `CREATE TABLE` 仍写死 `ENGINE=MyISAM`；**若在生产运行会把表退回 MyISAM**。该脚本不在
     Go 迁移范围内，建议标记废弃或改写为 InnoDB，切勿在生产执行。
   - 无关的陈旧注释：`schema.sql` 的 `openai_task_queue` 表注释仍是"异步任务队列-MyISAM版"
     （引擎实为 InnoDB）。因 `check_schema.py` 会比对表注释、生产库同样带该注释，故不改动。
4. **`admin/problem_batch_delete.php` 非原子** —— 涉及多表与文件删除回滚。
   Go 侧已事务化，但 PHP 侧仍是旧的逐条删除。
5. **数据库用户缺 `CREATE` 权限** —— 无法建影子库做数据级对拍。
