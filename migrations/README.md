# 数据库迁移

把**存量库**（结构不同、数据要保留）逐步对齐到 `schema/schema.sql`。
全新库不用这里，走 `extra/init_db.sh`。

## 约定

- 文件名 `NNNNN_描述.sql`，数字即版本号，只增不改。
- **只向前**：不写回滚脚本；回滚靠迁移前备份（`mysqldump`）。
- 每个脚本用 MariaDB 幂等写法（`ADD/DROP ... IF [NOT] EXISTS`、`MODIFY`），
  可中断、可重跑；**不要写 `DROP TABLE` / `DROP DATABASE`**。
- 环境要求 **MariaDB 10.1+**（`ADD INDEX/PRIMARY KEY [IF NOT EXISTS]` 等语法；
  MySQL 不支持这些写法，MySQL 用户需手工改写迁移脚本）。
- 大表（`solution` / `source_code`）的引擎转换会重建并加锁，低峰执行。
- 改动 `schema/schema.sql` 后，可用 `extra/schema_diff.py` 生成差异脚本，
  人工审阅后按新编号落入本目录。

## 版本台账

运行器 `extra/migrate.sh` 在目标库建 `schema_migrations(version, name,
checksum, applied_at, execution_ms)`，按序应用未登记的脚本，并校验已应用
脚本的 sha256（文件被改动会报错，防止"同一个编号语义漂移"）。

`extra/check_schema.py` 会忽略 `schema_migrations`（它是应用基础设施，
不属于业务结构）。

## 存量库接入

```bash
# 1) 备份
mysqldump --single-transaction --routines --triggers hustoj > backup-$(date +%F).sql
# 2) 迁移（凭据取自 judge.conf；也可 --host/--user/--password/--db 直给）
sudo extra/migrate.sh --ojhome /home/judge --check
# 3) 列级复核（仓库/CI 侧）
DB_NAME=hustoj ... python3 extra/check_schema.py --ignore-comments
```

若目标库**已经是新结构、只是没有版本表**，用 `--baseline` 登记而不执行：

```bash
sudo extra/migrate.sh --ojhome /home/judge --baseline
```

## 脚本清单

| 版本 | 作用 |
| --- | --- |
| 00001 | goose 基线占位（本库早于 goose 存在，仅登记版本） |
| 00002 | 存量结构对齐：引擎 MyISAM→InnoDB、排序规则→utf8mb4_general_ci、补索引、修列定义/顺序 |
| 00003 | 触发器 firstAC / simfilter（与 `schema/triggers.sql` 逐字一致，CI 校验） |
