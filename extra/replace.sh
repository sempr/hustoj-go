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
# env 合并约定：<id>.lang.toml 的 env 只写「镜像 ENV 里没有的 key」(delta)。
# 镜像已有的 key 一律以【镜像值】为准，避免「上次构建把镜像 ENV 写回模板、下次
# 构建又用旧值反向覆盖新版本镜像」的冻结问题。确需覆盖镜像值时设
# ALLOW_ENV_OVERRIDE=1 显式开启。
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

# 告警输出（写 stderr；stderr 是终端时加颜色）。
warn_env() {
	if [ -t 2 ]; then
		printf '\033[33m[replace.sh][env] %s\033[0m\n' "$*" >&2
	else
		printf '[replace.sh][env] %s\n' "$*" >&2
	fi
}

# 模板 env 只作为「镜像没有的 key」的 delta 合并；镜像已有的 key 以镜像值为准。
manual=$(sed -n 's/.*env[[:space:]]*=[[:space:]]*\[\(.*\)\][[:space:]]*$/\1/p' "$conf")
if [ -n "$manual" ]; then
	while IFS= read -r t; do
		t=${t//\"/}
		t=$(printf '%s' "$t" | sed 's/^[[:space:]]*//; s/[[:space:]]*$//')
		[ -n "$t" ] || continue
		key=${t%%=*}
		val=${t#*=}
		[ -n "$key" ] || continue

		if [ -z "${envmap[$key]+x}" ]; then
			# 镜像没有：真正的 delta，采纳
			linear+=("$key")
			envmap[$key]=$val
			# 顺带体检：delta 值是否像「钉死版本 / 绝对库路径」
			looks_pinned=0
			case "$val" in
				/*)
					if printf '%s' "$val" | grep -Eq '(^|/)[A-Za-z0-9._-]*([0-9]+\.[0-9]+|[A-Za-z_]+-[0-9]+)[A-Za-z0-9._-]*(/|$)'; then
						looks_pinned=1
					fi
					;;
			esac
			if printf '%s' "$key" | grep -Eq '(_VERSION|_SHA|LIBRARY_PATH)$'; then
				looks_pinned=1
			fi
			if [ "$looks_pinned" = 1 ]; then
				warn_env "delta '$key=$val' 疑似钉死版本/绝对库路径，基础镜像升级后易失效；建议改由镜像提供，或放进 rootfs 内的 wrapper"
			fi
		elif [ "${envmap[$key]}" = "$val" ]; then
			# 与镜像值一致：保留镜像值，无需动作
			:
		elif [ -n "${ALLOW_ENV_OVERRIDE:-}" ]; then
			warn_env "按 ALLOW_ENV_OVERRIDE 覆盖镜像值: $key 镜像='${envmap[$key]}' -> 模板='$val'"
			envmap[$key]=$val
		else
			warn_env "冲突: $key 镜像='${envmap[$key]}' vs 模板='$val'；已按【镜像值】为准（防止旧值冻结）。确需覆盖请设 ALLOW_ENV_OVERRIDE=1"
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
