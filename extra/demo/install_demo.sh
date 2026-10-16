#!/usr/bin/env bash
#
# 装载示例题：写入 problem / contest 表，并把判题数据复制到 <OJHome>/data/<pid>/。
# 目标机只需 bash + mysql/mariadb 客户端。
#
# 数据库凭据默认取自 <OJHome>/etc/judge.conf（与两个服务一致），
# 因此请在 init_db.sh 初始化数据库之后再运行本脚本。
#
# 用法：
#   sudo ./demo/install_demo.sh [选项]
#
# 选项：
#   --ojhome DIR    OJ 根目录（默认 /home/judge）
#   --conf FILE     judge.conf 路径（默认 <ojhome>/etc/judge.conf）
#   --only LIST     只装载列出的题号（逗号/空格分隔），如 --only 1000,1002
#   --no-contest    不创建演示比赛
#   --force         覆盖已存在的题号（默认跳过已存在的题与比赛）
#   --dry-run       只打印将要执行的动作，不改库、不写文件
#   -h, --help      显示帮助
#
# 幂等：重复执行默认跳过已存在的题；--force 才会覆盖。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROBLEMS_DIR="$SCRIPT_DIR/problems"

OJHOME=/home/judge
CONF=""
ONLY=""
DO_CONTEST=1
FORCE=0
DRY=0

info() { printf '==> %s\n' "$*"; }
warn() { printf '警告: %s\n' "$*" >&2; }
die()  { printf '错误: %s\n' "$*" >&2; exit 1; }
usage() { sed -n '2,/^set /p' "$0" | sed -e 's/^# \{0,1\}//' -e '/^set /d'; }

while [ $# -gt 0 ]; do
    case "$1" in
        --ojhome)     OJHOME="$2"; shift 2 ;;
        --conf)       CONF="$2"; shift 2 ;;
        --only)       ONLY="$2"; shift 2 ;;
        --no-contest) DO_CONTEST=0; shift ;;
        --force)      FORCE=1; shift ;;
        --dry-run)    DRY=1; shift ;;
        -h|--help)    usage; exit 0 ;;
        *)            die "未知参数: $1（用 --help 查看用法）" ;;
    esac
done

[ -n "$CONF" ] || CONF="$OJHOME/etc/judge.conf"
[ -f "$CONF" ] || die "找不到 $CONF（可用 --conf 指定，或先运行 init_db.sh）"
[ -d "$PROBLEMS_DIR" ] || die "找不到示例题目录 $PROBLEMS_DIR"

conf_get() { sed -n "s/^[[:space:]]*$1=//p" "$CONF" | head -1 | tr -d '\r'; }
DB_HOST="$(conf_get OJ_HOST_NAME)"; DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="$(conf_get OJ_PORT_NUMBER)"; DB_PORT="${DB_PORT:-3306}"
DB_USER="$(conf_get OJ_USER_NAME)"
DB_PASS="$(conf_get OJ_PASSWORD)"
DB_NAME="$(conf_get OJ_DB_NAME)"
[ -n "$DB_USER" ] && [ -n "$DB_NAME" ] || die "$CONF 缺少 OJ_USER_NAME / OJ_DB_NAME"

if [ "$DRY" -ne 1 ]; then
    MYSQL_BIN="$(command -v mysql || command -v mariadb || true)"
    [ -n "$MYSQL_BIN" ] || die "未找到 mysql/mariadb 客户端"
else
    MYSQL_BIN="mysql"
fi

mysql_run() { # <sql>
    if [ "$DRY" -eq 1 ]; then
        printf '  [dry-run] SQL: %s\n' "${1%%$'\n'*}"
        return 0
    fi
    MYSQL_PWD="$DB_PASS" "$MYSQL_BIN" -h "$DB_HOST" -P "$DB_PORT" -u "$DB_USER" "$DB_NAME" -e "$1"
}

mysql_scalar() { # <sql>
    MYSQL_PWD="$DB_PASS" "$MYSQL_BIN" -h "$DB_HOST" -P "$DB_PORT" -u "$DB_USER" "$DB_NAME" -N -B -e "$1"
}

# SQL 字符串字面量转义（反斜杠 + 单引号）。
sqlq() { local s="$1"; s="${s//\\/\\\\}"; s="${s//\'/\'\'}"; printf "'%s'" "$s"; }
# 读文件为 SQL 字符串字面量。
sqlfile() { local s; s="$(cat "$1")"; s="${s//\\/\\\\}"; s="${s//\'/\'\'}"; printf "'%s'" "$s"; }

