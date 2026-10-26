#!/usr/bin/env bash
#
# 安装语言运行时：优先使用预构建 rootfs（免 Docker），回退本地 Docker 构建
#
# 用法：
#   sudo ./install_langs.sh [--ojhome /home/judge] [--use-prebuilt] [--prebuilt-url URL] [--prebuilt-dir DIR] [--lang ID...]
#
# 选项：
#   --ojhome DIR           OJ 根目录 (默认 /home/judge)
#   --use-prebuilt         优先尝试使用预构建 rootfs
#   --prebuilt-url URL     预构建 rootfs 下载基础 URL (默认: GitHub Release)
#   --prebuilt-dir DIR     本地预构建 rootfs 目录 (离线包内 langs/prebuilt/)
#   --lang ID              只安装指定语言 ID (可多次，默认安装所有可用)
#   --force-docker         强制使用 Docker 构建（忽略预构建）
#   -h, --help             显示帮助
set -euo pipefail

SRC_DIR="$(cd "$(dirname "$0")" && pwd)"
OJHOME=/home/judge
USE_PREBUILT=0
PREBUILT_URL=""
PREBUILT_DIR=""
FORCE_DOCKER=0
WANT_LANGS=()

info() { printf '==> %s\n' "$*"; }
warn() { printf '警告: %s\n' "$*" >&2; }
die()  { printf '错误: %s\n' "$*" >&2; exit 1; }

usage() {
    sed -n '2,/^set /p' "$0" | sed -e 's/^# \{0,1\}//' -e '/^set /d'
}

while [ $# -gt 0 ]; do
    case "$1" in
        --ojhome)       OJHOME="$2"; shift 2 ;;
        --use-prebuilt) USE_PREBUILT=1; shift ;;
        --prebuilt-url) PREBUILT_URL="$2"; shift 2 ;;
        --prebuilt-dir) PREBUILT_DIR="$2"; shift 2 ;;
        --lang)         WANT_LANGS+=("$2"); shift 2 ;;
        --force-docker) FORCE_DOCKER=1; shift ;;
        -h|--help)      usage; exit 0 ;;
        *)              die "未知参数: $1" ;;
    esac
done

# 语言 ID -> 名称映射（用于文件名匹配）
declare -A LANG_NAME=(
    [0]="c"       [1]="cpp"       [2]="pascal"  [3]="java"
    [5]="bash"    [6]="python"    [7]="php"     [8]="perl"
    [9]="csharp"  [16]="node"     [17]="go"     [25]="rust"
    # C++ 标准变体（共享 cpp rootfs，仅 PCH 目录不同）
    [26]="cpp11"  [27]="cpp17"    [28]="cpp20"  [29]="cpp23"
)

# 确定架构
ARCH="$(dpkg --print-architecture 2>/dev/null || uname -m)"
case "$ARCH" in
    x86_64|amd64) ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
esac

# 默认预构建下载 URL (GitHub Release)
if [ -z "$PREBUILT_URL" ] && [ "$USE_PREBUILT" -eq 1 ]; then
    # 尝试从 VERSION 文件读取版本
    VERSION=""
    for vf in "$SRC_DIR/../../VERSION" "$OJHOME/VERSION" "$SRC_DIR/VERSION"; do
        if [ -f "$vf" ] && grep -q '^version:' "$vf"; then
            VERSION=$(grep '^version:' "$vf" | cut -d: -f2- | tr -d ' ')
            break
        fi
    done
    [ -n "$VERSION" ] || VERSION="latest"
    PREBUILT_URL="https://github.com/sempr/hustoj-go/releases/download/rootfs-v${VERSION}"
fi

# 预构建目录搜索顺序
PREBUILT_SEARCH_DIRS=()
[ -n "$PREBUILT_DIR" ] && PREBUILT_SEARCH_DIRS+=("$PREBUILT_DIR")
PREBUILT_SEARCH_DIRS+=(
    "$SRC_DIR/prebuilt"                    # 离线包内 langs/prebuilt/
    "$SRC_DIR/../prebuilt"                 # 兼容旧结构
    "$OJHOME/var/prebuilt-rootfs"          # 运行时目录
    "/tmp/hustoj-prebuilt-rootfs"          # 临时下载目录
)

