#!/usr/bin/env bash
#
# 在目标机用 Docker 构建各语言运行时镜像，并把生成的 <id>.lang.toml 装到
# $OJHOME/etc/langs/。判题时把镜像的 overlay 层当作 rootfs 挂载，因此生成的
# *.lang.toml 与本机 docker 的层路径强绑定，必须在目标机本地构建。
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
            sed -n '3,10p' "$0" | sed 's/^# \{0,1\}//'
            exit 0 ;;
        *) die "未知参数: $1" ;;
    esac
done

command -v docker >/dev/null 2>&1 || die "未找到 docker"

info "构建语言镜像并重写 etc/langs/*.lang.toml（较慢，取决于网络）"
( cd "$SRC_DIR" && bash prepare_langs.sh )

info "安装语言配置到 $OJHOME/etc/langs"
mkdir -p "$OJHOME/etc/langs"
cp -f "$SRC_DIR"/etc/langs/all.toml "$OJHOME/etc/langs/" 2>/dev/null || true
cp -f "$SRC_DIR"/etc/langs/*.lang.toml "$OJHOME/etc/langs/"
if id judge >/dev/null 2>&1; then
    chown judge:judge "$OJHOME/etc/langs"/*.toml 2>/dev/null || true
fi

# 刷新 FAQ 语言信息缓存（best-effort）：让站点立即看到新装语言。
if command -v hustoj >/dev/null 2>&1; then
    hustoj langinfo --ojhome "$OJHOME" || true
fi

printf '\n语言安装完成。重启判题服务使其生效：systemctl restart judged-go.service\n'
