#!/usr/bin/env bash
#
# 统一预构建 rootfs 构建脚本
# 基于 debian:slim 为基础镜像（部分语言需官方镜像）
# 用法：
#   ./build_prebuilt_rootfs.sh [--lang <lang>] [--arch <arch>] [--push] [--load]
#   ./build_prebuilt_rootfs.sh --all --arch amd64 --load
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OUT_DIR="$ROOT/dist/prebuilt-rootfs"

# 语言映射：ID -> { name, base_image, dockerfile_subdir, extra_packages, env_setup }
# 基础镜像统一使用 debian:13-slim（与现有 Dockerfile 一致），仅 Java/Node 需官方镜像
declare -A LANG_NAME=(
    [0]="c"       [1]="cpp"     [2]="pascal"  [3]="java"
    [5]="bash"    [6]="python"  [7]="php"     [8]="perl"
    [9]="csharp"  [16]="node"   [17]="go"     [25]="rust"
)

declare -A BASE_IMAGE=(
    [0]="debian:13-slim"      # C
    [1]="debian:13-slim"      # C++
    [2]="debian:13-slim"      # Pascal
    [3]="debian:13-slim"      # Java (统一 debian，apt 装 openjdk-21)
    [5]="debian:13-slim"      # Bash
    [6]="debian:13-slim"      # Python (统一 debian，apt 装 python3)
    [7]="debian:13-slim"      # PHP
    [8]="debian:13-slim"      # Perl
    [9]="debian:13-slim"      # C# (mono)
    [16]="debian:13-slim"     # Node.js (统一 debian，nodesource 源装 LTS)
    [17]="debian:13-slim"     # Go (从源码安装)
    [25]="debian:13-slim"     # Rust (从源码安装)
)

declare -A EXTRA_PACKAGES=(
    [0]="gcc g++ libc6-dev"                           # C
    [1]="gcc g++ libc6-dev"                           # C++
    [2]="fpc fp-compiler"                             # Pascal
    [3]="default-jdk"                                 # Java (OpenJDK，debian 13 默认 21，25 需 backports)
    [5]="bash"                                        # Bash
    [6]="python3 python3-pip python3-venv"            # Python (debian 13 默认 3.13，3.15 需 backports/源码)
    [7]="php-cli"                                     # PHP
    [8]="perl"                                        # Perl
    [9]="mono-mcs mono-runtime"                       # C# (mono)
    [16]=""                                           # Node.js (通过 nodesource 源安装，见 SPECIAL_INSTALL)
    [17]="golang-go"                                  # Go (apt 装旧版，再源码升级)
    [25]="rustc cargo"                                # Rust
)

declare -A POST_INSTALL=(
    [0]="g++ -x c++-header -O2 -std=c++23 \$(find /usr/include/ -name 'stdc++.h' 2>/dev/null | head -1) 2>/dev/null || true"
    [1]="g++ -x c++-header -fno-asm -Wall -O2 -std=c++14 -DONLINE_JUDGE \$(find /usr/include/ -name 'stdc++.h' 2>/dev/null | head -1) 2>/dev/null || true && echo '#include <bits/stdc++.h>' | g++ -x c++-header -fno-asm -Wall -O2 -std=c++14 -DONLINE_JUDGE -o /dev/null - 2>/dev/null || true"
    [2]=""
    [3]="ln -sf /usr/lib/jvm/default-java/bin/javac /usr/bin/javac && ln -sf /usr/lib/jvm/default-java/bin/java /usr/bin/java"
    [5]=""
    [6]="ln -sf /usr/bin/python3 /usr/bin/python"
    [7]="mkdir -p /code /tmp"
    [8]=""
    [9]=""                                             # C# mono 无需额外链接
    [16]="npm install -g typescript ts-node esbuild"
    [17]="mkdir -p /go-cache && GOCACHE=/go-cache GOFLAGS='-buildvcs=false' go build -o /dev/null -v std 2>/dev/null || true && GOCACHE=/go-cache GOFLAGS='-buildvcs=false' go build -o /dev/null -v runtime 2>/dev/null || true"
    [25]="ln -sf /usr/bin/rustc /usr/local/bin/rustc && mkdir -p /rust-cache && cd /tmp && cargo new --bin rust_warmup 2>/dev/null && cd rust_warmup && CARGO_TARGET_DIR=/rust-cache cargo build --release 2>/dev/null || true && rm -rf /tmp/rust_warmup"
)

