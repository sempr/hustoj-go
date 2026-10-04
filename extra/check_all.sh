#!/usr/bin/env bash
# 全量质量门禁：Go 与 PHP 两侧一起跑。
#
# 用法: extra/check_all.sh
# 退出码 0 = 全部通过。
#
# 与 CI 使用同一条路径，避免"本地过了 CI 挂了"或反之。
set -uo pipefail

cd "$(dirname "$0")/.."
ROOT="$PWD"

fail=0
declare -a FAILED=()

step() {
    local name="$1"; shift
    printf '\n=== %s ===\n' "$name"
    if "$@"; then
        printf '  ✓ %s\n' "$name"
    else
        printf '  ✗ %s\n' "$name"
        fail=1
        FAILED+=("$name")
    fi
}

need() {
    command -v "$1" >/dev/null 2>&1 || { echo "缺少依赖: $1"; exit 2; }
}
need go
need php

# ── Go ───────────────────────────────────────────────────────────────
gofmt_clean() {
    local out
    out=$(gofmt -l ./cmd ./pkg ./internal)
    if [ -n "$out" ]; then
        echo "$out"
        return 1
    fi
    return 0
}

step "gofmt"          gofmt_clean
step "go build"       go build ./...
step "go vet"         go vet ./...

if command -v staticcheck >/dev/null 2>&1; then
    step "staticcheck" staticcheck ./...
else
    printf '\n=== staticcheck ===\n  - 已跳过（未安装）\n'
fi

step "go test"        go test ./...

# ── PHP ──────────────────────────────────────────────────────────────
# php -l 扫描。已知且已归档的既有错误：
#   template/sidebar/problem1.php —— 生产遗留、零引用，迁移时整体排除。
php_lint() {
    local e=0 f
    while IFS= read -r f; do
        if ! php -l "$f" >/dev/null 2>&1; then
            echo "  语法错误: ${f#"$ROOT"/}"
            e=1
        fi
    done < <(find phpweb extra -name '*.php')
    return $e
}

step "php -l"         php_lint
step "PHP 变量插值"   php extra/check_php_interpolation.php phpweb
step "PHP 变量插值"   php extra/check_php_interpolation.php extra
step "CSRF 回归"      php extra/test_csrf.php
step "语言索引"       bash extra/check_lang_index.sh

# ── schema / codegen ──────────────────────────────────────────────────
# 这两项需要能连到数据库 / 需要 sqlc，本地缺失时跳过而非报错；
# CI 里两者都必须真实执行（见 .github/workflows/ci.yml）。
if [ "${SKIP_SCHEMA_CHECKS:-0}" != "1" ]; then
    if command -v mysql >/dev/null 2>&1; then
        step "schema 一致性" python3 extra/check_schema.py
    else
        printf '\n=== schema 一致性 ===\n  - 已跳过（未安装 mysql 客户端）\n'
    fi

    # 生成的代码必须与 schema/queries 同步。改了查询却忘了 generate，
    # 代码看起来能编译，跑起来才是旧行为 —— 这类错只有靠 diff 才能发现。
    if command -v sqlc >/dev/null 2>&1; then
        sqlc_codegen_fresh() {
            local before
            before=$(mktemp -d)
            # cp -r pkg/dbgen "$before/" 得到的是 "$before/dbgen"，
            # 比对时要跟它对齐。直接 diff "$before" pkg/dbgen 永远会多出一层
            # 目录差异（第一版就踩了这个坑，报了假的"不同步"）。
            cp -r pkg/dbgen "$before/"
            if ! sqlc generate; then
                echo "  sqlc generate 失败"
                rm -rf "$before"; return 1
            fi
            if ! diff -ru "$before/dbgen" pkg/dbgen >/dev/null 2>&1; then
                echo "  pkg/dbgen 与 schema/queries 不同步，请运行 sqlc generate"
                diff -rq "$before/dbgen" pkg/dbgen | sed 's/^/    /'
                rm -rf "$before"; return 1
            fi
            rm -rf "$before"
            return 0
        }
        step "sqlc codegen 同步" sqlc_codegen_fresh
    else
        printf '\n=== sqlc codegen 同步 ===\n  - 已跳过（未安装 sqlc）\n'
    fi
fi

# ── 汇总 ─────────────────────────────────────────────────────────────
printf '\n────────────────────────────────\n'
if [ "$fail" -eq 0 ]; then
    printf '全部通过\n'
else
    printf '失败项: %s\n' "${FAILED[*]}"
fi
exit "$fail"