want_pid() { # <pid>
    [ -z "$ONLY" ] && return 0
    local t
    for t in ${ONLY//,/ }; do [ "$t" = "$1" ] && return 0; done
    return 1
}

copy_data() { # <srcdir> <pid>
    local src="$1"
    local pid="$2"
    local dst="$OJHOME/data/$pid"
    if [ "$DRY" -eq 1 ]; then
        printf '  [dry-run] 复制 %s/. -> %s/\n' "$src" "$dst"
        return 0
    fi
    mkdir -p "$dst"
    cp -a "$src/." "$dst/"
    [ -f "$dst/spj" ] && chmod 0755 "$dst/spj"
    if id judge >/dev/null 2>&1; then
        chown -R judge:judge "$dst" 2>/dev/null || warn "chown judge:judge $dst 失败"
    fi
}

# ── 逐题装载 ─────────────────────────────────────────────────────────
found=0
for d in "$PROBLEMS_DIR"/*/; do
    [ -f "$d/meta.env" ] || continue
    d="${d%/}"
    found=$((found + 1))

    PID=""; TITLE=""; TIME_LIMIT=1; MEMORY_LIMIT=128; SPJ=0; COIN=1; SOURCE="示例题库"
    # shellcheck disable=SC1090
    . "$d/meta.env"
    [ -n "$PID" ] && [ -n "$TITLE" ] || { warn "$d/meta.env 缺少 PID/TITLE，跳过"; continue; }
    want_pid "$PID" || continue

    if [ "$DRY" -ne 1 ]; then
        existing="$(mysql_scalar "SELECT COUNT(*) FROM problem WHERE problem_id=$PID")"
        if [ "$existing" != 0 ] && [ "$FORCE" -ne 1 ]; then
            info "题 $PID 已存在，跳过（--force 可覆盖）"
            continue
        fi
    fi

    si="NULL"; so="NULL"
    [ -f "$d/sample/sample.in" ]  && si="$(sqlfile "$d/sample/sample.in")"
    [ -f "$d/sample/sample.out" ] && so="$(sqlfile "$d/sample/sample.out")"
    desc="$(sqlfile "$d/statement/description.md")"
    inp="$(sqlfile "$d/statement/input.md")"
    outp="$(sqlfile "$d/statement/output.md")"
    hint="$(sqlfile "$d/statement/hint.md")"

    info "装载题 $PID：$TITLE（spj=$SPJ）"
    mysql_run "INSERT INTO problem
        (problem_id,title,time_limit,memory_limit,description,input,output,
         sample_input,sample_output,hint,source,spj,in_date,defunct,coin)
      VALUES ($PID, $(sqlq "$TITLE"), $TIME_LIMIT, $MEMORY_LIMIT,
              $desc, $inp, $outp, $si, $so, $hint, $(sqlq "$SOURCE"),
              $SPJ, NOW(), 'N', $COIN)
      ON DUPLICATE KEY UPDATE
        title=VALUES(title), time_limit=VALUES(time_limit), memory_limit=VALUES(memory_limit),
        description=VALUES(description), input=VALUES(input), output=VALUES(output),
        sample_input=VALUES(sample_input), sample_output=VALUES(sample_output),
        hint=VALUES(hint), source=VALUES(source), spj=VALUES(spj),
        defunct='N', coin=VALUES(coin)"
    copy_data "$d/data" "$PID"
done

[ "$found" -gt 0 ] || die "在 $PROBLEMS_DIR 下没找到任何 meta.env"

# ── 演示比赛 ─────────────────────────────────────────────────────────
if [ "$DO_CONTEST" -eq 1 ] && [ -f "$SCRIPT_DIR/contest.env" ]; then
    # shellcheck disable=SC1090
    . "$SCRIPT_DIR/contest.env"

    members=""
    for pid in ${CONTEST_PIDS:-}; do
        if [ "$DRY" -eq 1 ]; then
            members="$members $pid"
        elif [ "$(mysql_scalar "SELECT COUNT(*) FROM problem WHERE problem_id=$pid")" != 0 ]; then
            members="$members $pid"
        fi
    done

    if [ -z "${members// /}" ]; then
        info "没有可挂载的题目，跳过演示比赛"
    elif [ "$DRY" -eq 0 ] && [ "$(mysql_scalar "SELECT COUNT(*) FROM contest WHERE contest_id=$CONTEST_ID")" != 0 ] && [ "$FORCE" -ne 1 ]; then
        info "比赛 $CONTEST_ID 已存在，跳过（--force 可覆盖）"
    else
        info "创建演示比赛 $CONTEST_ID：$CONTEST_TITLE"
        mysql_run "INSERT INTO contest
            (contest_id,title,start_time,end_time,defunct,description,private,user_id)
          VALUES ($CONTEST_ID, $(sqlq "$CONTEST_TITLE"),
                  NOW() - INTERVAL 1 DAY, NOW() + INTERVAL 365 DAY,
                  'N', $(sqlq "$CONTEST_DESC"), 0, 'admin')
          ON DUPLICATE KEY UPDATE title=VALUES(title), description=VALUES(description),
            start_time=VALUES(start_time), end_time=VALUES(end_time), defunct='N'"
        mysql_run "DELETE FROM contest_problem WHERE contest_id=$CONTEST_ID"
        num=0
        for pid in $members; do
            mysql_run "INSERT INTO contest_problem (problem_id,contest_id,num) VALUES ($pid,$CONTEST_ID,$num)"
            num=$((num + 1))
        done
    fi
fi

printf '\n示例题装载完成。\n'
printf '  判题数据目录：%s/data/<题号>/\n' "$OJHOME"
if [ "$DO_CONTEST" -eq 1 ]; then
    printf '  演示比赛：contest_id=%s\n' "${CONTEST_ID:-1000}"
fi
printf '  参考解见各题的 solutions/，可用于自检或体验提交。\n'
