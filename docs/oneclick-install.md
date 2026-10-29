# 一键部署补充说明（install_hustoj_go.sh）

发布包根目录同时带有 `install.sh` 和 `install_hustoj_go.sh`：

- `install.sh`：只安装 hustoj-go 二进制、systemd 服务和配置文件，不碰系统软件包；
- `install_hustoj_go.sh`：面向新机器的“从零到可访问”脚本，额外负责
  MariaDB/Redis/Docker/nginx、数据库初始化、admin 账号、示例题库、判题语言和
  nginx 反向代理。

**注意**：一键脚本默认把 OJ 根目录设为 `/srv/hustoj`，不是 `install.sh` 的
默认 `/home/judge`。本文命令都按 `/srv/hustoj` 说明。

## 一、在目标机上安装（推荐）

目标机要求：Debian/Ubuntu、systemd、可访问 apt 源，建议全新机器；需要 root。

```bash
tar -xzf hustoj-go-<版本>-linux-amd64.tar.gz
cd hustoj-go-<版本>-linux-amd64

# 先检查一下脚本会做什么（可选）
sudo ./install_hustoj_go.sh --local --help

# 正式安装
sudo ./install_hustoj_go.sh --local
```

脚本位于已解压的发布包内，会自动使用本地模式；也可以不加 `--local` 直接执行。
常用变体：

```bash
# 机器上 MariaDB/Redis/Docker/nginx 已经装好，只跳过 apt 安装
sudo ./install_hustoj_go.sh --local --skip-apt

# 只装部分语言，减少构建时间和磁盘占用
sudo ./install_hustoj_go.sh --local --langs "0 1 6"

# 不创建演示提交（题目和比赛仍会装载）
sudo ./install_hustoj_go.sh --local --no-samples

# 把 OJ 根目录换到别处
sudo ./install_hustoj_go.sh --local --ojhome /opt/hustoj
```

安装完成后：

```text
访问地址：http://<目标机IP>/
管理员：  admin
密码文件：/root/admin.pass.txt   （权限 0600，root:root）
OJ 根目录：/srv/hustoj
Web：     127.0.0.1:8080，由 nginx 80 端口反代（不配 SSL）
服务：    hustoj-web.service / judged-go.service
```

请用 `cat /root/admin.pass.txt` 取密码，首次登录后及时修改。

## 二、从构建机远程安装

在仓库或构建机上执行，脚本会把部署包和自身一起推到目标机：

```bash
./install_hustoj_go.sh \
  --target hustoj@10.80.1.12 \
  --package dist/hustoj-go-<版本>-linux-amd64.tar.gz
```

目标账号需要 SSH 公钥登录，并能无密码 `sudo -n`。不带 `--package` 时，脚本会
在 `dist/` 下找最新的 linux-amd64 包；本机有 Go 工具链且没有现成包时会尝试构建。

## 三、脚本做了什么

1. `apt-get install`：MariaDB、Redis、Docker、nginx、curl、openssl；
2. `systemctl enable --now` 上述服务；
3. 创建 `judge` 用户和 `/srv/hustoj` 目录树；
4. 新库：初始化 `hustoj` 数据库、触发器和 `admin` 管理员；已有库：跳过建表；
5. 安装 `hustoj` 二进制和 systemd 服务；
6. 装载 7 道示例题、1 个示例比赛和多语言演示提交；
7. 用 Docker 在目标机本地构建判题语言 rootfs；
8. 配置 nginx 80 端口反代到 `127.0.0.1:8080`，删除默认站点，不配 SSL；
9. 自检 `/healthz`、首页、题库、状态、比赛、排名页，打印题目/比赛/提交数量。

数据库账号密码随机生成，只写入 `/srv/hustoj/etc/judge.conf`（权限 0600）。
admin 密码首次随机生成；重复执行时沿用 `/root/admin.pass.txt` 并校正数据库口令。

## 四、默认语言

默认安装 12 种语言：

```text
id  语言
0   C
1   C++
2   Pascal
3   Java
5   Bash
6   Python
7   PHP
8   Perl
9   C#
16  TypeScript (JavaScript 运行时)
17  Go
25  Rust
```

`OJ_LANG_SET=0,1,2,3,5,6,7,8,9,16,17,25` 由 `langs/install_langs.sh` 按实际安装
的语言自动写入 `judge.conf`。