# 查找预构建 rootfs 文件
find_prebuilt() {
    local lang_name="$1"
    local file="rootfs-${lang_name}-${ARCH}.tar.gz"
    
    # 1. 本地目录查找
    for dir in "${PREBUILT_SEARCH_DIRS[@]}"; do
        if [ -f "$dir/$file" ]; then
            echo "$dir/$file"
            return 0
        fi
    done
    
    # 2. 远程下载
    if [ "$USE_PREBUILT" -eq 1 ] && [ -n "$PREBUILT_URL" ]; then
        local tmpdir="/tmp/hustoj-prebuilt-rootfs"
        mkdir -p "$tmpdir"
        if wget -q --timeout=30 --tries=2 "${PREBUILT_URL}/${file}" -O "${tmpdir}/${file}" 2>/dev/null; then
            # 校验 sha256
            if wget -q --timeout=10 --tries=1 "${PREBUILT_URL}/${file}.sha256" -O "${tmpdir}/${file}.sha256" 2>/dev/null; then
                if (cd "$tmpdir" && sha256sum -c "${file}.sha256" >/dev/null 2>&1); then
                    echo "${tmpdir}/${file}"
                    return 0
                else
                    warn "校验失败: ${file}"
                fi
            else
                warn "无校验和文件，跳过校验: ${file}"
                echo "${tmpdir}/${file}"
                return 0
            fi
        fi
    fi
    
    return 1
}

# 解压预构建 rootfs
extract_prebuilt() {
    local tarball="$1"
    local dest="$2"
    
    info "解压预构建 rootfs: $(basename "$tarball") -> $dest"
    rm -rf "$dest"
    mkdir -p "$dest"
    tar -xzf "$tarball" -C "$dest"
    
    # 清理 docker 注入文件
    rm -f "$dest/etc/hosts" "$dest/etc/hostname" "$dest/etc/resolv.conf"
}

# 生成 lang.toml（基于模板 + 预构建 rootfs 路径）
# C++ 变体 (26-29) 共享 cpp (id=1) 的基础 rootfs，仅设置各自的 pch_dir
generate_lang_toml() {
    local id="$1"
    local lang_name="${LANG_NAME[$id]}"
    local template="$SRC_DIR/etc/langs/${id}.lang.toml"
    local output="$OJHOME/etc/langs/${id}.lang.toml"
    
    # C++ 标准变体：共享 cpp 的基础 rootfs，指定各自的 PCH 目录
    local rootfs_dir
    local pch_dir=""
    case "$id" in
        26) rootfs_dir="$OJHOME/var/rootfs/1"; pch_dir="/opt/hustoj/langs/cpp-pch/cpp11" ;;  # C++11
        27) rootfs_dir="$OJHOME/var/rootfs/1"; pch_dir="/opt/hustoj/langs/cpp-pch/cpp17" ;;  # C++17
        28) rootfs_dir="$OJHOME/var/rootfs/1"; pch_dir="/opt/hustoj/langs/cpp-pch/cpp20" ;;  # C++20
        29) rootfs_dir="$OJHOME/var/rootfs/1"; pch_dir="/opt/hustoj/langs/cpp-pch/cpp23" ;;  # C++23
        *)  rootfs_dir="$OJHOME/var/rootfs/$id" ;;
    esac
    
    if [ ! -f "$template" ]; then
        # C++ 变体没有独立模板，基于 cpp (id=1) 模板生成
        if [[ "$id" =~ ^(26|27|28|29)$ ]]; then
            template="$SRC_DIR/etc/langs/1.lang.toml"
        else
            warn "模板不存在: $template，跳过 $lang_name"
            return 1
        fi
    fi
    
    info "生成配置: $output (base=$rootfs_dir, pch_dir=$pch_dir)"
    
    # 读取模板，替换 base 路径，添加 pch_dir
    local content
    content=$(cat "$template")
    content=$(echo "$content" | sed "s|^base = .*|base = \"$rootfs_dir\"|")
    
    # 添加/更新 pch_dir (仅 C++ 变体)
    if [ -n "$pch_dir" ]; then
        if echo "$content" | grep -q '^pch_dir ='; then
            content=$(echo "$content" | sed "s|^pch_dir = .*|pch_dir = \"$pch_dir\"|")
        else
            # 在 [fs] section 后插入 pch_dir
            content=$(echo "$content" | sed "/^\[fs\]/a pch_dir = \"$pch_dir\"")
        fi
    fi
    
    mkdir -p "$(dirname "$output")"
    echo "$content" > "$output"
    
    if id judge >/dev/null 2>&1; then
        chown judge:judge "$output" 2>/dev/null || true
    fi
}

