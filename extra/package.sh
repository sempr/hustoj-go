#!/usr/bin/env bash
#
# 在构建机产出「离线部署包」：交叉编译好的静态二进制 + unit + 配置样例 +
# 语言构建输入 + 纯 shell 安装脚本。目标机只需 bash/systemd，不需要
# Go 工具链或编译器。
#
# 用法：
#   extra/package.sh [目标...] [选项]
#
# 目标：<goos>/<goarch>，可给多个；也可以只给 <goarch>（默认 linux）。
#   缺省：linux/amd64 linux/arm64
#
# 选项：
#   --out DIR         输出目录（默认 <repo>/dist）
#   --version STR     覆盖版本号（默认取 git describe）
#   --no-tini         不打包 tini（判题会缺 /usr/local/bin/tini）
#   --no-fetch-tini   交叉编译 tini 失败时不从 GitHub 下载官方静态二进制
#   --no-demo         不打包示例题库（demo/）
#   -h, --help
#
# 产物：
#   dist/hustoj-go-<version>-<goos>-<goarch>.tar.gz
#   dist/hustoj-go-<version>-<goos>-<goarch>.tar.gz.sha256
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
MODULE="github.com/sempr/hustoj-go"

OUT_DIR="$ROOT/dist"
WANT_TINI=1
FETCH_TINI=1
WANT_DEMO=1
VERSION_OVERRIDE=""
TARGETS=()

TINI_VERSION=v0.19.0

info() { printf '==> %s\n' "$*"; }
warn() { printf '警告: %s\n' "$*" >&2; }
die()  { printf '错误: %s\n' "$*" >&2; exit 1; }

usage() { sed -n '3,/^set /p' "$0" | sed -e 's/^# \{0,1\}//' -e '/^set /d'; }

while [ $# -gt 0 ]; do
    case "$1" in
        --out)           OUT_DIR="$2"; shift 2 ;;
        --version)       VERSION_OVERRIDE="$2"; shift 2 ;;
        --no-tini)       WANT_TINI=0; shift ;;
        --no-fetch-tini) FETCH_TINI=0; shift ;;
        --no-demo)       WANT_DEMO=0; shift ;;
        -h|--help)       usage; exit 0 ;;
        -*|--*)          die "未知选项: $1" ;;
        *)
            case "$1" in
                */*) TARGETS+=("$1") ;;
                *)   TARGETS+=("linux/$1") ;;
            esac
            shift ;;
    esac
done

[ "${#TARGETS[@]}" -gt 0 ] || TARGETS=(linux/amd64 linux/arm64)

command -v go >/dev/null 2>&1 || die "构建机需要 Go 工具链"
command -v tar >/dev/null 2>&1 || die "需要 tar"

HOST_GOARCH="$(go env GOARCH)"
GO_VERSION="$(go env GOVERSION 2>/dev/null || go version | awk '{print $3}')"

# ── 版本信息（与 Makefile/deploy.sh 一致）───────────────────────────
if [ -n "$VERSION_OVERRIDE" ]; then
    VERSION="$VERSION_OVERRIDE"
else
    VERSION="$(git -C "$ROOT" describe --tags --always 2>/dev/null || echo unknown)"
fi
DIRTY=""
git -C "$ROOT" diff --quiet 2>/dev/null || DIRTY="-dirty"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD 2>/dev/null || echo unknown)${DIRTY}"
DATE="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

LDFLAGS="-s -w -X '${MODULE}/cmd.Version=${VERSION}' -X '${MODULE}/cmd.Commit=${COMMIT}' -X '${MODULE}/cmd.Date=${DATE}'"

info "版本: $VERSION  提交: $COMMIT  Go: $GO_VERSION"

# ── tini ─────────────────────────────────────────────────────────────
# 优先用本机/交叉 gcc 从仓库源码编译（离线、可复现）；否则回退到官方
# GitHub release 的静态二进制。
build_tini() { # <goarch> <out>
    local arch="$1" out="$2" cross=""
    case "$arch" in
        amd64) cross=x86_64-linux-gnu-gcc ;;
        arm64) cross=aarch64-linux-gnu-gcc ;;
        386)   cross=i686-linux-gnu-gcc ;;
    esac
    if [ "$arch" = "$HOST_GOARCH" ] && command -v gcc >/dev/null 2>&1; then
        gcc --static -O2 -o "$out" "$ROOT/tini/tini.c"
        return 0
    fi
    if [ -n "$cross" ] && command -v "$cross" >/dev/null 2>&1; then
        "$cross" --static -O2 -o "$out" "$ROOT/tini/tini.c"
        return 0
    fi
    return 1
}

fetch_tini() { # <goarch> <out>
    local arch="$1" out="$2"
    command -v curl >/dev/null 2>&1 || return 1
    local url="https://github.com/krallin/tini/releases/download/${TINI_VERSION}/tini-static-${arch}"
    info "从官方 release 获取 tini ($arch)"
    curl -fsSL --max-time 120 -o "$out" "$url" && chmod 0755 "$out"
}

make_tini() { # <goarch> <out>
    [ "$WANT_TINI" -eq 1 ] || return 0
    if build_tini "$1" "$2"; then
        return 0
    fi
    if [ "$FETCH_TINI" -eq 1 ] && fetch_tini "$1" "$2"; then
        return 0
    fi
    warn "$1: 无法编译或获取 tini；包内将不含 tini（判题需要 /usr/local/bin/tini）"
}

# 收集语言 rootfs 构建输入（Dockerfile + 脚本 + lang.toml 模板）。
copy_lang_inputs() { # <dest-langs-dir>
    local dst="$1"
    mkdir -p "$dst/etc/langs"
    cp "$SCRIPT_DIR/prepare_langs.sh" "$SCRIPT_DIR/replace.sh" \
       "$SCRIPT_DIR/build_rootfs.sh" "$SCRIPT_DIR/install_langs.sh" "$dst/"
    cp "$SCRIPT_DIR"/etc/langs/*.lang.toml "$SCRIPT_DIR/etc/langs/all.toml" "$dst/etc/langs/"
    local d
    for d in "$SCRIPT_DIR"/*/; do
        [ -f "$d/Dockerfile" ] || continue
        cp -r "$d" "$dst/$(basename "$d")"
    done
}

