# hustoj-go 离线部署包

在**构建机**上交叉编译并打包，得到一个可直接部署的 tar 包。**目标机只需
Bash 与 systemd**，不需要 Go 工具链、GCC 或任何编译器。

## 一、构建机：产出包

```bash
make dist                 # linux/amd64 + linux/arm64
make dist-amd64           # 只要 amd64
make dist-arm64           # 只要 arm64
```

等价于：

```bash
extra/package.sh [目标...] [--out DIR] [--version STR] [--no-tini] [--no-fetch-tini]
# 目标写作 <goos>/<goarch>，也可只写 <goarch>（默认 linux）
```

产物（默认在 `dist/`）：

```
dist/hustoj-go-<version>-linux-amd64.tar.gz
dist/hustoj-go-<version>-linux-amd64.tar.gz.sha256
```

二进制用 `CGO_ENABLED=0` 交叉编译；`tini` 优先用本机/交叉 GCC 从仓库源码
编译（离线可复现），失败时回退到 tini 官方 release 的静态二进制。

包内容：

```
hustoj-go-<version>-linux-<arch>/
├── bin/hustoj                 # 唯一主二进制（web/daemon/client/judge/run/version）
├── bin/tini                   # 判题沙箱用（安装到 /usr/local/bin/tini）
├── systemd/hustoj-web.service
├── systemd/judged-go.service
├── etc/langs/all.toml         # 语言注册表（全局）
├── etc/web.toml.example       # Web 运行配置样例（不含凭据）
├── etc/judge.conf.example     # DB 凭据样例（需自行填写）
├── langs/                      # 语言运行时构建输入（Dockerfile + 脚本 + lang.toml 模板）
├── schema/schema.sql           # 表结构（23 张表）
├── schema/triggers.sql         # 触发器 firstAC / simfilter（新库必装）
├── schema/seed.sql             # 种子：管理员账号 + administrator 权限
├── install.sh / uninstall.sh / init_db.sh
├── VERSION / SHA256SUMS / README.md / LICENSE
```

## 二、目标机：部署

前置：MariaDB（沿用既有 HUSTOJ 库）、可选 Redis、Docker（仅判题语言运行时
需要）、`www-data` 用户。

```bash
tar -xzf hustoj-go-<version>-linux-amd64.tar.gz
cd hustoj-go-<version>-linux-amd64
sha256sum -c *.tar.gz.sha256   # 若已下载校验文件
sudo ./install.sh              # 默认装到 /usr/bin + /home/judge
```

`install.sh` 会：校验包 → 安装 `hustoj` 与 `tini` → 建目录 → 写入
`all.toml` 与配置样例 → 生成 FAQ 语言缓存 → 安装并启用 systemd unit →
`/healthz` 自检。默认**不覆盖**已有 `web.toml`/`judge.conf`/unit，用
`--force` 可强制覆盖。

常用选项：

```
--prefix /usr/bin        二进制目录
--ojhome /home/judge     运行数据根目录
--web-user www-data      Web 降权用户
--tini-dir /usr/local/bin
--no-web / --no-judge    只部署其中一半
--skip-systemd           只落地文件，不 enable（容器/排查用）
--dry-run                只打印动作
--init-db                部署时初始化数据库（见下一节），需配 --db-password
```

把数据库凭据写入 `judge.conf` 后重启：

```bash
sudoedit /home/judge/etc/judge.conf     # OJ_HOST_NAME / OJ_USER_NAME / OJ_PASSWORD / OJ_DB_NAME
sudo systemctl restart judged-go.service hustoj-web.service
```

（用 `--init-db` 或 `init_db.sh --write-conf` 会自动写入这 5 个键。）

## 三、数据库初始化（全新库）

沿用既有 HUSTOJ 库时跳过本节。**全新部署必须初始化**，两种方式：

**A. 安装时一并做：**
```bash
sudo ./install.sh --init-db --db-password '强口令' \
     [--db-name hustoj --db-user hustoj --db-host 127.0.0.1 --db-port 3306 \
      --db-root-user root --db-socket /run/mysqld/mysqld.sock \
      --db-admin-user admin --db-admin-password '首个管理员口令']
```

**B. 安装后单独做（或已有二进制时）：**
```bash
sudo ./init_db.sh --db hustoj --user hustoj --password '强口令' \
     --ojhome /home/judge --write-conf
```

`init_db.sh` 按顺序：建库（utf8mb4）→ 建/更新应用账号（`localhost` 与
`127.0.0.1` 都建，兼容 `skip-name-resolve`）→ 导入 `schema/schema.sql` →
建触发器 `schema/triggers.sql` → 种子管理员 `schema/seed.sql` → 用应用账号
自检。口令只经 `MYSQL_PWD` 传递，不进命令行。

安全与边界：

- 目标库**已有表时默认拒绝执行**（`schema.sql` 含 `DROP TABLE`，属破坏性）；
  确认覆盖加 `--force`，整库重来加 `--recreate`（`DROP DATABASE`）。
- `--dry-run` 只打印计划、不连接；口令在 dry-run 输出里以 `****` 遮罩。
- 管理员默认 `admin` / `admin`（旧版 MD5 存储，Go 与 PHP 都能登录），
  **首次登录请立刻改密**。
- `schema/triggers.sql` 不能省：Go 只**读** `solution.first_time=1`，不自己写；
  缺 `firstAC` 会让排名/首页/用户页的「首杀」统计恒为空、金币不增长。
- `schema.sql` 里 `solution/problem/contest/news` 的 `AUTO_INCREMENT` 带着
  生产值（11261/1081/1001/1007），新库会从这些号起步，需要从 1 开始可改。

## 四、判题语言运行时（需 Docker）

判题把语言镜像的 overlay 层当 rootfs 挂载，路径与**本机 Docker** 绑定，
因此必须在目标机本地构建，不能随包分发：

```bash
cd langs
sudo ./install_langs.sh --ojhome /home/judge
sudo systemctl restart judged-go.service
```

`install_langs.sh` 会为 `langs/<id>/Dockerfile` 逐个 `docker build`，重写
`etc/langs/<id>.lang.toml` 里的 `base`/`env`，再安装到
`/home/judge/etc/langs/`。各 Dockerfile 默认走清华 apt 镜像，非中国网络
可按注释改回官方源。

## 五、nginx

Web 服务默认只监听 `127.0.0.1:8080`（见 `etc/web.toml` 的 `listen`），需
由 nginx 按路径反代。已迁移路径与回滚步骤见
`docs/web-cutover-checklist.md`。

## 六、卸载

卸载是破坏性操作，默认会在终端里二次确认；非交互环境必须显式 `--yes`：

```bash
sudo ./uninstall.sh            # 交互确认；移除二进制与 unit，保留数据/配置
sudo ./uninstall.sh --purge    # 连 web.toml / judge.conf / all.toml 一起删
sudo ./uninstall.sh --yes      # 跳过确认（脚本/CI 用）
sudo ./uninstall.sh --dry-run  # 只预览，不动手
```

数据目录 `/home/judge/data`、语言 rootfs (`/home/judge/etc/langs/*.lang.toml`)
与 Docker 镜像一律保留。

`--ojhome` / `--prefix` / `--tini-dir` / `--systemd-dir` 均要求**绝对路径**
（脚本会用 sed 改写 unit，路径不能含 `|` 或换行）。
