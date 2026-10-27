#!/usr/bin/env bash
#
# 存量库结构迁移：把已有 HUSTOJ 库按 migrations/ 里的编号脚本，逐步对齐到
# schema/schema.sql。目标机只需 bash + mysql/mariadb 客户端（与 init_db.sh 一致）。
#
# 与 init_db.sh 的分工：
#   * 全新库（无表）—— 用 init_db.sh（schema.sql 含 DROP TABLE，属另起炉灶）；
#   * 存量库（结构不同、数据要保留）—— 用本脚本（只做 ALTER，不 DROP 数据）。
#
# 迁移由 extra/schema_diff.py 依据「线上 vs schema/schema.sql」的差异生成，
# 全部使用 MariaDB 的幂等写法（IF [NOT] EXISTS / MODIFY），可中断、可重跑。
# 环境要求 MariaDB 10.1+；MySQL 不支持这些语法。
#
# 版本追踪：schema_migrations(version, name, checksum, applied_at, execution_ms)。
# 只向前（forward-only）；回滚靠迁移前的备份，不提供 down 脚本。
#
# 用法：
#   sudo ./migrate.sh --ojhome /home/judge              # 凭据取自 judge.conf
#   sudo ./migrate.sh --db hustoj --user hustoj --password '...' --host 127.0.0.1
#
# 选项：
#   --ojhome DIR      OJ 根目录（默认 /home/judge）
#   --conf FILE       judge.conf 路径（默认 <ojhome>/etc/judge.conf）
#   --host/--port/--db/--user/--password   覆盖 judge.conf 里的连接信息
#   --migrations DIR  迁移目录（默认自动探测 <脚本目录>/migrations 或 ../migrations）
#   --schema FILE     schema.sql 路径（--check 用；默认自动探测）
#   --to VERSION      只迁移到该版本（默认最新）
#   --baseline [V]    不执行 SQL，把版本登记为“已应用”（默认登记到最新）。
#                     用于把“已经是新结构、但没有版本表”的库接入迁移体系。
#   --status          只列出各迁移的应用状态，不做任何改动
#   --check           迁移后核对表集合 / 引擎 / 排序规则（粗粒度，全量列级见 check_schema.py）
#   --force           已应用脚本校验和不一致时仍继续（慎用）
#   --dry-run         只打印将要执行的动作
#   -h, --help        显示帮助
#
# 迁移前请自行备份：mysqldump --single-transaction <db> > backup.sql
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

OJHOME=/home/judge
CONF=""
MIGRATIONS_DIR=""
SCHEMA_FILE=""
DB_HOST=""
DB_PORT=""
DB_NAME=""
DB_USER=""
DB_PASS=""
TO=""
BASELINE=0
BASELINE_TO=""
DO_STATUS=0
DO_CHECK=0
FORCE=0
DRY=0

log()  { printf '==> %s\n' "$*"; }
warn() { printf '警告: %s\n' "$*" >&2; }
die()  { printf '错误: %s\n' "$*" >&2; exit 1; }
usage() { sed -n '2,/^set /p' "$0" | sed -e 's/^# \{0,1\}//' -e '/^set /d'; }

while [ $# -gt 0 ]; do
    case "$1" in
        --ojhome)      OJHOME="$2"; shift 2 ;;
        --conf)        CONF="$2"; shift 2 ;;
        --host)        DB_HOST="$2"; shift 2 ;;
        --port)        DB_PORT="$2"; shift 2 ;;
        --db|--db-name) DB_NAME="$2"; shift 2 ;;
        --user|--db-user) DB_USER="$2"; shift 2 ;;
        --password)    DB_PASS="$2"; shift 2 ;;
        --migrations)  MIGRATIONS_DIR="$2"; shift 2 ;;
        --schema)      SCHEMA_FILE="$2"; shift 2 ;;
        --to)          TO="$2"; shift 2 ;;
        --baseline)
            BASELINE=1; shift
            # 可选版本号：下一个参数是纯数字才算
            if [ $# -gt 0 ] && case "$1" in ''|*[!0-9]*) false ;; *) true ;; esac; then
                BASELINE_TO="$1"; shift
            fi
            ;;
        --status)      DO_STATUS=1; shift ;;
        --check)       DO_CHECK=1; shift ;;
        --force)       FORCE=1; shift ;;
        --dry-run)     DRY=1; shift ;;
        -h|--help)     usage; exit 0 ;;
        *)             die "未知参数: $1（用 --help 查看用法）" ;;
    esac
done

