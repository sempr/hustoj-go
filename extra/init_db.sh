#!/usr/bin/env bash
#
# 为全新部署初始化 HUSTOJ 数据库：建库、建应用账号、导入表结构、建触发器、种子管理员。
# 目标机只需 bash + mysql/mariadb 客户端。
#
# 执行顺序：schema/schema.sql（建表）→ schema/triggers.sql（触发器）→ schema/seed.sql（管理员）。
#
# 安全设计：
#   * 目标库已有表时默认拒绝执行（schema.sql 含 DROP TABLE，属破坏性）；
#     确认覆盖加 --force；整库重来加 --recreate（会 DROP DATABASE）。
#   * --dry-run 只打印计划、不连接数据库。
#   * 应用账号口令仅通过 MYSQL_PWD 环境变量传递，不出现在命令行/进程表。
#
# 用法：
#   sudo ./init_db.sh --db hustoj --user hustoj --password '强口令'
#   常用选项：
#     --host 127.0.0.1 --port 3306     应用连接的地址（会写进 judge.conf）
#     --root-user root                  管理员账号（默认 root）
#     --root-password '...'             管理员口令
#     --socket /run/mysqld/mysqld.sock  管理员走 unix socket（默认走客户端默认）
#     --admin-host HOST --admin-port N  管理员改走 TCP（默认 socket）
#     --admin-user admin --admin-password admin   种子管理员（默认 admin/admin）
#     --no-triggers --no-seed           跳过触发器 / 种子
#     --ojhome /home/judge --write-conf 把 OJ_* 写入 <ojhome>/etc/judge.conf（0600 root:root）
#     --force                           目标库非空时仍继续（会丢表内数据）
#     --recreate                        DROP DATABASE 后重建
#     --dry-run                         只打印计划
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [ -d "$SCRIPT_DIR/schema" ]; then
    SCHEMA_DIR="$SCRIPT_DIR/schema"
elif [ -d "$SCRIPT_DIR/../schema" ]; then
    SCHEMA_DIR="$(cd "$SCRIPT_DIR/../schema" && pwd)"
else
    SCHEMA_DIR=""
fi

HOST=127.0.0.1
PORT=3306
DB=hustoj
DBUSER=hustoj
DBPASSWORD=""
ADMIN_USER=admin
ADMIN_PASSWORD=admin
ROOT_USER=root
ROOT_PASSWORD=""
SOCKET=""
ADMIN_HOST=""
ADMIN_PORT=3306
DO_TRIGGERS=1
DO_SEED=1
FORCE=0
RECREATE=0
WRITE_CONF=0
OJHOME=/home/judge
DRY=0

log()  { printf '==> %s\n' "$*"; }
warn() { printf '警告: %s\n' "$*" >&2; }
die()  { printf '错误: %s\n' "$*" >&2; exit 1; }

usage() { sed -n '2,/^set /p' "$0" | sed -e 's/^# \{0,1\}//' -e '/^set /d'; }

# SQL 字符串字面量转义（反斜杠与单引号），用于拼接连接信息。
sq() {
    local s="$1"
    s="${s//\\/\\\\}"
    s="${s//\'/\'\'}"
    printf '%s' "$s"
}

while [ $# -gt 0 ]; do
    case "$1" in
        --host)            HOST="$2"; shift 2 ;;
        --port)            PORT="$2"; shift 2 ;;
        --db|--db-name)    DB="$2"; shift 2 ;;
        --user|--db-user)  DBUSER="$2"; shift 2 ;;
        --password)        DBPASSWORD="$2"; shift 2 ;;
        --root-user)       ROOT_USER="$2"; shift 2 ;;
        --root-password)   ROOT_PASSWORD="$2"; shift 2 ;;
        --socket)          SOCKET="$2"; shift 2 ;;
        --admin-host)      ADMIN_HOST="$2"; shift 2 ;;
        --admin-port)      ADMIN_PORT="$2"; shift 2 ;;
        --admin-user)      ADMIN_USER="$2"; shift 2 ;;
        --admin-password)  ADMIN_PASSWORD="$2"; shift 2 ;;
        --ojhome)          OJHOME="$2"; shift 2 ;;
        --no-triggers)     DO_TRIGGERS=0; shift ;;
        --no-seed)         DO_SEED=0; shift ;;
        --write-conf)      WRITE_CONF=1; shift ;;
        --force)           FORCE=1; shift ;;
        --recreate)        RECREATE=1; shift ;;
        --dry-run)         DRY=1; shift ;;
        -h|--help)         usage; exit 0 ;;
        *)                 die "未知参数: $1（用 --help 查看用法）" ;;
    esac
