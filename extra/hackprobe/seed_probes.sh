#!/usr/bin/env bash
#
# 提交沙箱逃逸探针：把 probes/ 下的探针作为"测试运行"提交，由判题器在沙箱里
# 跑一遍，探针的输出会写进 runtimeinfo，在 showsource.php 的"各用例结果"里
# 就能看到报告。
#
# 用法：
#   sudo ./hackprobe/seed_probes.sh [选项]
#
# 选项：
#   --ojhome DIR    OJ 根目录（默认 /home/judge）
#   --conf FILE     judge.conf 路径（默认 <ojhome>/etc/judge.conf）
#   --user NAME     提交者账号（默认 demo；该账号必须已存在）
#   --only LIST     只提交列出的探针（逗号/空格分隔）
#   --dry-run       只打印将要执行的 SQL，不改库
#   -h, --help      显示帮助
#
# 说明：
#  * 走测试运行（problem_id=0 + custominput）而不是普通提交：测试运行的
#    stdout 会进 runtimeinfo，普通提交不会；
#  * 探针只做探测与自述，不带破坏性载荷；唯一的写操作是往
#    /tmp/hustoj_escape_probe 写标记文件，用来确认是否真的逃到了宿主机；
#  * fork/内存那两个探针会真的把资源顶到上限，判题器会按超限判掉
#    （TL/ML），这是预期行为。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROBES_DIR="$SCRIPT_DIR/probes"

OJHOME=/home/judge
CONF=""
USER_NAME=demo
ONLY=""
DRY=0

info() { printf '==> %s\n' "$*"; }
warn() { printf '警告: %s\n' "$*" >&2; }
die()  { printf '错误: %s\n' "$*" >&2; exit 1; }
usage() { sed -n '2,/^set /p' "$0" | sed -e 's/^# \{0,1\}//' -e '/^set /d'; }

while [ $# -gt 0 ]; do
    case "$1" in
        --ojhome)  OJHOME="$2"; shift 2 ;;
        --conf)    CONF="$2"; shift 2 ;;
        --user)    USER_NAME="$2"; shift 2 ;;
        --only)    ONLY="$2"; shift 2 ;;
        --dry-run) DRY=1; shift ;;
        -h|--help) usage; exit 0 ;;
        *)         die "未知参数: $1（用 --help 查看用法）" ;;
    esac
done

[ -n "$CONF" ] || CONF="$OJHOME/etc/judge.conf"
[ -f "$CONF" ] || die "找不到 $CONF（可用 --conf 指定，或先运行 init_db.sh）"
[ -d "$PROBES_DIR" ] || die "找不到探针目录 $PROBES_DIR"

conf_get() { sed -n "s/^[[:space:]]*$1=//p" "$CONF" | head -1 | tr -d '\r'; }
DB_HOST="$(conf_get OJ_HOST_NAME)"; DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="$(conf_get OJ_PORT_NUMBER)"; DB_PORT="${DB_PORT:-3306}"
DB_USER="$(conf_get OJ_USER_NAME)"
DB_PASS="$(conf_get OJ_PASSWORD)"
DB_NAME="$(conf_get OJ_DB_NAME)"
[ -n "$DB_USER" ] && [ -n "$DB_NAME" ] || die "$CONF 缺少 OJ_USER_NAME / OJ_DB_NAME"

MYSQL_BIN="$(command -v mysql || command -v mariadb || true)"
[ -n "$MYSQL_BIN" ] || die "未找到 mysql/mariadb 客户端"

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

sqlq() { local s="$1"; s="${s//\\/\\\\}"; s="${s//\'/\'\'}"; printf "'%s'" "$s"; }
sqlfile() { local s; s="$(cat "$1")"; s="${s//\\/\\\\}"; s="${s//\'/\'\'}"; printf "'%s'" "$s"; }

# 语言 id：C 的 id 从注册表查（各 installations 可能不同）。
LANGS_TOML="$OJHOME/etc/langs/all.toml"
lang_id() { # <name>
    local name="$1" id=""
    if [ -f "$LANGS_TOML" ]; then
        id="$(awk -v want="$name" '
            function flush() { if (cur_name == want && cur_id != "") { print cur_id; exit } }
            /^\[\[lang\]\]/ { flush(); cur_name = ""; cur_id = "" }
            /^id *= *"/ { v = $0; gsub(/[^0-9]/, "", v); cur_id = v }
            /^name *= *"/ { v = $0; sub(/^[^=]*=[[:space:]]*"/, "", v); sub(/".*$/, "", v); cur_name = v }
            END { flush() }
        ' "$LANGS_TOML")"
    fi
    [ -n "$id" ] || id=0   # 回退：C
    printf '%s' "$id"
}

want_probe() { # <name>
    [ -z "$ONLY" ] && return 0
    local t
    for t in ${ONLY//,/ }; do [ "$t" = "$1" ] && return 0; done
    return 1
}

if [ "$DRY" -ne 1 ]; then
    if ! mysql_scalar "SELECT COUNT(1) FROM users WHERE user_id = $(sqlq "$USER_NAME")" | grep -q '^1$'; then
        die "账号 $USER_NAME 不存在（先在题库里建好这个账号，或用 --user 指定）"
    fi
fi

C_LANG="$(lang_id C)"
info "沙箱探针：账号 $USER_NAME，语言 C($C_LANG)"

for src in "$PROBES_DIR"/*.c; do
    name="$(basename "$src" .c)"
    want_probe "$name" || continue

    info "$name"
    mysql_run "
INSERT INTO solution (problem_id, user_id, nick, in_date, result, language, ip,
                      contest_id, valid, num, code_length, judger)
VALUES (0, $(sqlq "$USER_NAME"), $(sqlq "$USER_NAME"), NOW(), 0, $C_LANG, '127.0.0.1',
        0, 1, -1, $(wc -c < "$src" | tr -d ' '), 'LOCAL');
SET @sid = LAST_INSERT_ID();
INSERT INTO source_code (solution_id, source) VALUES (@sid, $(sqlfile "$src"));
INSERT INTO source_code_user (solution_id, source) VALUES (@sid, $(sqlfile "$src"));
INSERT INTO custominput (solution_id, input_text) VALUES (@sid, '');
UPDATE solution SET first_time = 1 WHERE solution_id = @sid;
SELECT @sid;
"
done

info "完成：探针已进入判题队列；判完后在 showsource.php 的'各用例结果'里看报告"