# ========== 版本号集中管理（2026-10-26 实际最新稳定版，可通过环境变量/参数覆盖）==========
# 支持版本回退：export GO_VERSION=1.22.5 && ./build_prebuilt_rootfs.sh --lang go
# 或：./build_prebuilt_rootfs.sh --lang go --go-version 1.22.5
GO_VERSION="${GO_VERSION:-1.27.3}"          # Go 官方最新稳定
RUST_VERSION="${RUST_VERSION:-1.99.1}"      # Rust 官方最新稳定
NODE_MAJOR="${NODE_MAJOR:-24}"              # Node.js 24 LTS (官方源)

# 版本回退参数（命令行优先级最高）
OVERRIDE_GO_VERSION=""
OVERRIDE_RUST_VERSION=""
OVERRIDE_NODE_MAJOR=""

# 特殊安装：Node.js/Go/Rust 从官方源安装最新稳定版
declare -A SPECIAL_INSTALL=(
    [16]="curl -fsSL https://deb.nodesource.com/setup_${NODE_MAJOR}.x | bash - && apt-get install -y --no-install-recommends nodejs && rm -rf /var/lib/apt/lists/*"
    [17]="ARCH=\$(dpkg --print-architecture) && case \$ARCH in amd64) GOARCH=amd64;; arm64) GOARCH=arm64;; esac && wget -q https://go.dev/dl/go${GO_VERSION}.linux-\${GOARCH}.tar.gz -O /tmp/go.tar.gz && tar -C /usr/local -xzf /tmp/go.tar.gz && rm /tmp/go.tar.gz && ln -sf /usr/local/go/bin/go /usr/bin/go && ln -sf /usr/local/go/bin/gofmt /usr/bin/gofmt"
    [25]="ARCH=\$(dpkg --print-architecture) && case \$ARCH in amd64) RUSTARCH=x86_64-unknown-linux-gnu;; arm64) RUSTARCH=aarch64-unknown-linux-gnu;; esac && wget -q https://static.rust-lang.org/dist/rust-${RUST_VERSION}-\${RUSTARCH}.tar.gz -O /tmp/rust.tar.gz && tar -xzf /tmp/rust.tar.gz -C /tmp && /tmp/rust-${RUST_VERSION}-\${RUSTARCH}/install.sh --prefix=/usr/local --without=rust-docs && rm -rf /tmp/rust* && ln -sf /usr/local/bin/rustc /usr/bin/rustc && ln -sf /usr/local/bin/cargo /usr/bin/cargo"
)

# 镜像标签前缀
IMAGE_PREFIX="hustoj/lang"

info() { printf '==> %s\n' "$*"; }
warn() { printf '警告: %s\n' "$*" >&2; }
die()  { printf '错误: %s\n' "$*" >&2; exit 1; }

usage() {
    cat <<EOF
用法: $0 [选项]
选项:
  --lang <id|name>    只构建指定语言 (可多次指定，如 --lang go --lang java)
  --all               构建所有支持的语言
  --arch <arch>       目标架构: amd64, arm64 (默认: \$(dpkg --print-architecture))
  --push              推送镜像到 registry (需先 docker login)
  --load              构建后加载到本地 docker (用于后续 export)
  --out-dir <dir>     输出目录 (默认: \$ROOT/dist/prebuilt-rootfs)
  --go-version <ver>  覆盖 Go 版本 (如 1.22.5)
  --rust-version <ver> 覆盖 Rust 版本 (如 1.82.0)
  --node-major <ver>  覆盖 Node.js 主版本 (如 20)
  --java-version <ver> 覆盖 Java 版本 (如 21)
  --python-version <ver> 覆盖 Python 版本 (如 3.12)
  -h, --help          显示帮助
示例:
  $0 --all --arch amd64 --load
  $0 --lang 17 --lang 3 --arch arm64 --load
  $0 --lang go --go-version 1.22.5 --load
  $0 --lang rust --rust-version 1.82.0 --load
EOF
}

WANT_LANGS=()
WANT_ALL=0
TARGET_ARCH="$(dpkg --print-architecture 2>/dev/null || echo amd64)"
DO_PUSH=0
DO_LOAD=0
OVERRIDE_GO_VERSION=""
OVERRIDE_RUST_VERSION=""
OVERRIDE_NODE_MAJOR=""
OVERRIDE_JAVA_VERSION=""
OVERRIDE_PYTHON_VERSION=""

