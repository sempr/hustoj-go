#!/usr/bin/env bash
#
# 造一批"演示提交"：把示例题 solutions/ 下的反例写成提交，由判题器正常判一遍，
# status.php 上就能看到各种判罚结果（PE/WA/TL/ML/OL/RE/CE、部分分等）。
#
# 用法：
#   sudo ./demo/seed_samples.sh [选项]
#
# 选项：
#   --ojhome DIR    OJ 根目录（默认 /home/judge）
#   --conf FILE     judge.conf 路径（默认 <ojhome>/etc/judge.conf）
#   --pid-base N    示例题基序号（默认 1000）：A+B 题为 N，子任务题为 N+1。
#                   与 install_demo.sh 的 --pid-base 同一套编号，方便对着
#                   已用 --auto-pid 装载过的题库继续用
#   --user NAME     提交者账号（默认 demo；该账号必须已存在）
#   --only LIST     只造列出的项（逗号/空格分隔），项名见下方 SEED_ITEMS
#   --force         已存在同一条（用户+题目+语言+源码相同）也再次插入
#   --dry-run       只打印将要执行的 SQL，不改库
#   -h, --help      显示帮助
#
# 说明：
#  * 提交会进入正常判题队列（result=0），判完由判题器回写结果与得分；
#  * source_code 与 source_code_user 都要写：前者是判题用的副本，后者是
#    showsource.php 展示的那份，缺一就会出现"没有可用代码"；
#  * first_time 置 1：这些是演示数据，不该给账号发金币（solution.result=4
#    时会触发 firstAC 触发器改 users.coin_earned）。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROBLEMS_DIR="$SCRIPT_DIR/problems"

OJHOME=/home/judge
CONF=""
PID_BASE=1000
USER_NAME=demo
ONLY=""
FORCE=0
DRY=0

info() { printf '==> %s\n' "$*"; }
warn() { printf '警告: %s\n' "$*" >&2; }
die()  { printf '错误: %s\n' "$*" >&2; exit 1; }
usage() { sed -n '2,/^set /p' "$0" | sed -e 's/^# \{0,1\}//' -e '/^set /d'; }

while [ $# -gt 0 ]; do
    case "$1" in
        --ojhome)   OJHOME="$2"; shift 2 ;;
        --conf)     CONF="$2"; shift 2 ;;
        --pid-base) PID_BASE="$2"; shift 2 ;;
        --user)     USER_NAME="$2"; shift 2 ;;
        --only)     ONLY="$2"; shift 2 ;;
        --force)    FORCE=1; shift ;;
        --dry-run)  DRY=1; shift ;;
        -h|--help)  usage; exit 0 ;;
        *)          die "未知参数: $1（用 --help 查看用法）" ;;
    esac
done

[ -n "$CONF" ] || CONF="$OJHOME/etc/judge.conf"
[ -f "$CONF" ] || die "找不到 $CONF（可用 --conf 指定，或先运行 init_db.sh）"
[ -d "$PROBLEMS_DIR" ] || die "找不到示例题目录 $PROBLEMS_DIR"

case "$PID_BASE" in
    ''|*[!0-9]*) die "--pid-base 必须是正整数: $PID_BASE" ;;
esac
[ "$PID_BASE" -ge 1 ] || die "--pid-base 必须 >= 1"

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

# SQL 字符串字面量转义（反斜杠 + 单引号）。
sqlq() { local s="$1"; s="${s//\\/\\\\}"; s="${s//\'/\'\'}"; printf "'%s'" "$s"; }
# 读文件为 SQL 字符串字面量。
sqlfile() { local s; s="$(cat "$1")"; s="${s//\\/\\\\}"; s="${s//\'/\'\'}"; printf "'%s'" "$s"; }

# 语言注册表：<ojhome>/etc/langs/all.toml，按 name 取 id。
#
# 语言 id 由注册表决定，各 installations 可能不同，所以不能写死；查不到时
# 退回内置表（与仓库默认注册表一致）。
LANGS_TOML="$OJHOME/etc/langs/all.toml"
lang_id_by_name() { # <name>
    local name="$1"
    if [ -f "$LANGS_TOML" ]; then
        local id
        id="$(awk -v want="$name" '
            function flush() { if (cur_name == want && cur_id != "") { print cur_id; exit } }
            /^\[\[lang\]\]/ { flush(); cur_name = ""; cur_id = "" }
            /^id *= *"/ { v = $0; gsub(/[^0-9]/, "", v); cur_id = v }
            /^name *= *"/ { v = $0; sub(/^[^=]*=[[:space:]]*"/, "", v); sub(/".*$/, "", v); cur_name = v }
            END { flush() }
        ' "$LANGS_TOML")"
        if [ -n "$id" ]; then printf '%s' "$id"; return 0; fi
    fi
    case "$name" in
        C) printf '0' ;; CPP) printf '1' ;; PASCAL) printf '2' ;; JAVA) printf '3' ;;
        RUBY) printf '4' ;; BASH) printf '5' ;; PYTHON) printf '6' ;; PHP) printf '7' ;;
        PERL) printf '8' ;; CSHARP) printf '9' ;; JS) printf '16' ;; GO) printf '17' ;;
        RUST) printf '25' ;; *) return 1 ;;
    esac
}

