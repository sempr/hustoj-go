# Web 切流清单（PHP → Go）

供运维在 nginx 上按路径把流量从 PHP-FPM 切到 Go 服务（`hustoj web`，默认监听
`127.0.0.1:8080`）。迁移采用**绞杀者模式**：两侧共享同一 MariaDB 与 `/home/judge/data`，
无数据同步；切流只改 nginx，回滚只需 reload nginx。

- 设计/等价性说明：`docs/phpweb-go-refactor-design.md`
- 统一门禁（发布前必须全绿）：`bash extra/check_all.sh`
- 切流**按路径逐个**，先切只读路径，切完立即比对两侧输出。

---

## 1. Go 已接管的路径

> 未列出的路径（及静态资源）仍由 PHP 提供，属正常混合状态，例如
> `admin/settings.php`、`admin/backup.php`、`admin/phpfm.php`（题目数据管理已由 Go 的
> `/admin/test_data.php` 取代，phpfm 的其余功能未迁移）、`admin/setmsg.php`、
> `admin/update_db.php`、`admin/aidba.php`、`admin/team_generate*.php`、
> `admin/offline_import.php`、`admin/problem_import_*.php`、`admin/problem_add_page_*.php`、
> `admin/check.php`。

### 探针

| 方法 | 路径 |
|---|---|
| GET | `/healthz`（存活，不触库） |
| GET | `/readyz`（就绪，Ping DB） |

### 前台 / 用户

| 方法 | 路径 |
|---|---|
| GET | `/`、`/index.php`、`/portal.php`、`/viewnews.php` |
| GET | `/problemset.php`、`/problem.php`、`/category.php` |
| GET/POST | `/contest.php` |
| GET | `/contestrank.php`、`/contestrank-oi.php`、`/contestrank4.php`、`/contestrank5.php`、`/contestrank.xls.php`、`/conteststatistics.php` |
| GET | `/status.php`、`/problemstatus.php`、`/showsource.php` |
| GET | `/submitpage.php`；POST `/submit.php` |
| GET | `/problem-ajax.php`、`/status-ajax.php` |
| GET | `/userinfo.php`、`/ranklist.php` |
| GET | `/online.php`、`/recent-contest.php`、`/export_ac_code.php`、`/export_contest_code.php` |
| GET | `/comparesource.php`、`/getsource.php`、`/showsource2.php`、`/contestrank2.php`、`/contestrank3.php` |
| ANY | `/user_set_ip.php` |
| GET | `/loginpage.php`、`/registerpage.php`、`/logout.php`、`/vcode.php`；POST `/login.php`、`/register.php` |
| ANY | `/mail.php`、`/modify.php`、`/modifypage.php`、`/lostpassword.php`、`/lostpassword2.php`、`/setlang.php`、`/refresh-privilege.php`、`/reinfo.php`、`/active.php` |

### 后台（`/admin/`）

| 方法 | 路径 |
|---|---|
| GET | `/admin/index.php`、`/admin/menu.php`、`/admin/admin-header.php`、`/admin/help.php` |
| ANY | `/admin/problem_batch_delete.php` |
| GET | `/admin/news_list.php`、`/admin/news_add_page.php`、`/admin/news_df_change.php` |
| GET/POST | `/admin/news_edit.php`；POST `/admin/news_add.php`、`/admin/ajax.php` |
| GET | `/admin/user_list.php`、`/admin/user_df_change.php`、`/admin/privilege_list.php` |
| GET/POST | `/admin/user_add.php`、`/admin/user_import.php`、`/admin/user_set_ip.php`、`/admin/privilege_add.php`、`/admin/changepass.php` |
| POST | `/admin/privilege_delete.php` |
| GET | `/admin/problem_list.php`、`/admin/problem_add_page.php`、`/admin/problem_edit.php`、`/admin/problem_export.php`、`/admin/problem_copy.php`、`/admin/problem_del.php` |
| GET/POST | `/admin/problem_df_change.php`、`/admin/problem_changeid.php`、`/admin/problem_add.php`、`/admin/problem_export_xml.php` |
| GET/POST | `/admin/test_data.php`（测试数据管理：列表/上传+zip解压/下载/打包/查看/编辑/新建/重命名/删除） |
| GET/POST | `/admin/rejudge.php`、`/admin/solution_statistics.php`；GET `/admin/ranklist_export.php` |
| GET | `/admin/contest_list.php`、`/admin/contest_df_change.php`、`/admin/contest_pr_change.php`、`/admin/contest_edit.php` |
| GET/POST | `/admin/contest_add.php` |
| GET | `/admin/suspect_list.php`、`/admin/watch.php` |
| POST | `/admin/markdown_preview.php`（后台 Markdown 编辑器实时预览，登录/出题人角色；CSRF 豁免） |

> 注：`/admin/watch.php?json` 供监控图表轮询；`/admin/problem_export.php` 会 302 到
> `/admin/problem_export_xml.php`（后者直接产出下载）。
> 编辑器上传走 `/assets/editor/upload.php`（已在 `^~ /assets/` 反代内，字段 `file`，CSRF 豁免）；
> **切流时须把 `markdown_preview` 加进 nginx 的 `/admin/` Go location 正则**，否则预览会落到
> PHP-FPM 得到 404。

---

## 2. 切流示例（nginx）

```nginx
# 只读路径先行
location = /problemset.php { proxy_pass http://127.0.0.1:8080; }
location = /problem.php    { proxy_pass http://127.0.0.1:8080; }
location = /status.php     { proxy_pass http://127.0.0.1:8080; }
# ... 逐条添加；POST 路径同样用 location = 精确匹配
location = /submit.php     { proxy_pass http://127.0.0.1:8080; }
```

要点：

- 用 `location =`（精确匹配）而不是前缀匹配，避免误伤未迁移路径。
- Go 服务需绑定回环（`web.toml` 的 `listen`，或环境变量），由 nginx 反代；
  TLS 在 nginx 终止，Go 依据 `X-Forwarded-Proto` 判定 CSRF 同源。
- 逐条切换后立即 `curl` 对比两侧（标题、行数、AC 标记、分页等）。

---

## 3. 同批上线项（必须与对应路由一起发布）

- **CSRF**：Go 用 `nosurf`。生产部署树 `/home/judge/src/web` 若未启用 `$OJ_CSRF` 且缺
  `csrf_*.php`，需与 Go 路由同批补齐脚本（见设计文档 §6.4）。
- **语言掩码**：`$OJ_LANGMASK=33356816`（含 Cangjie 屏蔽位）。
- **collation**：PHP 四处 `SET NAMES utf8mb4_general_ci` 与 Go `repository.DSN()`。
- **`$OJ_FREE_PRACTICE` 等开关**：两端各一份（Go 读 `web.toml`），发布时必须同步。

---

## 4. 回滚

```bash
# 摘掉 Go 的 location（恢复 PHP）；无需重启 PHP-FPM
cp /root/backup/nginx-0-default.<日期> /etc/nginx/sites-enabled/0-default
nginx -t && systemctl reload nginx

# 必要时停掉 Go 服务
systemctl stop hustoj-go-web
```

---

## 5. 发布前检查

- [ ] `export PATH="$PATH:/root/go/bin"; bash extra/check_all.sh` 全绿
- [ ] 两层对拍全绿（`HUSTOJ_EQUIV_TEST=1` / `HUSTOJ_HTTP_EQUIV=1`）
- [ ] 已备份 nginx 配置与部署树：`tar czf /root/backup/web-<ts>.tar.gz -C /home/judge/src web`
- [ ] 处于维护窗口内