while [ $# -gt 0 ]; do
    case "$1" in
        --lang)
            WANT_LANGS+=("$2"); shift 2 ;;
        --all)
            WANT_ALL=1; shift ;;
        --arch)
            TARGET_ARCH="$2"; shift 2 ;;
        --push)
            DO_PUSH=1; shift ;;
        --load)
            DO_LOAD=1; shift ;;
        --out-dir)
            OUT_DIR="$2"; shift 2 ;;
        --go-version)
            OVERRIDE_GO_VERSION="$2"; shift 2 ;;
        --rust-version)
            OVERRIDE_RUST_VERSION="$2"; shift 2 ;;
        --node-major)
            OVERRIDE_NODE_MAJOR="$2"; shift 2 ;;
        --java-version)
            OVERRIDE_JAVA_VERSION="$2"; shift 2 ;;
        --python-version)
            OVERRIDE_PYTHON_VERSION="$2"; shift 2 ;;
        -h|--help)
            usage; exit 0 ;;
        *) die "未知参数: $1" ;;
    esac
done

# 应用命令行版本覆盖（优先级：命令行 > 环境变量 > 默认值）
[ -n "$OVERRIDE_GO_VERSION" ] && GO_VERSION="$OVERRIDE_GO_VERSION"
[ -n "$OVERRIDE_RUST_VERSION" ] && RUST_VERSION="$OVERRIDE_RUST_VERSION"
[ -n "$OVERRIDE_NODE_MAJOR" ] && NODE_MAJOR="$OVERRIDE_NODE_MAJOR"
[ -n "$OVERRIDE_JAVA_VERSION" ] && JAVA_VERSION="$OVERRIDE_JAVA_VERSION"
[ -n "$OVERRIDE_PYTHON_VERSION" ] && PYTHON_VERSION="$OVERRIDE_PYTHON_VERSION"

# 规范化架构
case "$TARGET_ARCH" in
    x86_64|amd64) TARGET_ARCH="amd64"; DOCKER_ARCH="amd64" ;;
    aarch64|arm64) TARGET_ARCH="arm64"; DOCKER_ARCH="arm64" ;;
    *) die "不支持的架构: $TARGET_ARCH" ;;
esac

# 确定要构建的语言列表
BUILD_LANGS=()
if [ "$WANT_ALL" -eq 1 ]; then
    for id in "${!LANG_NAME[@]}"; do
        BUILD_LANGS+=("$id")
    done
else
    for spec in "${WANT_LANGS[@]}"; do
        # 支持 ID 或名称
        found=0
        for id in "${!LANG_NAME[@]}"; do
            if [ "$spec" = "$id" ] || [ "$spec" = "${LANG_NAME[$id]}" ]; then
                BUILD_LANGS+=("$id")
                found=1
                break
            fi
        done
        [ "$found" -eq 1 ] || die "未知语言: $spec (可用: ${!LANG_NAME[@]} 或 ${LANG_NAME[@]})"
    done
fi

[ "${#BUILD_LANGS[@]}" -gt 0 ] || die "未指定语言 (用 --all 或 --lang)"

# 检查 docker
command -v docker >/dev/null 2>&1 || die "未找到 docker"

# 设置 buildx 多架构构建器
BUILDER_NAME="hustoj-rootfs-builder"
if ! docker buildx inspect "$BUILDER_NAME" >/dev/null 2>&1; then
    info "创建 buildx 构建器: $BUILDER_NAME"
    docker buildx create --name "$BUILDER_NAME" --driver docker-container --use
    docker buildx inspect "$BUILDER_NAME" --bootstrap
else
    docker buildx use "$BUILDER_NAME"
fi

mkdir -p "$OUT_DIR"