# 扩展名 → 语言注册表里的 name。
lang_name_by_ext() { # <ext>
    case "$1" in
        c) printf 'C' ;; cpp|cc) printf 'CPP' ;; py) printf 'PYTHON' ;;
        java) printf 'JAVA' ;; go) printf 'GO' ;; rb) printf 'RUBY' ;;
        pas) printf 'PASCAL' ;; sh) printf 'BASH' ;; php) printf 'PHP' ;;
        pl) printf 'PERL' ;; cs) printf 'CSHARP' ;; ts) printf 'JS' ;;
        rs) printf 'RUST' ;;
        *) return 1 ;;
    esac
}

# 语言是否真的安装了（有 <id>.lang.toml 才算）。没装的直接跳过，
# 否则会写入判题器无法运行的提交，status 页只会出现失败的判罚。
lang_installed() { # <id>
    [ -f "$OJHOME/etc/langs/$1.lang.toml" ]
}

# ── 演示项 ────────────────────────────────────────────────────────────────
# 格式：<项名>:<相对 problems/ 的解法文件>:<预期判罚>
#
# 覆盖：
#   1) solutions/ 下所有语言的满分参考解（有代码就提交，包含 Go/Java/
#      Pascal/Bash/PHP/Perl/C#/TypeScript/Rust）；
#   2) 子任务题 1001 的部分分（C/C++/Python，均 30 分）与捆绑测试 0 分；
#   3) SPJ 题 1002 的 WA 反例；
#   4) A+B 的各类非 AC 判罚（PE/WA/TL/ML/OL/RE/CE 等）。
# 未安装的语言由 lang_installed 跳过。
SEED_ITEMS=(
    "ac_1000_c:1000-aplusb/solutions/ac.c:满分 AC（C）"
    "ac_1000_cpp:1000-aplusb/solutions/ac.cpp:满分 AC（C++）"
    "ac_1000_py:1000-aplusb/solutions/ac.py:满分 AC（Python）"
    "ac_1000_go:1000-aplusb/solutions/ac.go:满分 AC（Go）"
    "ac_1000_java:1000-aplusb/solutions/ac.java:满分 AC（Java）"
    "ac_1001_c:1001-subtask/solutions/ac.c:满分 AC（C）"
    "ac_1001_cpp:1001-subtask/solutions/ac.cpp:满分 AC（C++）"
    "ac_1001_py:1001-subtask/solutions/ac.py:满分 AC（Python）"
    "ac_1002_c:1002-spj-magic/solutions/ac.c:满分 AC（C）"
    "ac_1002_cpp:1002-spj-magic/solutions/ac.cpp:满分 AC（C++）"
    "ac_1002_py:1002-spj-magic/solutions/ac.py:满分 AC（Python）"
    "ac_1003_c:1003-interactive/solutions/ac.c:满分 AC（C，交互）"
    "ac_1003_cpp:1003-interactive/solutions/ac.cpp:满分 AC（C++，交互）"
    "ac_1003_py:1003-interactive/solutions/ac.py:满分 AC（Python，交互）"
    "ac_1003_java:1003-interactive/solutions/ac.java:满分 AC（Java，交互）"
    "ac_1003_go:1003-interactive/solutions/ac.go:满分 AC（Go，交互）"
    "ac_1003_pas:1003-interactive/solutions/ac.pas:满分 AC（Pascal，交互）"
    "ac_1003_sh:1003-interactive/solutions/ac.sh:满分 AC（Bash，交互）"
    "ac_1003_php:1003-interactive/solutions/ac.php:满分 AC（PHP，交互）"
    "ac_1003_pl:1003-interactive/solutions/ac.pl:满分 AC（Perl，交互）"
    "ac_1003_cs:1003-interactive/solutions/ac.cs:满分 AC（C#，交互）"
    "ac_1003_ts:1003-interactive/solutions/ac.ts:满分 AC（TypeScript，交互）"
    "ac_1003_rs:1003-interactive/solutions/ac.rs:满分 AC（Rust，交互）"
    "ac_1004_c:1004-fileio/solutions/ac.c:满分 AC（C，文件 IO）"
    "ac_1004_cpp:1004-fileio/solutions/ac.cpp:满分 AC（C++，文件 IO）"
    "ac_1004_py:1004-fileio/solutions/ac.py:满分 AC（Python，文件 IO）"
    "partial_1001_c:1001-subtask/solutions/partial.c:部分分 30/100（C）"
    "partial_1001_cpp:1001-subtask/solutions/partial.cpp:部分分 30/100（C++）"
    "partial_1001_py:1001-subtask/solutions/partial.py:部分分 30/100（Python）"
    "bundle:1001-subtask/solutions/bundle_partial.cpp:捆绑测试：同组只过一个 → 0 分"
    "wa_1002:1002-spj-magic/solutions/wa.cpp:SPJ 答案错误 WA"
    "pe:1000-aplusb/solutions/pe.cpp:格式错误 PE"
    "wa:1000-aplusb/solutions/wa.cpp:答案错误 WA"
    "tl:1000-aplusb/solutions/tle.cpp:时间超限 TL"
    "ml:1000-aplusb/solutions/mle.cpp:内存超限 ML"
    "ol:1000-aplusb/solutions/ole.cpp:输出超限 OL"
    "re:1000-aplusb/solutions/re.cpp:运行错误 RE"
    "ce:1000-aplusb/solutions/ce.cpp:编译错误 CE"
    "ml_first:1000-aplusb/solutions/mle_first.cpp:内存先超（内存+输出双超）"
    "ol_first:1000-aplusb/solutions/ole_first.cpp:输出先超（内存+输出双超）"
)

