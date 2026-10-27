#!/bin/bash
#
# 逐个构建 langs/<id>/Dockerfile 并调用 replace.sh 拍平 rootfs、改写 lang.toml。
# 可用 LANG_ROOTFS_DIR 指定拍平目录（install_langs.sh 会设成 $OJHOME/var/rootfs）。
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$SCRIPT_DIR"

for dir in */; do
    id="${dir%/}"
    [ "$id" = "base" ] && continue
    [ -f "$id/Dockerfile" ] || continue
    [ -f "etc/langs/${id}.lang.toml" ] || continue

    echo "===> building image hustoj:lang-${id}"
    docker build -t "hustoj:lang-${id}" "$id"
    bash replace.sh "hustoj:lang-${id}" "etc/langs/${id}.lang.toml"
done