# 回退：Docker 本地构建
build_with_docker() {
    info "回退 Docker 本地构建..."
    command -v docker >/dev/null 2>&1 || die "未找到 docker，且无可用预构建 rootfs"
    
    ( cd "$SRC_DIR" && LANG_ROOTFS_DIR="$OJHOME/var/rootfs" bash prepare_langs.sh )
}

# ===== 主流程 =====

info "开始安装语言运行时 (OJHOME=$OJHOME, ARCH=$ARCH)"
[ "$USE_PREBUILT" -eq 1 ] && info "预构建模式: 开启 (URL: ${PREBUILT_URL:-无})"
[ "$FORCE_DOCKER" -eq 1 ] && info "强制 Docker 模式"

mkdir -p "$OJHOME/var/rootfs" "$OJHOME/etc/langs"

# 确定要安装的语言列表
INSTALL_LANGS=()
if [ "${#WANT_LANGS[@]}" -gt 0 ]; then
    for spec in "${WANT_LANGS[@]}"; do
        # 支持 ID 或名称
        found=0
        for id in "${!LANG_NAME[@]}"; do
            if [ "$spec" = "$id" ] || [ "$spec" = "${LANG_NAME[$id]}" ]; then
                INSTALL_LANGS+=("$id")
                found=1
                break
            fi
        done
        [ "$found" -eq 1 ] || die "未知语言: $spec"
    done
else
    # 安装所有有模板的语言
    for f in "$SRC_DIR"/etc/langs/*.lang.toml; do
        [ -f "$f" ] || continue
        id=$(basename "$f" .lang.toml)
        [[ "$id" =~ ^[0-9]+$ ]] && INSTALL_LANGS+=("$id")
    done
fi

# 处理每个语言
installed=()
for id in "${INSTALL_LANGS[@]}"; do
    lang_name="${LANG_NAME[$id]:-}"
    [ -n "$lang_name" ] || { warn "未知语言 ID: $id"; continue; }
    
    rootfs_dir="$OJHOME/var/rootfs/$id"
    
    if [ "$FORCE_DOCKER" -eq 0 ] && [ "$USE_PREBUILT" -eq 1 ]; then
        # 尝试预构建
        if tarball=$(find_prebuilt "$lang_name"); then
            extract_prebuilt "$tarball" "$rootfs_dir"
            generate_lang_toml "$id"
            installed+=("$id")
            continue
        else
            warn "预构建不可用: $lang_name ($ARCH)"
        fi
    fi
    
    # 回退 Docker 构建（只构建当前语言）
    info "Docker 构建语言: $lang_name"
    dockerfile="$SRC_DIR/$id/Dockerfile"
    lang_toml="$SRC_DIR/etc/langs/${id}.lang.toml"
    
    if [ -f "$dockerfile" ] && [ -f "$lang_toml" ]; then
        # 单独构建该语言
        image="hustoj:lang-${id}"
        docker build --platform "linux/$ARCH" -t "$image" "$SRC_DIR/$id"
        LANG_ROOTFS_DIR="$OJHOME/var/rootfs" bash "$SRC_DIR/replace.sh" "$image" "$lang_toml"
        # 复制生成的 lang.toml 到目标位置
        cp -f "$lang_toml" "$OJHOME/etc/langs/${id}.lang.toml"
        installed+=("$id")
    else
        warn "缺少构建文件: $dockerfile 或 $lang_toml，跳过 $lang_name"
    fi
done

# 安装 all.toml
cp -f "$SRC_DIR/etc/langs/all.toml" "$OJHOME/etc/langs/" 2>/dev/null || true

# 更新 judge.conf
if [ "${#installed[@]}" -gt 0 ] && [ -f "$OJHOME/etc/judge.conf" ]; then
    lang_ids=$(printf '%s\n' "${installed[@]}" | sort -n | paste -sd, -)
    tmp="$(mktemp)"
    grep -v '^OJ_LANG_SET=' "$OJHOME/etc/judge.conf" > "$tmp" || true
    printf 'OJ_LANG_SET=%s\n' "$lang_ids" >> "$tmp"
    install -m 0600 "$tmp" "$OJHOME/etc/judge.conf"
    rm -f "$tmp"
    info "已更新 $OJHOME/etc/judge.conf 的 OJ_LANG_SET=$lang_ids"
fi

# 刷新语言信息缓存
if command -v hustoj >/dev/null 2>&1; then
    hustoj langinfo --ojhome "$OJHOME" || true
fi

printf '\n语言安装完成。已安装: %s\n' "${installed[*]}"
printf '重启判题服务使其生效：systemctl restart judged-go.service\n'