make_package() { # <goos> <goarch>
    local goos="$1" goarch="$2"
    local name="hustoj-go-${VERSION}-${goos}-${goarch}"
    local dir="$OUT_DIR/$name"

    info "构建 $goos/$goarch -> $name"
    rm -rf "$dir"
    mkdir -p "$dir/bin" "$dir/systemd" "$dir/etc/langs" "$dir/langs"

    info "  编译 bin/hustoj（CGO_ENABLED=0）"
    ( cd "$ROOT" && CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
        go build -trimpath -ldflags "$LDFLAGS" -o "$dir/bin/hustoj" . )

    make_tini "$goarch" "$dir/bin/tini"

    info "  收集 unit / 配置 / 脚本"
    cp "$SCRIPT_DIR/hustoj-web.service" "$SCRIPT_DIR/judged-go.service" "$dir/systemd/"
    cp "$SCRIPT_DIR/web.toml.example" "$dir/etc/web.toml.example"
    cp "$SCRIPT_DIR/judge.conf.example" "$dir/etc/judge.conf.example"
    cp "$SCRIPT_DIR/etc/langs/all.toml" "$dir/etc/langs/all.toml"
    cp "$SCRIPT_DIR/install.sh" "$SCRIPT_DIR/uninstall.sh" "$SCRIPT_DIR/init_db.sh" "$SCRIPT_DIR/migrate.sh" "$dir/"
    chmod 0755 "$dir/install.sh" "$dir/uninstall.sh" "$dir/init_db.sh" "$dir/migrate.sh"
    info "  收集 schema（建表/触发器/种子）"
    mkdir -p "$dir/schema"
    cp "$ROOT/schema/schema.sql" "$ROOT/schema/triggers.sql" "$ROOT/schema/seed.sql" "$dir/schema/"
    info "  收集迁移脚本（migrations/）"
    mkdir -p "$dir/migrations"
    cp -r "$ROOT/migrations/." "$dir/migrations/"
    [ -f "$ROOT/LICENSE" ] && cp "$ROOT/LICENSE" "$dir/"
    [ -f "$ROOT/docs/deploy-package.md" ] && cp "$ROOT/docs/deploy-package.md" "$dir/README.md"

    copy_lang_inputs "$dir/langs"
    chmod 0755 "$dir/langs/install_langs.sh" "$dir/langs/prepare_langs.sh" \
               "$dir/langs/replace.sh" "$dir/langs/build_rootfs.sh"

    if [ "$WANT_DEMO" -eq 1 ] && [ -d "$SCRIPT_DIR/demo" ]; then
        info "  收集示例题库并编译判题程序（$goarch）"
        rm -rf "$dir/demo"
        cp -r "$SCRIPT_DIR/demo" "$dir/demo"
        rm -f "$dir/demo/go.mod"   # 仅供仓库内 Go 工具链忽略该子树，不进部署包
        chmod 0755 "$dir/demo/install_demo.sh" "$dir/demo/build_data.sh"
        if [ "$goos" = "linux" ]; then
            bash "$SCRIPT_DIR/demo/build_data.sh" "$goarch" "$dir/demo" \
                || warn "$goos/$goarch: 示例题判题程序未全部编译（对应题将无法判题）"
        else
            warn "$goos/$goarch: 跳过示例题判题程序编译（仅支持 linux）"
        fi
    fi

    cat > "$dir/VERSION" <<EOF
version: ${VERSION}
commit:  ${COMMIT}
date:    ${DATE}
goos:    ${goos}
goarch:  ${goarch}
go:      ${GO_VERSION}
EOF

    info "  生成 SHA256SUMS"
    ( cd "$dir" && find . -type f ! -name SHA256SUMS -print0 \
        | sort -z | xargs -0 sha256sum > SHA256SUMS )

    info "  打包 $name.tar.gz"
    ( cd "$OUT_DIR" && tar -czf "$name.tar.gz" "$name" )
    ( cd "$OUT_DIR" && sha256sum "$name.tar.gz" > "$name.tar.gz.sha256" )
    info "  -> $OUT_DIR/$name.tar.gz"
    # 清掉暂存目录：dist/ 只留交付物，且避免 Go 工具链把展开后的
    # 参考解目录也当成包（`go build ./...` 会因此报错）。
    rm -rf "$dir"
}

mkdir -p "$OUT_DIR"
for t in "${TARGETS[@]}"; do
    goos="${t%%/*}"
    goarch="${t##*/}"
    [ -n "$goos" ] && [ -n "$goarch" ] || die "非法目标: $t（应为 <goos>/<goarch>）"
    make_package "$goos" "$goarch"
done

printf '\n完成。产物在 %s：\n' "$OUT_DIR"
ls -1 "$OUT_DIR"/*.tar.gz "$OUT_DIR"/*.sha256 2>/dev/null || true
