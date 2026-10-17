#!/bin/bash
#
# 为语言镜像生成 rootfs，并写入 <id>.lang.toml 的 base / env。
#
# 采用「拍平成目录」：不再依赖 docker 的 graphdriver 层目录
# (/var/lib/docker/overlay2/<id>/diff)。Docker Engine 29 起全新安装默认改用
# containerd image store，镜像不再有 per-image 的 diff 目录（docker inspect 的
# GraphDriver.Data 为空），老做法会写出空 base，判题时 ensureRootfsReady 直接报
# "language rootfs not found"。这里改用 docker export 把镜像拍平成一个普通目录，
# base 指向该目录——overlayfs 的 lowerdir 允许单个目录，且与镜像存储后端无关
# （overlay2 / containerd 两种后端都能用）。
#
# rootfs 目录：优先取环境变量 LANG_ROOTFS_DIR，否则落在 <root>/var/rootfs/<id>
# （<root> 为 lang.toml 的 ../../，即 etc/langs 所属的那一层，与 OJHOME 布局一致）。
# install_langs.sh 会把 LANG_ROOTFS_DIR 设为 $OJHOME/var/rootfs，直接落到运行时
# 位置，避免装完再拷一份。
#
# 用法: replace.sh <image> <lang.toml>
set -e
set -o pipefail

img="${1:-}"
conf="${2:-}"
if [ -z "$img" ] || [ -z "$conf" ]; then
	echo "用法: $0 <image> <lang.toml>" >&2
	exit 2
fi
[ -f "$conf" ] || { echo "找不到配置: $conf" >&2; exit 2; }
command -v docker >/dev/null 2>&1 || { echo "未找到 docker" >&2; exit 1; }

id="$(basename "$conf" .lang.toml)"
conf_dir="$(cd "$(dirname "$conf")" && pwd)"
rootfs_root="${LANG_ROOTFS_DIR:-$(cd "$conf_dir/../.." && pwd)/var/rootfs}"
dest="$rootfs_root/$id"

echo "==> 拍平 $img -> $dest"
rm -rf "$dest"
mkdir -p "$dest"

# docker export 导出的是「容器已合并的文件系统」（白色删除已生效），比
# docker save + 逐层解包可靠；对 overlay2 / containerd 两种后端都适用。
cid="$(docker create "$img")"
cleanup() { if [ -n "$cid" ]; then docker rm -f "$cid" >/dev/null 2>&1 || true; fi; }
trap cleanup EXIT

docker export "$cid" | tar -x -C "$dest"
docker rm -f "$cid" >/dev/null 2>&1
cid=""

# 去掉 docker 注入的空占位文件；原 graphdriver 路线只用镜像层，本来就没有它们。
rm -f "$dest/etc/hosts" "$dest/etc/hostname" "$dest/etc/resolv.conf"

V="base = \"$dest\""
echo "$V"

env_json=$(docker inspect "$img" --format '{{json .Config.Env}}')

declare -A envmap=()
linear=()

while IFS= read -r tok; do
	key=${tok%%=*}
	val=${tok#*=}
	if [ -n "$key" ] && [ -z "${envmap[$key]+x}" ]; then
		linear+=("$key")
	fi
	envmap[$key]=$val
done < <(printf '%s' "$env_json" | grep -o '"[^"]*"' | tr -d '"')

manual=$(sed -n 's/.*env[[:space:]]*=[[:space:]]*\[\(.*\)\][[:space:]]*$/\1/p' "$conf")
	if [ -n "$manual" ]; then
		while IFS= read -r t; do
			t=${t//\"/}
			t=$(printf '%s' "$t" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')
			[ -n "$t" ] || continue
			key=${t%%=*}
			val=${t#*=}
			if [ -n "$key" ]; then
				if [ -z "${envmap[$key]+x}" ]; then
					linear+=("$key")
				fi
				envmap[$key]=$val
			fi
		done < <(printf '%s\n' "$manual" | tr ',' '\n')
	fi

env_items=()
for key in "${linear[@]}"; do
	env_items+=("\"$key=${envmap[$key]}\"")
done

env_line="env = [$(IFS=,; echo "${env_items[*]}")]"
esc=$(printf '%s' "$env_line" | sed 's/&/\\&/g; s/\\/\\\\/g')

sed -i.bak \
	-e "s|^base = .*|${V}|g" \
	-e "s|^env = .*|${esc}|" \
	"$conf"

echo "==> 完成: $conf"