# ── 迁移目录 / schema 文件自动探测 ───────────────────────────────────
if [ -z "$MIGRATIONS_DIR" ]; then
    if [ -d "$SCRIPT_DIR/migrations" ]; then
        MIGRATIONS_DIR="$SCRIPT_DIR/migrations"
    elif [ -d "$SCRIPT_DIR/../migrations" ]; then
        MIGRATIONS_DIR="$(cd "$SCRIPT_DIR/../migrations" && pwd)"
    fi
fi
[ -n "$MIGRATIONS_DIR" ] && [ -d "$MIGRATIONS_DIR" ] || die "找不到 migrations 目录（用 --migrations 指定）"

if [ -z "$SCHEMA_FILE" ]; then
    for c in "$SCRIPT_DIR/schema/schema.sql" "$SCRIPT_DIR/../schema/schema.sql"; do
        [ -f "$c" ] && SCHEMA_FILE="$c" && break
    done
fi

# ── 凭据：命令行优先，否则读 judge.conf ──────────────────────────────
if [ -z "$DB_NAME" ] || [ -z "$DB_USER" ]; then
    [ -n "$CONF" ] || CONF="$OJHOME/etc/judge.conf"
    [ -f "$CONF" ] || die "找不到 $CONF（可用 --conf 指定，或用 --host/--user/--password 直接给）"
    conf_get() { sed -n "s/^[[:space:]]*$1=//p" "$CONF" | head -1 | tr -d '\r'; }
    [ -n "$DB_HOST" ] || DB_HOST="$(conf_get OJ_HOST_NAME)"
    [ -n "$DB_PORT" ] || DB_PORT="$(conf_get OJ_PORT_NUMBER)"
    [ -n "$DB_NAME" ] || DB_NAME="$(conf_get OJ_DB_NAME)"
    [ -n "$DB_USER" ] || DB_USER="$(conf_get OJ_USER_NAME)"
    [ -n "$DB_PASS" ] || DB_PASS="$(conf_get OJ_PASSWORD)"
fi
DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="${DB_PORT:-3306}"
[ -n "$DB_NAME" ] || die "缺少库名（--db 或 judge.conf 的 OJ_DB_NAME）"
[ -n "$DB_USER" ] || die "缺少账号（--user 或 judge.conf 的 OJ_USER_NAME）"

if [ "$DRY" -ne 1 ]; then
    MYSQL_BIN="$(command -v mysql || command -v mariadb || true)"
    [ -n "$MYSQL_BIN" ] || die "未找到 mysql/mariadb 客户端"
else
    MYSQL_BIN="mysql"
fi

sql() { # <sql>  → 逐行输出（-N -B）
    MYSQL_PWD="$DB_PASS" "$MYSQL_BIN" -h "$DB_HOST" -P "$DB_PORT" -u "$DB_USER" \
        -N -B "$DB_NAME" -e "$1"
}

sql_exec() { # <sql>
    if [ "$DRY" -eq 1 ]; then
        printf '  [dry-run] SQL: %s\n' "${1%%$'\n'*}"
        return 0
    fi
    MYSQL_PWD="$DB_PASS" "$MYSQL_BIN" -h "$DB_HOST" -P "$DB_PORT" -u "$DB_USER" \
        "$DB_NAME" -e "$1"
}

# 数字版本：去前导零（避免 bash/test 把 010 当八进制）。空或非数字返回 0。
num() {
    local v="${1:-0}"
    v="${v#"${v%%[!0]*}"}"      # 去掉前导 0
    printf '%s' "${v:-0}"
}

# ── 迁移清单 ─────────────────────────────────────────────────────────
declare -a MIG_FILES=() MIG_VERS=() MIG_NAMES=() MIG_SUMS=()
shopt -s nullglob
for f in "$MIGRATIONS_DIR"/[0-9]*_*.sql; do
    base="$(basename "$f")"
    v="${base%%_*}"
    name="${base#*_}"
    MIG_FILES+=("$f")
    MIG_VERS+=("$(num "$v")")
    MIG_NAMES+=("$name")
    if command -v sha256sum >/dev/null 2>&1; then
        MIG_SUMS+=("$(sha256sum "$f" | awk '{print $1}')")
    else
        MIG_SUMS+=("")
    fi
done
shopt -u nullglob
[ "${#MIG_FILES[@]}" -gt 0 ] || die "$MIGRATIONS_DIR 下没有迁移脚本（形如 00001_xxx.sql）"