want_item() { # <name>
    [ -z "$ONLY" ] && return 0
    local t
    for t in ${ONLY//,/ }; do [ "$t" = "$1" ] && return 0; done
    return 1
}

# 某个解法文件属于哪道题：按 problems/<目录> 的序号偏移到 pid。
pid_of_file() { # <相对路径>
    case "$1" in
        1000-aplusb/*)      printf '%s' "$PID_BASE" ;;
        1001-subtask/*)     printf '%s' "$(( PID_BASE + 1 ))" ;;
        1002-spj-magic/*)   printf '%s' "$(( PID_BASE + 2 ))" ;;
        1003-interactive/*) printf '%s' "$(( PID_BASE + 3 ))" ;;
        1004-fileio/*)      printf '%s' "$(( PID_BASE + 4 ))" ;;
        *) return 1 ;;
    esac
}

[ "$DRY" -eq 1 ] || [ -n "$MYSQL_BIN" ] || die "未找到 mysql/mariadb 客户端"

if [ "$DRY" -ne 1 ]; then
    if ! mysql_scalar "SELECT COUNT(1) FROM users WHERE user_id = $(sqlq "$USER_NAME")" | grep -q '^1$'; then
        die "账号 $USER_NAME 不存在（先在题库里建好这个账号，或用 --user 指定）"
    fi
fi

info "演示提交：账号 $USER_NAME，A+B 题号 $PID_BASE，子任务题号 $(( PID_BASE + 1 ))"

for item in "${SEED_ITEMS[@]}"; do
    name="${item%%:*}"
    rest="${item#*:}"
    rel="${rest%%:*}"
    expect="${rest#*:}"

    want_item "$name" || continue

    src="$PROBLEMS_DIR/$rel"
    [ -f "$src" ] || die "找不到解法文件 $src"

    pid="$(pid_of_file "$rel")" || die "无法确定 $rel 属于哪道题"
    ext="${src##*.}"
    lang_name="$(lang_name_by_ext "$ext")" || die "未知扩展名: $src"
    lang="$(lang_id_by_name "$lang_name")" || die "语言注册表里没有 $lang_name（$src）"

    if ! lang_installed "$lang"; then
        info "$name：跳过（$lang_name 语言未安装，缺少 $OJHOME/etc/langs/$lang.lang.toml）"
        continue
    fi

    # 幂等：同一用户、题目、语言且源码完全相同时不重复插入。
    # 这样重跑脚本只会补装新增的示例提交（例如后来补上的多语言 AC/部分分）。
    if [ "$DRY" -ne 1 ] && [ "$FORCE" -ne 1 ]; then
        exists="$(mysql_scalar "SELECT COUNT(*)
            FROM solution s
            JOIN source_code sc ON sc.solution_id = s.solution_id
            WHERE s.user_id = $(sqlq "$USER_NAME")
              AND s.problem_id = $pid
              AND s.language = $lang
              AND sc.source = $(sqlfile "$src")")"
        if [ "${exists:-0}" -gt 0 ]; then
            info "$name：已存在相同提交，跳过"
            continue
        fi
    fi

    info "$name → 题号 $pid，语言 $lang_name($lang)，预期 $expect"

    mysql_run "
INSERT INTO solution (problem_id, user_id, nick, in_date, result, language, ip,
                      contest_id, valid, num, code_length, judger)
VALUES ($pid, $(sqlq "$USER_NAME"), $(sqlq "$USER_NAME"), NOW(), 0, $lang, '127.0.0.1',
        0, 1, -1, $(wc -c < "$src" | tr -d ' '), 'LOCAL');
SET @sid = LAST_INSERT_ID();
INSERT INTO source_code (solution_id, source) VALUES (@sid, $(sqlfile "$src"));
INSERT INTO source_code_user (solution_id, source) VALUES (@sid, $(sqlfile "$src"));
UPDATE solution SET first_time = 1 WHERE solution_id = @sid;
"
done

info "完成：提交已进入判题队列，等 judged-go 判完就能在 status.php 看到"
