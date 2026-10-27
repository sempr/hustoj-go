#!/bin/bash
#
# 构建单个语言镜像，把镜像 rootfs 拍平成目录，并改写 etc/langs/<id>.lang.toml。
#
# rootfs 落到 LANG_ROOTFS_DIR（默认 <root>/var/rootfs/<id>），可用环境变量覆盖；
# 判题/单测直接用该目录作为 overlay 的 lowerdir。
#
# 用法: build_rootfs.sh <lang_id>
set -e

name="${1:-0}"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

[ -f "$name/Dockerfile" ] || { echo "找不到 $name/Dockerfile" >&2; exit 2; }

docker build -t "hustoj:lang-${name}" "$name"
bash replace.sh "hustoj:lang-${name}" "etc/langs/${name}.lang.toml"
