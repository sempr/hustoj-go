# 数据库迁移工作流文档

## 概览

本项目采用 **Go 生态工具链** 管理数据库结构变更，核心组件：

| 工具 | 角色 | 备注 |
|------|------|------|
| **goose** | 迁移执行器 | 版本管理、Up/Down 执行、支持 SQL 与 Go 迁移 |
| **sqlc** | 类型安全查询生成 | `schema.sql` + `queries/` → Go 代码，编译期发现 SQL 错误 |
| **Atlas** (可选) | 自动生成差异迁移 | `atlas migrate diff` 对比 `schema.sql` 与开发库生成草稿 |
| **migrate.sh** | 生产部署兜底 | 纯 bash，运维无需 Go 环境，复用同一 `schema_migrations` 表 |

---

## 目录结构

```
gojudge/
├── schema/
│   ├── schema.sql          # 权威表结构定义（手工维护或 mysqldump 导出）
│   ├── triggers.sql        # 触发器定义
│   ├── seed.sql            # 种子数据
│   └── queries/            # sqlc 业务查询文件
├── migrations/             # 版本化迁移脚本（Git 管理）
│   ├── _template.sql       # 迁移模板（Up/Down 规范）
│   ├── 00001_baseline.sql
│   ├── 00002_legacy_to_schema.sql
│   ├── 00003_triggers.sql
│   └── 20250115103000_xxx.sql  # 新增：时间戳命名避免冲突
├── internal/db/
│   ├── sqlc/               # sqlc 生成代码（Git 提交，避免 CI 差异）
│   │   ├── querier.go
│   │   ├── models.go
│   │   └── ...
│   └── migration/          # Go 复杂数据迁移
│       ├── coin_history.go
│       └── register.go
├── goose.yaml              # goose 配置
├── sqlc.yaml               # sqlc 配置
├── Makefile                # 统一入口：make migrate-*
└── extra/
    ├── migrate.sh          # 生产部署脚本（保留）
    ├── check_schema.py     # CI 列级校验（门禁）
    └── schema_diff.py      # 历史工具，逐步废弃
```

---

## 核心约定（必须遵守）

1. **`schema/schema.sql` 是唯一权威结构定义** —— 所有工具以此为准
2. **迁移脚本一旦提交不可修改**（校验和机制保证）—— 改动必须新建迁移
3. **自动生成的迁移必须人工审阅** —— 尤其大表 ALTER、索引顺序、数据迁移
4. **sqlc 生成代码纳入版本控制** —— 避免 CI 环境差异，保证类型安全
5. **保留 `migrate.sh` 作为生产部署兜底** —— 运维无需安装 Go 工具链
6. **迁移命名用时间戳**：`YYYYMMDDHHMMSS_描述.sql`，避免分支冲突

---

## 日常开发流程

### 场景 A：加字段 / 加索引 / 改字段类型（最常见）

```bash
# 1. 修改权威结构
vim schema/schema.sql

# 2. 本地有开发库的前提下，自动生成差异迁移草稿
make migrate-gen
# → 生成 migrations/20250115103000_auto.sql

# 3. ⚠️ 必须人工审阅生成的 SQL！
vim migrations/20250115103000_auto.sql
#   - 确认大表 ALTER 是否需分批/低峰
#   - 确认索引顺序、字段默认值、注释
#   - 必要时手工改写为幂等形式（ADD IF NOT EXISTS）

# 4. 重新生成 sqlc 代码（同步模型）
make sqlc-gen

# 5. 本地验证迁移可应用
make migrate-up

# 6. 提交：迁移脚本 + sqlc 生成代码 + schema.sql 变更
git add migrations/20250115103000_xxx.sql internal/db/sqlc/ schema/schema.sql
git commit -m "ADD: problem 表新增 tags 字段及索引"

# 7. CI 自动跑 make migrate-check（列级校验 + 迁移干运行）
# 8. 部署：生产用 extra/migrate.sh 或 make migrate-up
```

### 场景 B：复杂数据迁移（需 Go 代码逻辑）

```bash
# 1. 创建 Go 迁移文件（参考 internal/db/migration/coin_history.go）
vim internal/db/migration/my_data_fix.go

# 2. 在 register.go 中确保 init() 注册了 Up/Down 函数

# 3. 同步创建对应的 SQL 迁移占位文件（版本号必须一致）
make migrate-create name=fix_user_coin_history
# → 编辑生成的文件，Up 部分留空（由 Go 迁移处理），Down 标记不可回滚

# 4. 本地验证
make migrate-up

# 5. 提交 Go 文件 + SQL 占位文件
```