done

is_ident() { case "$1" in ''|*[!A-Za-z0-9_]*) return 1 ;; esac; [ "${#1}" -le 64 ]; }
is_ident "$DB"     || die "库名只能含字母/数字/下划线且非空: '$DB'"
is_ident "$DBUSER" || die "应用账号只能含字母/数字/下划线且非空: '$DBUSER'"
[ -n "$DBPASSWORD" ] || die "需要 --password 指定应用账号口令"

if [ "$DRY" -ne 1 ]; then
    [ -n "$SCHEMA_DIR" ] || die "找不到 schema 目录（schema.sql/triggers.sql/seed.sql）"
    for f in schema.sql; do
        [ -f "$SCHEMA_DIR/$f" ] || die "缺少 $SCHEMA_DIR/$f"
    done
    [ "$DO_TRIGGERS" -eq 0 ] || [ -f "$SCHEMA_DIR/triggers.sql" ] || die "缺少 $SCHEMA_DIR/triggers.sql"
    [ "$DO_SEED" -eq 0 ] || [ -f "$SCHEMA_DIR/seed.sql" ] || die "缺少 $SCHEMA_DIR/seed.sql"
fi

# ── mysql 客户端与管理员连接参数 ─────────────────────────────────────
if [ "$DRY" -ne 1 ]; then
    MYSQL_BIN="$(command -v mysql || command -v mariadb || true)"
    [ -n "$MYSQL_BIN" ] || die "未找到 mysql/mariadb 客户端"
else
    MYSQL_BIN="${MYSQL_BIN:-mysql}"
fi

admin_conn=(-u "$ROOT_USER")
[ -n "$SOCKET" ] && admin_conn+=(-S "$SOCKET")
[ -n "$ADMIN_HOST" ] && admin_conn+=(-h "$ADMIN_HOST" -P "$ADMIN_PORT")
admin_env=()
[ -n "$ROOT_PASSWORD" ] && admin_env=(MYSQL_PWD="$ROOT_PASSWORD")

sql_admin() { # <sql> [db]
    local sql="$1" db="${2:-}"
    local args=("${admin_conn[@]}")
    [ -n "$db" ] && args+=("$db")
    if [ "$DRY" -eq 1 ]; then
        printf '  [dry-run] mysql %s -e %s\n' "${args[*]}" "$sql"
        return 0
    fi
    env "${admin_env[@]}" "$MYSQL_BIN" "${args[@]}" -e "$sql"
}

sql_file() { # <db> <file> [extra mysql args...]
    local db="$1" file="$2"; shift 2
    if [ "$DRY" -eq 1 ]; then
        printf '  [dry-run] mysql %s%s %s < %s\n' "${admin_conn[*]}" "${*:+ $*}" "$db" "$file"
        return 0
    fi
    env "${admin_env[@]}" "$MYSQL_BIN" "${admin_conn[@]}" "$@" "$db" < "$file"
}

write_conf() {
    local conf="$OJHOME/etc/judge.conf" tmp
    if [ "$DRY" -eq 1 ]; then
        printf '  [dry-run] 写入 %s（OJ_HOST_NAME/OJ_PORT_NUMBER/OJ_USER_NAME/OJ_PASSWORD/OJ_DB_NAME）\n' "$conf"
        return 0
    fi
    mkdir -p "$OJHOME/etc"
    tmp="$(mktemp)"
    if [ -f "$conf" ]; then
        grep -vE '^[[:space:]]*OJ_(HOST_NAME|PORT_NUMBER|USER_NAME|PASSWORD|DB_NAME)=' "$conf" > "$tmp" || true
    fi
    {
        printf 'OJ_HOST_NAME=%s\n' "$HOST"
        printf 'OJ_PORT_NUMBER=%s\n' "$PORT"
        printf 'OJ_USER_NAME=%s\n' "$DBUSER"
        printf 'OJ_PASSWORD=%s\n' "$DBPASSWORD"
        printf 'OJ_DB_NAME=%s\n' "$DB"
    } >> "$tmp"
    install -m 0600 "$tmp" "$conf"
    rm -f "$tmp"
    log "已写入 $conf"
}

