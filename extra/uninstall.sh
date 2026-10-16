#!/usr/bin/env bash
#
# hustoj-go 卸载脚本（install.sh 的反向操作）。
#
# 默认只移除二进制与 systemd unit，保留数据、配置与语言 rootfs；
# 加 --purge 才会删除安装时写入的配置（web.toml / judge.conf / all.toml）。
#
# 用法：
#   sudo ./uninstall.sh [选项]
#
# 选项：
#   --prefix DIR       二进制目录（默认 /usr/bin）
#   --ojhome DIR       运行数据根目录（默认 /home/judge）
#   --tini-dir DIR     tini 目录（默认 /usr/local/bin）
#   --systemd-dir DIR  systemd unit 目录（默认 /etc/systemd/system）
#   --purge            同时删除 web.toml / judge.conf / etc/langs/all.toml
#   --dry-run          只打印将要执行的动作
#   -h, --help
set -euo pipefail

PREFIX=/usr/bin
OJHOME=/home/judge
TINI_DIR=/usr/local/bin
SYSDIR=/etc/systemd/system
PURGE=0
DRY=0

die()  { printf '错误: %s\n' "$*" >&2; exit 1; }
info() { printf '==> %s\n' "$*"; }
run() {
    if [ "$DRY" -eq 1 ]; then
        printf '  [dry-run] %s\n' "$*"
    else
        "$@"
    fi
}

usage() { sed -n '3,18p' "$0" | sed 's/^# \{0,1\}//'; }

while [ $# -gt 0 ]; do
    case "$1" in
        --prefix)       PREFIX="$2"; shift 2 ;;
        --ojhome)       OJHOME="$2"; shift 2 ;;
        --tini-dir)     TINI_DIR="$2"; shift 2 ;;
        --systemd-dir)  SYSDIR="$2"; shift 2 ;;
        --purge)        PURGE=1; shift ;;
        --dry-run)      DRY=1; shift ;;
        -h|--help)      usage; exit 0 ;;
        *)              die "未知参数: $1（用 --help 查看用法）" ;;
    esac
done

if [ "$DRY" -ne 1 ] && [ "$(id -u)" -ne 0 ]; then
    die "请以 root 运行（sudo ./uninstall.sh）"
fi

if [ "$DRY" -ne 1 ] && command -v systemctl >/dev/null 2>&1; then
    info "停止并禁用服务"
    run systemctl disable --now hustoj-web.service 2>/dev/null || true
    run systemctl disable --now judged-go.service 2>/dev/null || true
fi

info "移除 systemd unit"
run rm -f "$SYSDIR/hustoj-web.service" "$SYSDIR/judged-go.service"
if [ "$DRY" -ne 1 ] && command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload || true
fi

info "移除二进制"
run rm -f "$PREFIX/hustoj" "$TINI_DIR/tini"

if [ "$PURGE" -eq 1 ]; then
    info "清除配置文件（--purge）"
    run rm -f "$OJHOME/etc/web.toml" "$OJHOME/etc/judge.conf" "$OJHOME/etc/langs/all.toml"
    info "数据目录与语言 rootfs 一律保留：$OJHOME/data $OJHOME/etc/langs"
fi

printf '\n卸载完成。\n'
