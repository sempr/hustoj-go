#!/usr/bin/env bash
#
# hustoj-go 离线部署安装脚本。
#
# 目标机只需 bash / systemd，不需要 Go 工具链或编译器：二进制与静态资源
# 都已在构建机交叉编译好，打进本包。
#
# 用法（先解压，再在包目录里执行）：
#   tar -xzf hustoj-go-<version>-linux-amd64.tar.gz
#   cd hustoj-go-<version>-linux-amd64
#   sudo ./install.sh [选项]
#
# 选项：
#   --prefix DIR      二进制安装目录（默认 /usr/bin）
#   --ojhome DIR      运行数据根目录（默认 /home/judge）
#   --web-user USER   Web 服务降权用户（默认 www-data）
#   --tini-dir DIR    tini 安装目录（默认 /usr/local/bin，判题沙箱固定找这里）
#   --systemd-dir DIR systemd unit 安装目录（默认 /etc/systemd/system）
#   --no-web          不安装/启用 hustoj-web
#   --no-judge        不安装/启用 judged-go
#   --skip-systemd    只落地文件，不 daemon-reload、不 enable（容器/离线排查用）
#   --force           覆盖已存在的 web.toml / judge.conf / unit
#   --dry-run         只打印将要执行的动作
#   -h, --help        显示帮助
#
# 幂等：重复执行只会刷新二进制/unit，默认不覆盖既有配置。
set -euo pipefail

SRC_DIR="$(cd "$(dirname "$0")" && pwd)"

PREFIX=/usr/bin
OJHOME=/home/judge
WEB_USER=www-data
TINI_DIR=/usr/local/bin
SYSDIR=/etc/systemd/system
DO_WEB=1
DO_JUDGE=1
SKIP_SYSTEMD=0
FORCE=0
DRY=0
WEB_HEALTH_PORT=8080

die()  { printf '错误: %s\n' "$*" >&2; exit 1; }
info() { printf '==> %s\n' "$*"; }
warn() { printf '警告: %s\n' "$*" >&2; }

run() {
    if [ "$DRY" -eq 1 ]; then
        printf '  [dry-run] %s\n' "$*"
    else
        "$@"
    fi
}

usage() { sed -n '3,26p' "$0" | sed 's/^# \{0,1\}//'; }

while [ $# -gt 0 ]; do
    case "$1" in
        --prefix)       PREFIX="$2"; shift 2 ;;
        --ojhome)       OJHOME="$2"; shift 2 ;;
        --web-user)     WEB_USER="$2"; shift 2 ;;
        --tini-dir)     TINI_DIR="$2"; shift 2 ;;
        --systemd-dir)  SYSDIR="$2"; shift 2 ;;
        --health-port)  WEB_HEALTH_PORT="$2"; shift 2 ;;
        --no-web)       DO_WEB=0; shift ;;
        --no-judge)     DO_JUDGE=0; shift ;;
        --skip-systemd) SKIP_SYSTEMD=1; shift ;;
        --force)        FORCE=1; shift ;;
        --dry-run)      DRY=1; shift ;;
        -h|--help)      usage; exit 0 ;;
        *)              die "未知参数: $1（用 --help 查看用法）" ;;
    esac
done

# sed 替换用 | 作分隔符，路径里不能有它或换行。
for v in "$PREFIX" "$OJHOME" "$SYSDIR" "$TINI_DIR"; do
    case "$v" in
        *"|"*|*$'\n'*) die "路径不能包含 '|' 或换行: $v" ;;
    esac
done

if [ "$DRY" -ne 1 ] && [ "$(id -u)" -ne 0 ]; then
    die "请以 root 运行（sudo ./install.sh）"
fi

[ -f "$SRC_DIR/bin/hustoj" ] || die "包不完整：缺少 $SRC_DIR/bin/hustoj"
[ -f "$SRC_DIR/systemd/hustoj-web.service" ] || die "包不完整：缺少 systemd/hustoj-web.service"
[ -f "$SRC_DIR/systemd/judged-go.service" ] || die "包不完整：缺少 systemd/judged-go.service"

# ── 校验包完整性 ─────────────────────────────────────────────────────
if command -v sha256sum >/dev/null 2>&1 && [ -f "$SRC_DIR/SHA256SUMS" ]; then
    info "校验 SHA256SUMS"
    if [ "$DRY" -eq 1 ]; then
        printf '  [dry-run] (cd %s && sha256sum -c SHA256SUMS)\n' "$SRC_DIR"
    elif ! ( cd "$SRC_DIR" && sha256sum -c --quiet SHA256SUMS ); then
        die "包校验失败，请重新获取"
    fi
fi

backup() {
    local f="$1"
    if [ -e "$f" ]; then
        local ts; ts="$(date +%Y%m%d%H%M%S)"
        run cp -a "$f" "${f}.bak-${ts}"
    fi
}

install_file() { # <src> <dst> <mode>
    info "安装 $2"
    backup "$2"
    run install -D -m "$3" "$1" "$2"
}

# ── 二进制 / tini ────────────────────────────────────────────────────
install_file "$SRC_DIR/bin/hustoj" "$PREFIX/hustoj" 0755
if [ -f "$SRC_DIR/bin/tini" ]; then
    install_file "$SRC_DIR/bin/tini" "$TINI_DIR/tini" 0755
