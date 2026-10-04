#!/bin/bash
#
# 校验语言索引契约在四处定义中保持一致。
#
# 语言索引是 PHP 前端、Go judge 客户端与 judge.conf 共同依赖的隐式契约：
#
#   1. extra/etc/langs/all.toml            完整注册表（name/id/suffix）
#   2. extra/etc/langs/<id>.lang.toml      仅 extra/ 下有 Dockerfile 的语言
#   3. phpweb/include/const.inc.php        PHP 的 $language_name / $language_ext
#   4. judge.conf 的 OJ_LANG_SET           judge 实际受理的语言 id
#   5. db_info.inc.php 的 $OJ_LANGMASK     提交页可见语言（【禁用位掩码】）
#
# 任一处漂移都会导致「用户能选但判题失败」或反之。
# 历史事故：仓库 const.inc.php 缺失 Rust（id 25）而 judge.conf 已启用 25，
# 会使 Rust 提交的语言名退化为空。
#
# 用法: bash extra/check_lang_index.sh [judge.conf 路径]

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$SCRIPT_DIR/.." && pwd)"

ALL_TOML="$REPO/extra/etc/langs/all.toml"
LANGS_DIR="$REPO/extra/etc/langs"
CONST_INC="$REPO/phpweb/include/const.inc.php"
JUDGE_CONF="${1:-/home/judge/etc/judge.conf}"

rc=0
fail() { echo "  ✗ $1"; rc=1; }
warn() { echo "  ! $1"; }
ok()   { echo "  ✓ $1"; }

echo "===> 语言索引一致性校验"

# ---------- 1. all.toml ----------
[[ -f "$ALL_TOML" ]] || { fail "缺少 $ALL_TOML"; exit 1; }