max_ver=0
for v in "${MIG_VERS[@]}"; do [ "$v" -gt "$max_ver" ] && max_ver="$v"; done

# 校验 --to
if [ -n "$TO" ]; then
    case "$TO" in *[!0-9]*) die "--to 需要数字版本号: $TO" ;; esac
    TO="$(num "$TO")"
fi
if [ "$BASELINE" -eq 1 ] && [ -z "$BASELINE_TO" ]; then
    BASELINE_TO="$max_ver"
else
    [ -n "$BASELINE_TO" ] && BASELINE_TO="$(num "$BASELINE_TO")"
fi

# ── 建版本表 ─────────────────────────────────────────────────────────
VERSION_DDL="CREATE TABLE IF NOT EXISTS \`schema_migrations\` (
  \`version\` bigint NOT NULL,
  \`name\` varchar(191) NOT NULL,
  \`checksum\` char(64) NOT NULL DEFAULT '',
  \`applied_at\` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,
  \`execution_ms\` int NOT NULL DEFAULT 0,
  PRIMARY KEY (\`version\`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_general_ci"

if [ "$DRY" -eq 1 ]; then
    printf '  [dry-run] 连接 mysql -h %s -P %s -u %s %s\n' "$DB_HOST" "$DB_PORT" "$DB_USER" "$DB_NAME"
    printf '  [dry-run] 确保存在版本表 schema_migrations\n'
else
    if ! sql 'SELECT 1' >/dev/null 2>&1; then
        die "无法以账号 '$DB_USER' 连接 '$DB_NAME'@$DB_HOST:$DB_PORT；检查 judge.conf 凭据"
    fi
    ntab="$(sql "SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA='$DB_NAME' AND TABLE_TYPE='BASE TABLE'")"
    if [ "${ntab:-0}" -eq 0 ]; then
        die "库 '$DB_NAME' 里没有表；全新库请用 init_db.sh，而不是 migrate.sh"
    fi
    sql_exec "$VERSION_DDL"
fi

# 已应用版本 → 校验和
declare -A APPLIED_SUM=()
if [ "$DRY" -ne 1 ]; then
    while IFS=$'\t' read -r v sum; do
        [ -n "${v:-}" ] && APPLIED_SUM["$v"]="$sum"
    done < <(sql "SELECT version, checksum FROM schema_migrations")
fi

# ── --status ─────────────────────────────────────────────────────────
if [ "$DO_STATUS" -eq 1 ]; then
    printf '%-8s %-40s %s\n' 版本 名称 状态
    for i in "${!MIG_FILES[@]}"; do
        v="${MIG_VERS[$i]}"
        if [ -n "${APPLIED_SUM[$v]:-}" ]; then st="已应用"; else st="待应用"; fi
        printf '%-8s %-40s %s\n' "$(printf '%05d' "$v")" "${MIG_NAMES[$i]%.sql}" "$st"
    done
    exit 0
fi

# ── 应用 / 登记 ──────────────────────────────────────────────────────
record() { # <version> <name> <checksum> <ms>
    local v="$1" n="$2" c="$3" ms="$4"
    local sqls="INSERT INTO schema_migrations (version, name, checksum, applied_at, execution_ms)
                VALUES ($v, '${n//\'/\'\'}', '$c', NOW(), $ms)
                ON DUPLICATE KEY UPDATE name=VALUES(name), checksum=VALUES(checksum),
                                        applied_at=VALUES(applied_at), execution_ms=VALUES(execution_ms)"
    sql_exec "$sqls"
}

changed=0
for i in "${!MIG_FILES[@]}"; do
    v="${MIG_VERS[$i]}"; name="${MIG_NAMES[$i]}"; sum="${MIG_SUMS[$i]}"; file="${MIG_FILES[$i]}"
    label="$(printf '%05d' "$v") $name"

    if [ -n "$TO" ] && [ "$v" -gt "$TO" ]; then
        log "跳过 $label（超过 --to $TO）"
        continue
    fi

    if [ -n "${APPLIED_SUM[$v]:-}" ]; then
        if [ -n "$sum" ] && [ -n "${APPLIED_SUM[$v]}" ] && [ "${APPLIED_SUM[$v]}" != "$sum" ] && [ "$FORCE" -ne 1 ]; then
            die "迁移 $label 已应用但文件已改动（校验和不一致）；请新开编号，或用 --force 覆盖登记"
        fi
        log "已应用 $label，跳过"
        continue
    fi

    if [ "$BASELINE" -eq 1 ]; then
        if [ "$v" -le "$BASELINE_TO" ]; then
            log "登记(不执行) $label"
            if [ "$DRY" -ne 1 ]; then record "$v" "$name" "$sum" 0; APPLIED_SUM["$v"]="$sum"; fi
        fi
        continue
    fi

    log "应用 $label"
    changed=1
    if [ "$DRY" -eq 1 ]; then
        printf '  [dry-run] %s < %s\n' "mysql -h $DB_HOST -P $DB_PORT -u $DB_USER $DB_NAME" "$file"
        continue
    fi
    start="$(date +%s%N 2>/dev/null || echo 0)"
    if ! MYSQL_PWD="$DB_PASS" "$MYSQL_BIN" -h "$DB_HOST" -P "$DB_PORT" -u "$DB_USER" \
            "$DB_NAME" < "$file" >/dev/null; then
        die "迁移 $label 执行失败；库可能处于半迁移状态，修正后重跑本脚本（脚本幂等）"
    fi
    end="$(date +%s%N 2>/dev/null || echo 0)"
    ms=0; [ "$start" != 0 ] && ms=$(( (end - start) / 1000000 ))
    record "$v" "$name" "$sum" "$ms"
done

# ── --check：粗粒度结构核对 ──────────────────────────────────────────
if [ "$DO_CHECK" -eq 1 ]; then
    [ -n "$SCHEMA_FILE" ] && [ -f "$SCHEMA_FILE" ] || die "找不到 schema.sql（用 --schema 指定）"
    if [ "$DRY" -eq 1 ]; then
        printf '  [dry-run] 核对表集合 / 引擎 / 排序规则（对照 %s）\n' "$SCHEMA_FILE"
        exit 0
    fi
    log "结构核对（对照 $SCHEMA_FILE）"
    fail=0
    expected="$(grep -oE 'CREATE TABLE `[^`]+`' "$SCHEMA_FILE" | sed -E 's/.*`([^`]+)`.*/\1/' | sort)"
    actual="$(sql "SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA='$DB_NAME' AND TABLE_TYPE='BASE TABLE' AND TABLE_NAME<>'schema_migrations'" | sort)"
    if ! diff <(printf '%s\n' "$expected") <(printf '%s\n' "$actual") >/dev/null; then
        printf '  ✗ 表集合不一致：\n' >&2
        diff <(printf '%s\n' "$expected") <(printf '%s\n' "$actual") | sed 's/^/      /' >&2 || true
        fail=1
    else
        printf '  ✓ 表集合一致（%s 张）\n' "$(printf '%s\n' "$expected" | grep -c .)"
    fi
    bad="$(sql "SELECT GROUP_CONCAT(CONCAT(TABLE_NAME,'=',ENGINE)) FROM information_schema.TABLES
                 WHERE TABLE_SCHEMA='$DB_NAME' AND TABLE_TYPE='BASE TABLE'
                   AND TABLE_NAME<>'online' AND TABLE_NAME<>'schema_migrations'
                   AND ENGINE<>'InnoDB'")"
    if [ -n "${bad:-}" ] && [ "$bad" != "NULL" ]; then
        printf '  ✗ 非 InnoDB 的表: %s\n' "$bad" >&2; fail=1
    else
        printf '  ✓ 引擎均为 InnoDB（online 除外）\n'
    fi
    badcol="$(sql "SELECT GROUP_CONCAT(DISTINCT TABLE_COLLATION) FROM information_schema.TABLES
                    WHERE TABLE_SCHEMA='$DB_NAME' AND TABLE_TYPE='BASE TABLE' AND TABLE_COLLATION<>'utf8mb4_general_ci'")"
    if [ -n "${badcol:-}" ] && [ "$badcol" != "NULL" ]; then
        printf '  ✗ 排序规则不是 utf8mb4_general_ci: %s\n' "$badcol" >&2; fail=1
    else
        printf '  ✓ 排序规则均为 utf8mb4_general_ci\n'
    fi
    [ "$fail" -eq 0 ] || die "结构核对未通过（列级核对见 extra/check_schema.py）"
fi

if [ "$changed" -eq 0 ] && [ "$BASELINE" -eq 0 ]; then
    printf '\n没有需要应用的迁移（库已是最新）。\n'
else
    printf '\n迁移完成。\n'
fi
if [ "$DO_CHECK" -eq 0 ] && [ "$DRY" -ne 1 ]; then
    printf '提示：列级结构核对请在仓库/CI 用 `python3 extra/check_schema.py --ignore-comments`。\n'
fi