else
    warn "包内没有 tini，判题沙箱需要 $TINI_DIR/tini（可稍后单独安装）"
fi

# ── 目录 ─────────────────────────────────────────────────────────────
info "创建运行目录：$OJHOME/{etc/langs,data,run}"
run mkdir -p "$OJHOME/etc/langs" "$OJHOME/data" "$OJHOME/run"
if id judge >/dev/null 2>&1 && [ "$DRY" -ne 1 ]; then
    chown judge:judge "$OJHOME/etc" "$OJHOME/data" "$OJHOME/run" 2>/dev/null || true
fi

# ── 语言注册表（仅 all.toml；host 相关的 *.lang.toml 由 install_langs.sh 生成）
if [ -f "$SRC_DIR/etc/langs/all.toml" ]; then
    install_file "$SRC_DIR/etc/langs/all.toml" "$OJHOME/etc/langs/all.toml" 0644
fi

# ── 运行配置（默认不覆盖已有）────────────────────────────────────────
info "写入运行配置"
for pair in "web.toml:web.toml.example:0640" "judge.conf:judge.conf.example:0600"; do
    dst="${pair%%:*}"; rest="${pair#*:}"; src="${rest%%:*}"; mode="${rest##*:}"
    target="$OJHOME/etc/$dst"
    if [ -e "$target" ] && [ "$FORCE" -ne 1 ]; then
        info "  保留现有 $target（--force 可覆盖）"
        continue
    fi
    if [ -f "$SRC_DIR/etc/$src" ]; then
        run install -D -m "$mode" "$SRC_DIR/etc/$src" "$target"
        if [ "$dst" = "web.toml" ] && id "$WEB_USER" >/dev/null 2>&1 && [ "$DRY" -ne 1 ]; then
            chown "root:$WEB_USER" "$target" 2>/dev/null || true
        fi
        if [ "$dst" = "judge.conf" ]; then
            warn "请编辑 $target 填入真实数据库凭据后再启动服务"
        fi
        info "  -> $target"
    fi
done

# ── 生成 FAQ 语言信息缓存（best-effort）─────────────────────────────
if [ "$DRY" -eq 1 ]; then
    printf '  [dry-run] %s langinfo --ojhome %s\n' "$PREFIX/hustoj" "$OJHOME"
else
    "$PREFIX/hustoj" langinfo --ojhome "$OJHOME" || warn "langinfo 生成失败（可稍后重试）"
fi

# ── systemd unit ─────────────────────────────────────────────────────
render_unit() { # <src> <dst> <web?>
    local src="$1" dst="$2" web="$3"
    info "安装 unit $dst"
    backup "$dst"
    local tmp; tmp="$(mktemp)"
    sed -e "s|/usr/bin/hustoj|$PREFIX/hustoj|g" \
        -e "s|/home/judge|$OJHOME|g" \
        "$src" > "$tmp"
    if [ "$web" = "1" ]; then
        sed -i "s|^User=.*|User=$WEB_USER|" "$tmp"
    fi
    run install -D -m 0644 "$tmp" "$dst"
    rm -f "$tmp"
}

if [ "$DO_WEB" -eq 1 ]; then
    id "$WEB_USER" >/dev/null 2>&1 || warn "用户 $WEB_USER 不存在，hustoj-web 将无法启动（--web-user 可改）"
    render_unit "$SRC_DIR/systemd/hustoj-web.service" "$SYSDIR/hustoj-web.service" 1
fi
if [ "$DO_JUDGE" -eq 1 ]; then
    render_unit "$SRC_DIR/systemd/judged-go.service" "$SYSDIR/judged-go.service" 0
fi

# ── 生效 ─────────────────────────────────────────────────────────────
if [ "$SKIP_SYSTEMD" -eq 1 ]; then
    warn "已跳过 systemd 操作（--skip-systemd）；unit 已写入 $SYSDIR"
else
    info "重载并启动服务"
    run systemctl daemon-reload
    if [ "$DO_JUDGE" -eq 1 ]; then
        run systemctl enable --now judged-go.service
    fi
    if [ "$DO_WEB" -eq 1 ]; then
        run systemctl enable --now hustoj-web.service
    fi
fi

# ── 自检 ─────────────────────────────────────────────────────────────
if [ "$DO_WEB" -eq 1 ] && [ "$SKIP_SYSTEMD" -ne 1 ] && [ "$DRY" -ne 1 ]; then
    info "Web 健康检查（127.0.0.1:${WEB_HEALTH_PORT}/healthz）"
    ok=0
    for _ in 1 2 3 4 5; do
        if curl -fsS "http://127.0.0.1:${WEB_HEALTH_PORT}/healthz" >/dev/null 2>&1; then ok=1; break; fi
        sleep 1
    done
    if [ "$ok" -eq 1 ]; then
        info "Web 服务已就绪"
    else
        warn "健康检查未通过，请查看：journalctl -u hustoj-web.service -n 50"
    fi
fi

printf '\n安装完成。\n'
printf '  - 判题语言运行时（g++/python 等）需另行准备：\n'
printf '      cd langs && sudo ./install_langs.sh --ojhome %s   # 需要 docker\n' "$OJHOME"
printf '  - 语言 rootfs 生成后，重启判题服务：systemctl restart judged-go.service\n'
printf '  - nginx 需把对应路径反代到 127.0.0.1:8080（详见 README / 迁移清单）\n'