mapfile -t ids < <(grep -oE 'id = [0-9]+' "$ALL_TOML" | grep -oE '[0-9]+' | sort -n)
[[ ${#ids[@]} -gt 0 ]] || fail "all.toml 未解析到任何语言"

for i in "${!ids[@]}"; do
    if [[ "${ids[$i]}" != "$i" ]]; then
        fail "all.toml id 不连续：位置 $i 期望 id=$i，实际 id=${ids[$i]}"
        break
    fi
done
[[ ${#ids[@]} -gt 0 ]] && ok "all.toml id 连续 0..$(( ${#ids[@]} - 1 ))（共 ${#ids[@]} 种）"

lang_count=${#ids[@]}

# id -> name 映射，供告警文本使用
declare -A lang_name=()
while read -r nm idx; do
    [[ "$nm" =~ ^[A-Za-z0-9_+#.-]+$ ]] && lang_name[$idx]="$nm"
done < <(paste -d' ' \
    <(grep -oE 'name = "[^"]+"' "$ALL_TOML" | sed 's/name = "//;s/"//') \
    <(grep -oE 'id = [0-9]+'    "$ALL_TOML" | grep -oE '[0-9]+'))

# ---------- 2. 存在配置文件的 id ----------
configs=()
for f in "$LANGS_DIR"/*.lang.toml; do
    [[ -e "$f" ]] || continue
    b="$(basename "$f" .lang.toml)"
    [[ "$b" =~ ^[0-9]+$ ]] && configs+=("$b")
done
ok "存在 judge 配置的语言 id: ${configs[*]:-无}"

# ---------- 3. const.inc.php ----------
if [[ -f "$CONST_INC" ]]; then
    names_n=$(grep -oE '\$language_name=Array\([^;]*' "$CONST_INC" \
              | grep -oE '"[^"]*"' | wc -l)
    exts_n=$(grep -oE '\$language_ext=Array\([^;]*' "$CONST_INC" \
             | grep -oE '"[^"]*"' | wc -l)

    # 契约: $language_ext 比 $language_name 少一项。缺的那一项是
    # UnknownLanguage 哨兵 —— 提交页用 $lang_count = count($language_ext)
    # 作为禁用位掩码的位宽，从而把该哨兵自然排除在下拉框之外。
    # 若两者等长，$lang_count 会变成 27，掩码将放行 id=26（无意义选项）。
    if [[ "$exts_n" != "$(( names_n - 1 ))" ]]; then
        fail "const.inc.php 语言后缀数($exts_n)应为语言名数($names_n)-1（末项为 UnknownLanguage 哨兵，不应出现在后缀表中）"
    else
        ok "const.inc.php 后缀数 = 名称数 - 1（$exts_n = $names_n - 1），哨兵已排除在掩码位宽外"
    fi

    # 末项必须是 UnknownLanguage 哨兵；它不对应任何真实语言
    if grep -qE '\$language_name=Array\([^;]*"UnknownLanguage"' "$CONST_INC"; then
        sentinel=$(( names_n - 1 ))
        if [[ "$sentinel" -eq "$exts_n" ]]; then
            ok "const.inc.php 末项为 UnknownLanguage 哨兵（id=$sentinel，超出掩码位宽 0..$((exts_n-1))）"
        else
            fail "const.inc.php 哨兵 id=$sentinel 与后缀表长度 $exts_n 不一致"
        fi
    else
        fail "const.inc.php 缺少 UnknownLanguage 哨兵项"
    fi
else
    fail "缺少 $CONST_INC（仓库尚未纳入 phpweb？）"
fi

# ---------- 4. judge.conf 的 OJ_LANG_SET ----------
if [[ -f "$JUDGE_CONF" ]]; then
    set_line=$(grep -E '^OJ_LANG_SET=' "$JUDGE_CONF" | cut -d= -f2-)
    if [[ -z "$set_line" ]]; then
        fail "$JUDGE_CONF 未定义 OJ_LANG_SET"
    else
        bad=()
        for id in ${set_line//,/ }; do
            in_all=0
            for a in "${ids[@]}"; do [[ "$a" == "$id" ]] && in_all=1 && break; done
            [[ $in_all -eq 1 ]] || bad+=("$id")
        done
        if [[ ${#bad[@]} -gt 0 ]]; then
            fail "OJ_LANG_SET 含 all.toml 未登记的 id: ${bad[*]}"
        else
            ok "OJ_LANG_SET 的 id 均已在 all.toml 登记"
        fi
    fi
else
    echo "  - 跳过 OJ_LANG_SET 校验（未找到 $JUDGE_CONF）"
fi

# ---------- 5. $OJ_LANGMASK 禁用位 ----------
DB_INFO="$REPO/phpweb/include/db_info.inc.php"
if [[ -f "$DB_INFO" ]]; then
    mask=$(grep -oE '\$OJ_LANGMASK=[0-9]+' "$DB_INFO" | head -1 | cut -d= -f2)
    if [[ -z "$mask" ]]; then
        fail "db_info.inc.php 未定义 \$OJ_LANGMASK"
    else
        # 语义: 禁用位掩码。visible = ~mask & ((1<<lang_count)-1)
        # 其中 lang_count 取自 count($language_ext)，即 const.inc.php 校验出的 $exts_n
        width="${exts_n:-$lang_count}"
        visible=""
        hidden_judgeable=()
        visible_unjudgeable=()
        for ((i = 0; i < width; i++)); do
            has_cfg=0
            for c in "${configs[@]}"; do [[ "$c" == "$i" ]] && has_cfg=1 && break; done
            if (( (mask >> i) & 1 )); then
                # 已被屏蔽
                [[ $has_cfg -eq 1 ]] && hidden_judgeable+=("$i")
            else
                visible+="$i "
                [[ $has_cfg -eq 0 ]] && visible_unjudgeable+=("$i")
            fi
        done
        ok "\$OJ_LANGMASK=$mask（位宽 $width）→ 可见语言 id: ${visible% }"

        if [[ ${#hidden_judgeable[@]} -gt 0 ]]; then
            fail "有 judge 配置的语言被 \$OJ_LANGMASK 屏蔽（提交页不可选）: ${hidden_judgeable[*]}"
        else
            ok "所有具备 judge 配置的语言均未被 \$OJ_LANGMASK 屏蔽"
        fi

        # 反向: 可见但无 judge 配置 —— 用户可选却必然判题失败（OJ_SE）。
        # 这是生产既有缺陷，需运营方决定是补 Dockerfile 还是加入屏蔽位，故仅告警。
        if [[ ${#visible_unjudgeable[@]} -gt 0 ]]; then
            names=""
            for i in "${visible_unjudgeable[@]}"; do
                names+="${lang_name[$i]:-$i}($i) "
            done
            warn "以下语言在提交页可见但缺少 <id>.lang.toml，提交后必然 OJ_SE: ${names% }"
            fixed=$mask
            for i in "${visible_unjudgeable[@]}"; do fixed=$(( fixed | (1 << i) )); done
            warn "  处置二选一：① 补 extra/<id>/Dockerfile 与 langs/<id>.lang.toml；"
            warn "  ② 将这些位加入 \$OJ_LANGMASK 屏蔽：$mask → $fixed"
        else
            ok "提交页可见的语言均具备 judge 配置"
        fi
    fi
else
    echo "  - 跳过 \$OJ_LANGMASK 校验（未找到 $DB_INFO）"
fi

if [[ $rc -eq 0 ]]; then
    echo "===> 语言索引一致性校验通过"
else
    echo "===> 语言索引一致性校验失败"
fi
exit $rc