语言 rootfs 必须在目标机本地构建（镜像拍平后与机器绑定），不能随包分发。
每种语言约占数百 MB，首次安装需要 Docker 能拉取基础镜像并访问 apt/npm/Go
下载源；网络慢时会明显耗时。已经构建好的语言会被跳过，重复执行不会重建。

## 五、示例题库与演示提交

示例题固定为 `1000`～`1006`，演示比赛 `contest_id=1000`：

| 题号 | 内容 | 演示点 |
| --- | --- | --- |
| 1000 | A+B Problem | 普通评测、多测试点、多语言 |
| 1001 | 子任务与部分分：数列求和 | OI 子任务、部分分、捆绑测试 |
| 1002 | 特殊评测：构造幻方 | SPJ |
| 1003 | 交互题：猜数字 | 交互评测 |
| 1004 | 文件 IO | `sum.in` / `sum.out` |
| 1005 | 通讯题：奇偶子集的双射 | 通讯评测（run-twice toggle n） |
| 1006 | 两次通信：编码与定位 | run-twice 通讯 |

默认会造 44 条演示提交（账号 `admin`）：

- `solutions/` 里有代码的所有语言满分 AC：C、C++、Python、Go、Java、
  Pascal、Bash、PHP、Perl、C#、TypeScript、Rust；
- 1001 的 C/C++/Python 部分分，均 30/100；
- 1001 的捆绑测试反例，0 分；
- 1002 的 SPJ 反例，判 WA；
- 1005 / 1006 的通讯题 AC，以及 1006 的首轮 WA、次轮 WA 反例；
- 1000 的 PE/WA/TL/ML/OL/RE/CE、内存先超、输出先超等判罚演示。

演示提交按「用户 + 题目 + 语言 + 源码」去重，重复执行只补新增项，不会重复插入。
查看结果：

```text
http://<目标机IP>/status.php
```

也可以单独重跑演示数据（在解压后的发布包目录里执行）：

```bash
sudo ./demo/seed_samples.sh --ojhome /srv/hustoj --pid-base 1000 --user admin
```

## 六、重复执行与升级

脚本是幂等的，可以重复执行：

- 已有数据库不会重建；已有表会跳过初始化；
- 已有示例题/比赛会跳过，已有演示提交按源码去重；
- 已存在的语言 rootfs 和 `*.lang.toml` 会跳过构建；
- 已有 `/root/admin.pass.txt` 会沿用；管理员口令会按该文件校正；
- 默认不覆盖已有 `web.toml` / `judge.conf` / systemd unit，除非包内
  `install.sh` 使用 `--force`（一键脚本目前不主动传 `--force`）。

升级二进制时，把新发布包解压后再次执行 `sudo ./install_hustoj_go.sh --local`
即可；数据、题库、提交和语言 rootfs 都会保留。

## 七、安装后检查

```bash
systemctl status hustoj-web.service judged-go.service nginx
curl -fsS http://127.0.0.1:8080/healthz
curl -sS -o /dev/null -w '%{http_code}\n' http://127.0.0.1/status.php
journalctl -u judged-go.service -n 100 --no-pager
sudo cat /srv/hustoj/etc/judge.conf        # 数据库凭据，0600
sudo cat /root/admin.pass.txt              # admin 密码，0600
```

常见问题：

- **判题报 `language rootfs not found`**：`etc/langs/<id>.lang.toml` 的 `base`
  指向了已清理的 Docker overlay 路径。重新执行一键脚本或
  `langs/install_langs.sh`，脚本会自动改成 `/srv/hustoj/var/rootfs/<id>`。
- **某语言缺失**：确认 `etc/langs/<id>.lang.toml` 和 `var/rootfs/<id>` 都存在；
  缺哪个就重新执行 `install_langs.sh`（会构建全部随包语言），或调整 `--langs`。
- **首页 500**：先看 `journalctl -u hustoj-web -n 100`；确认数据库已初始化。
- **80 端口打不开**：确认 nginx 配置指向 `127.0.0.1:8080` 且
  `hustoj-web.service` 正常。

## 八、安全提示

- 脚本只监听 80，不配置 SSL；公网部署请在前面加 HTTPS 终止（Caddy/nginx/云负载均衡）。
- `/root/admin.pass.txt` 和 `/srv/hustoj/etc/judge.conf` 含敏感信息，权限为 0600，
  不要提交到仓库或发到公开渠道。
- 判题会执行用户提交的代码，务必保持 Docker/内核和判题沙箱更新，不要把
  Docker socket 暴露给不可信用户。
