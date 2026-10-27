#!/usr/bin/env bash
#
# 为示例题编译判题程序（SPJ / 交互器）。目标机没有编译器，故在打包机按目标
# arch 交叉编译，产物随部署包分发（与 tini 的处理一致）。
#
# 用法：build_data.sh <goarch> <demo-dir>
#   <goarch>    amd64 / arm64 / 386 …
#   <demo-dir>  含 problems/ 的目录
#
# 编译映射取自各题 data/BUILD 的每一行：<源文件> <输出名> <cc|cxx>
# 缺交叉编译器时告警并跳过该产物（不中断打包）。
set -euo pipefail

GOARCH="${1:?用法: build_data.sh <goarch> <demo-dir>}"
DEMO_DIR="${2:?用法: build_data.sh <goarch> <demo-dir>}"

info() { printf '==> %s\n' "$*"; }
warn() { printf '警告: %s\n' "$*" >&2; }

HOST_GOARCH="$(go env GOARCH 2>/dev/null || echo amd64)"

pick_compiler() { # <goarch> <cc|cxx> -> 打印编译器路径；找不到返回非零
    local arch="$1" kind="$2" prefix=""
    if [ "$arch" != "$HOST_GOARCH" ]; then
        case "$arch" in
            amd64) prefix=x86_64-linux-gnu- ;;
            arm64) prefix=aarch64-linux-gnu- ;;
            386)   prefix=i686-linux-gnu- ;;
            *)     return 1 ;;
        esac
    fi
    if [ "$kind" = cxx ]; then
        command -v "${prefix}g++"
    else
        command -v "${prefix}gcc" || command -v "${prefix}cc"
    fi
}

rc=0
for buildfile in "$DEMO_DIR"/problems/*/data/BUILD; do
    [ -f "$buildfile" ] || continue
    dir="$(dirname "$buildfile")"
    while read -r src out kind _rest; do
        [ -n "${src:-}" ] || continue
        case "$src" in \#*) continue ;; esac
        if [ -z "${out:-}" ] || [ -z "${kind:-}" ]; then
            warn "BUILD 行格式错误（应为 <源文件> <输出名> <cc|cxx>）: $buildfile"
            rc=1
            continue
        fi
        # 先删旧产物：工作区里可能残留上一次（别的架构）编出来的二进制，
        # 若本次编不出来还留着，就会把错误的架构打进包里。
        rm -f "$dir/$out"
        compiler="$(pick_compiler "$GOARCH" "$kind" || true)"
        if [ -z "$compiler" ]; then
            warn "$GOARCH: 缺少 $kind 交叉编译器，跳过 $out（$dir）"
            rc=1
            continue
        fi
        "$compiler" -static -O2 -o "$dir/$out" "$dir/$src"
        chmod 0755 "$dir/$out"
        info "  编译 $(basename "$(dirname "$dir")")/$out <- $src"
    done < "$buildfile"
done

exit "$rc"
