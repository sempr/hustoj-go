#!/usr/bin/env bash
#
# 在目标机用 Docker 构建各语言运行时镜像，把镜像 rootfs「拍平」成目录、生成
# <id>.lang.toml，并装到 $OJHOME/etc/langs/。判题把该目录当 overlay 的 lowerdir
# 挂载，因此 rootfs 与本机强绑定，必须在目标机本地构建（不能随包分发）。
#
# 需要：docker、可访问 apt/npm/go 等上游（受各 Dockerfile 控制）、root 或 docker 组。
#
# 用法（在包内的 langs/ 目录执行）：
#   sudo ./install_langs.sh [--ojhome /home/judge]
set -euo pipefail

SRC_DIR="$(cd "$(dirname "$0")" && pwd)"
OJHOME=/home/judge

info() { printf '==> %s\n' "$*"; }
die()  { printf '错误: %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
    case "$1" in
        --ojhome) OJHOME="$2"; shift 2 ;;
        -h|--help)
            sed -n '2,/^set /p' "$0" | sed -e 's/^# \{0,1\}//' -e '/^set /d'
            exit 0 ;;
        *) die "未知参数: $1" ;;
    esac
done

command -v docker >/dev/null 2>&1 || die "未找到 docker"

info "构建语言镜像、拍平 rootfs 并重写 etc/langs/*.lang.toml（较慢，取决于网络）"
# LANG_ROOTFS_DIR 让 replace.sh 把拍平后的 rootfs 直接落到运行时目录，省一次拷贝。
# 放在 <ojhome>/var/rootfs（变量数据），与 etc/ 下的配置分开。
( cd "$SRC_DIR" && LANG_ROOTFS_DIR="$OJHOME/var/rootfs" bash prepare_langs.sh )

info "安装语言配置到 $OJHOME/etc/langs"
mkdir -p "$OJHOME/etc/langs"
cp -f "$SRC_DIR"/etc/langs/all.toml "$OJHOME/etc/langs/" 2>/dev/null || true
cp -f "$SRC_DIR"/etc/langs/*.lang.toml "$OJHOME/etc/langs/"
# 兼容旧版安装：早期 *.lang.toml 的 base 可能指向 Docker overlay2 镜像层。
# 镜像层被清理后判题会报 "language rootfs not found"；统一改指拍平目录。
for f in "$OJHOME"/etc/langs/*.lang.toml; do
    [ -f "$f" ] || continue
    id="$(basename "$f" .lang.toml)"
    root="$OJHOME/var/rootfs/$id"
    if [ -d "$root" ] && grep -q '^base *= *"/var/lib/docker/' "$f"; then
        tmp="$(mktemp)"
        sed "s|^base *=.*|base = \"$root\"|" "$f" > "$tmp"
        install -m 0644 "$tmp" "$f"
        rm -f "$tmp"
        info "修正 $id.lang.toml 的 base -> $root"
    fi
done
if id judge >/dev/null 2>&1; then
    chown judge:judge "$OJHOME/etc/langs"/*.toml 2>/dev/null || true
fi

# 判题守护进程要求 judge.conf 显式声明受理语言（不许回退默认值）。
# 这里按实际安装的 <id>.lang.toml 生成，避免装完语言后 judged-go 起不来。
lang_ids="$(for f in "$OJHOME"/etc/langs/*.lang.toml; do
    [ -f "$f" ] || continue
    basename "$f" .lang.toml
done | sort -n | paste -sd, -)"
if [ -n "$lang_ids" ] && [ -f "$OJHOME/etc/judge.conf" ]; then
    tmp="$(mktemp)"
    grep -v '^OJ_LANG_SET=' "$OJHOME/etc/judge.conf" > "$tmp" || true
    printf 'OJ_LANG_SET=%s\n' "$lang_ids" >> "$tmp"
    install -m 0600 "$tmp" "$OJHOME/etc/judge.conf"
    rm -f "$tmp"
    info "已更新 $OJHOME/etc/judge.conf 的 OJ_LANG_SET=$lang_ids"
fi

# 刷新 FAQ 语言信息缓存（best-effort）：让站点立即看到新装语言。
if command -v hustoj >/dev/null 2>&1; then
    hustoj langinfo --ojhome "$OJHOME" || true
fi

printf '\n语言安装完成。重启判题服务使其生效：systemctl restart judged-go.service\n'