# ── 管理员连通性 ─────────────────────────────────────────────────────
if [ "$DRY" -ne 1 ]; then
    if ! env "${admin_env[@]}" "$MYSQL_BIN" "${admin_conn[@]}" -N -B -e 'SELECT 1' >/dev/null 2>&1; then
        die "无法以 '$ROOT_USER' 连接数据库；本地默认走 unix socket，可试 --socket /run/mysqld/mysqld.sock 或 --root-password"
    fi
fi

# ── 目标库是否已有表 ─────────────────────────────────────────────────
if [ "$DRY" -ne 1 ] && [ "$RECREATE" -eq 0 ]; then
    existing="$(env "${admin_env[@]}" "$MYSQL_BIN" "${admin_conn[@]}" -N -B \
        -e "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='$(sq "$DB")'")"
    if [ "${existing:-0}" -gt 0 ] && [ "$FORCE" -ne 1 ]; then
        die "库 '$DB' 已有 $existing 张表；schema.sql 含 DROP TABLE 会清空数据。确认覆盖加 --force，整库重来加 --recreate"
    fi
fi

# ── 1) 库 ────────────────────────────────────────────────────────────
if [ "$RECREATE" -eq 1 ]; then
    log "DROP DATABASE IF EXISTS \`$DB\`"
    sql_admin "DROP DATABASE IF EXISTS \`$DB\`"
fi
log "CREATE DATABASE IF NOT EXISTS \`$DB\` (utf8mb4)"
sql_admin "CREATE DATABASE IF NOT EXISTS \`$DB\` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci"

# ── 2) 应用账号（localhost 与 127.0.0.1 都建，兼容 skip-name-resolve）──
log "创建/更新应用账号 '$DBUSER' 并授权"
pw="$(sq "$DBPASSWORD")"
for h in localhost 127.0.0.1; do
    sql_admin "CREATE USER IF NOT EXISTS '$DBUSER'@'$h' IDENTIFIED BY '$pw'"
    sql_admin "ALTER USER '$DBUSER'@'$h' IDENTIFIED BY '$pw'"
    sql_admin "GRANT ALL PRIVILEGES ON \`$DB\`.* TO '$DBUSER'@'$h'"
done
sql_admin "FLUSH PRIVILEGES"

# ── 3) 表结构 ────────────────────────────────────────────────────────
log "导入 schema.sql（23 张表）"
sql_file "$DB" "$SCHEMA_DIR/schema.sql"

# ── 4) 触发器 ────────────────────────────────────────────────────────
if [ "$DO_TRIGGERS" -eq 1 ]; then
    log "创建触发器 firstAC / simfilter"
    sql_file "$DB" "$SCHEMA_DIR/triggers.sql"
fi

# ── 5) 种子管理员 ────────────────────────────────────────────────────
if [ "$DO_SEED" -eq 1 ]; then
    log "种子管理员 '$ADMIN_USER'"
    sql_file "$DB" "$SCHEMA_DIR/seed.sql" \
        --init-command="SET @admin_user='$(sq "$ADMIN_USER")', @admin_password='$(sq "$ADMIN_PASSWORD")'"
fi

# ── 6) 写 judge.conf ─────────────────────────────────────────────────
[ "$WRITE_CONF" -eq 1 ] && write_conf

# ── 7) 用应用账号自检 ────────────────────────────────────────────────
if [ "$DRY" -ne 1 ]; then
    log "用应用账号 '$DBUSER' 连接自检"
    if MYSQL_PWD="$DBPASSWORD" "$MYSQL_BIN" -h "$HOST" -P "$PORT" -u "$DBUSER" -N -B \
        -e "SELECT CONCAT('tables=', COUNT(*)) FROM information_schema.tables WHERE table_schema=DATABASE()" "$DB"; then
        :
    else
        warn "应用账号连接失败；检查 '$DBUSER'@'{localhost,127.0.0.1}' 的口令与 skip-name-resolve 设置"
    fi
fi

printf '\n数据库初始化完成。\n'
if [ "$WRITE_CONF" -ne 1 ]; then
    printf '请把以下内容写入 %s/etc/judge.conf：\n' "$OJHOME"
    printf '  OJ_HOST_NAME=%s\n  OJ_PORT_NUMBER=%s\n  OJ_USER_NAME=%s\n  OJ_PASSWORD=%s\n  OJ_DB_NAME=%s\n' \
        "$HOST" "$PORT" "$DBUSER" "$DBPASSWORD" "$DB"
fi
printf '然后重启服务：systemctl restart judged-go.service hustoj-web.service\n'