# 构建单个语言
build_lang() {
    local id="$1"
    local name="${LANG_NAME[$id]}"
    local base="${BASE_IMAGE[$id]}"
    local extra="${EXTRA_PACKAGES[$id]}"
    local post="${POST_INSTALL[$id]}"
    local special="${SPECIAL_INSTALL[$id]:-}"
    local tag="${IMAGE_PREFIX}-${name}:${TARGET_ARCH}"
    local dockerfile="$SCRIPT_DIR/$id/Dockerfile"

    info "构建语言: $name (id=$id, arch=$TARGET_ARCH)"
    info "  基础镜像: $base"
    info "  标签: $tag"

    # 生成临时 Dockerfile
    local tmp_df="$(mktemp)"
    cat > "$tmp_df" <<EOF
FROM $base

ENV DEBIAN_FRONTEND=noninteractive

# 统一使用国内源加速（可通过 BUILD_ARGS 覆盖）
ARG APT_MIRROR="mirrors.tuna.tsinghua.edu.cn"
RUN if [ -f /etc/apt/sources.list.d/debian.sources ]; then \
        sed -i "s|deb.debian.org|\${APT_MIRROR}|g" /etc/apt/sources.list.d/debian.sources; \
    elif [ -f /etc/apt/sources.list ]; then \
        sed -i "s|deb.debian.org|\${APT_MIRROR}|g" /etc/apt/sources.list; \
    fi

# 安装额外包
EOF
    if [ -n "$extra" ]; then
        cat >> "$tmp_df" <<EOF
RUN apt-get update && apt-get install -y --no-install-recommends $extra && rm -rf /var/lib/apt/lists/*
EOF
    fi

    # 特殊安装
    if [ -n "$special" ]; then
        cat >> "$tmp_df" <<EOF
RUN $special
EOF
    fi

    # 通用后处理
    if [ -n "$post" ]; then
        cat >> "$tmp_df" <<EOF
RUN $post
EOF
    fi

    # 统一工作目录
    cat >> "$tmp_df" <<EOF
WORKDIR /code
CMD ["/bin/sh"]
EOF

    # 构建参数
    BUILD_ARGS=("--platform" "linux/$DOCKER_ARCH" "--tag" "$tag" "--file" "$tmp_df" ".")
    
    if [ "$DO_PUSH" -eq 1 ]; then
        BUILD_ARGS+=("--push")
    elif [ "$DO_LOAD" -eq 1 ]; then
        BUILD_ARGS+=("--load")
    else
        BUILD_ARGS+=("--output" "type=docker,dest=-")  # 输出到 stdout，供后续处理
    fi

    # 执行构建
    if ! docker buildx build "${BUILD_ARGS[@]}"; then
        rm -f "$tmp_df"
        return 1
    fi
    rm -f "$tmp_df"

    # 如果是 load 模式，导出 rootfs tarball
    if [ "$DO_LOAD" -eq 1 ]; then
        export_rootfs "$name" "$tag"
    fi
}

# 从镜像导出 rootfs
export_rootfs() {
    local name="$1"
    local tag="$2"
    local outfile="$OUT_DIR/rootfs-${name}-${TARGET_ARCH}.tar.gz"

    info "导出 rootfs: $outfile"
    
    # 创建临时容器并 export
    local cid
    cid="$(docker create "$tag")"
    docker export "$cid" | gzip > "$outfile"
    docker rm "$cid" >/dev/null 2>&1

    # 生成校验和
    sha256sum "$outfile" > "$outfile.sha256"
    
    info "完成: $outfile ($(du -h "$outfile" | cut -f1))"
}

# 主循环
failed=()
for id in "${BUILD_LANGS[@]}"; do
    if ! build_lang "$id"; then
        failed+=("${LANG_NAME[$id]}")
        warn "语言 ${LANG_NAME[$id]} (id=$id) 构建失败"
    fi
done

# 生成 manifest.json
if [ -d "$OUT_DIR" ] && [ "$(ls -A "$OUT_DIR"/rootfs-*.tar.gz 2>/dev/null | wc -l)" -gt 0 ]; then
    info "生成 manifest.json"
    cat > "$OUT_DIR/manifest.json" <<EOF
{
  "version": "$(cd "$ROOT" && git describe --tags --always 2>/dev/null || echo dev)",
  "date": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "architectures": ["$TARGET_ARCH"],
  "languages": {}
}
EOF

    for f in "$OUT_DIR"/rootfs-*-${TARGET_ARCH}.tar.gz; do
        [ -f "$f" ] || continue
        lang=$(basename "$f" .tar.gz | sed "s/rootfs-//; s/-${TARGET_ARCH}$//")
        sha256=$(cat "$f.sha256" | awk '{print $1}')
        size=$(stat -c%s "$f")
        jq --arg lang "$lang" --arg sha "$sha256" --arg size "$size" \
            '.languages[$lang][$ENV.TARGET_ARCH] = {sha256: $sha, size: ($size|tonumber)}' \
            "$OUT_DIR/manifest.json" > "$OUT_DIR/manifest.json.tmp" && mv "$OUT_DIR/manifest.json.tmp" "$OUT_DIR/manifest.json"
    done
    cat "$OUT_DIR/manifest.json"
fi

if [ "${#failed[@]}" -gt 0 ]; then
    warn "以下语言构建失败: ${failed[*]}"
    exit 1
fi

info "所有语言构建完成，产物在: $OUT_DIR"
ls -lh "$OUT_DIR"