### 场景 C：新增业务查询（sqlc 驱动）

```bash
# 1. 在 schema/queries/ 下新增 .sql 文件
# 2. make sqlc-gen
# 3. 在 Go 代码中使用生成的类型安全查询方法
# 4. 提交 queries/ + sqlc 生成代码
```

---

## 命令速查表

| 命令 | 作用 |
|------|------|
| `make migrate-up` | 执行所有待应用迁移 |
| `make migrate-down` | 回滚最后一条（需 Down 语句） |
| `make migrate-status` | 查看已应用/待应用版本 |
| `make migrate-create name=xxx` | 创建新 SQL 迁移文件 |
| `make migrate-gen` | **自动生成差异迁移**（需 atlas） |
| `make sqlc-gen` | 重新生成 sqlc 代码 |
| `make migrate-check` | **CI 门禁**：列级校验 + 干运行 |
| `make migrate-baseline` | 把已是新结构的库接入版本管理 |
| `make migrate-dry` | 干运行：只打印将执行的迁移 |

---

## CI/CD 集成

### GitHub Actions (`.github/workflows/ci.yml`)

核心门禁 Job：`migrate-check`
1. 启动 MariaDB 服务容器
2. 用 `schema.sql` 初始化库
3. `goose -baseline up` 登记基线（模拟生产库已是新结构）
4. 运行 `make migrate-check`：
   - `python3 extra/check_schema.py --ignore-comments` —— 列级精确校验
   - `goose -dry-run up` —— 验证迁移序列可应用
5. 验证 `sqlc` 生成代码可编译

### PR 自动生成迁移预览

- `migrate-preview` Job 对比 `schema.sql` 变更，生成迁移草稿上传为 Artifact
- Reviewer 下载审阅，确认无误后合并

---

## 生产部署

### 方式 1：运维用 `extra/migrate.sh`（推荐，无 Go 依赖）

```bash
# 备份
mysqldump --single-transaction --routines --triggers hustoj > backup-$(date +%F).sql

# 迁移（凭据取自 judge.conf）
sudo extra/migrate.sh --ojhome /home/judge --check

# 列级复核
DB_NAME=hustoj python3 extra/check_schema.py --ignore-comments
```

### 方式 2：用 `goose`（需 Go 环境）

```bash
goose -dir migrations mysql "user:pass@tcp(host:3306)/db?parseTime=true" up
```

### 方式 3：应用启动时自动迁移（内嵌）

```go
// main.go 或 daemon 启动时
import "github.com/sempr/hustoj-go/internal/db/migration"

func initDB() {
    db, _ := sql.Open("mysql", dsn)
    migration.RegisterGoMigrations()
    if err := migration.RunGooseUp(db, "migrations"); err != nil {
        log.Fatal("迁移失败:", err)
    }
}
```

---

## 回滚策略

| 场景 | 处理方式 |
|------|----------|
| SQL 迁移有 Down 语句 | `make migrate-down` 或 `goose down` |
| 无 Down / 数据已变更 | **恢复 mysqldump 备份**（标准流程） |
| Go 迁移标记不可回滚 | 文档记录原因，仅支持备份恢复 |

> **原则**：不依赖 Down 回滚，所有变更前必须备份。

---

## 常见问题 FAQ

### Q: 迁移文件冲突了怎么办？
A: 使用时间戳命名（`20250115103000_xxx.sql`）几乎不冲突。若冲突，手工调整时间戳或合并为单文件。

### Q: 大表加索引会锁表怎么办？
A: 
- 生产用 `pt-online-schema-change` 或 `gh-ost` 在线变更
- 迁移脚本中标注 `-- 大表索引，建议用在线工具执行`
- 或分批次在低峰执行

### Q: 如何处理分支间的迁移依赖？
A: 
- 主分支线性历史，迁移按时间顺序应用
- 功能分支合并前 rebase 主分支，必要时合并迁移文件
- CI `migrate-check` 会捕获顺序问题

### Q: sqlc 生成代码冲突怎么解决？
A: 
- `sqlc generate` 生成的代码通常不冲突（按查询文件名分组）
- 冲突时保留双方查询，重新 `make sqlc-gen` 即可

---

## 相关文件参考

- `goose.yaml` — goose 配置
- `sqlc.yaml` — sqlc 配置
- `Makefile` — 统一命令入口
- `migrations/_template.sql` — 迁移模板
- `internal/db/migration/coin_history.go` — Go 迁移示例
- `.github/workflows/ci.yml` — CI 配置
- `extra/migrate.sh` — 生产部署脚本（保留）
- `extra/check_schema.py` — 列级校验工具