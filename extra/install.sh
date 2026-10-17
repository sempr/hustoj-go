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
# 全新库初始化（可选，转发给同目录的 init_db.sh；详见 docs/deploy-package.md）：
#   --init-db         部署时初始化数据库（建库/建表/触发器/管理员，并写 judge.conf）
#   --db-host HOST    应用连接地址（默认 127.0.0.1）
#   --db-port PORT    应用连接端口（默认 3306）
#   --db-name NAME    库名（默认 hustoj）
#   --db-user NAME    应用账号（默认 hustoj）
#   --db-password PWD 应用账号口令（--init-db 时必填）
#   --db-root-user U  管理员账号（默认 root，走 unix socket）
#   --db-root-password PWD  管理员口令
#   --db-socket PATH  管理员 unix socket 路径
#   --db-admin-user U       种子管理员账号（默认 admin）
#   --db-admin-password PWD 种子管理员口令（默认 admin，建议改）
#   --db-recreate     重建库（DROP DATABASE，破坏性）
#   --db-force        目标库非空时仍继续（会丢表内数据）
#
# 存量库结构迁移（可选，转发给同目录 migrate.sh；详见 docs/deploy-package.md）：
#   --migrate         按 migrations/ 把已有库结构对齐到 schema.sql（只 ALTER，不清数据；
#                     未给 --db-password 时凭据取自 <ojhome>/etc/judge.conf）
#
# 示例题库（可选，转发给同目录 demo/install_demo.sh；详见 demo/README.md）：
#   --demo            装载示例题（普通/子任务/SPJ/交互/文件IO 各一）+ 演示比赛
#   --no-demo-contest --demo 时不创建演示比赛
#
# 幂等：重复执行只会刷新二进制/unit，默认不覆盖既有配置；--init-db 对非空库会拒绝。
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
INIT_DB=0
MIGRATE_DB=0
DB_HOST=127.0.0.1
DB_PORT=3306
DB_NAME=hustoj
DB_USER=hustoj
DB_PASSWORD=""
DB_ROOT_USER=root
DB_ROOT_PASSWORD=""
DB_SOCKET=""
DB_RECREATE=0
DB_FORCE=0
DB_ADMIN_USER=""
DB_ADMIN_PASSWORD=""
DO_DEMO=0
DEMO_CONTEST=1

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

usage() { sed -n '2,/^set /p' "$0" | sed -e 's/^# \{0,1\}//' -e '/^set /d'; }

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
        --init-db)      INIT_DB=1; shift ;;
        --migrate)      MIGRATE_DB=1; shift ;;
        --db-host)      DB_HOST="$2"; shift 2 ;;
        --db-port)      DB_PORT="$2"; shift 2 ;;
        --db-name)      DB_NAME="$2"; shift 2 ;;
        --db-user)      DB_USER="$2"; shift 2 ;;
        --db-password)  DB_PASSWORD="$2"; shift 2 ;;
        --db-root-user) DB_ROOT_USER="$2"; shift 2 ;;
        --db-root-password) DB_ROOT_PASSWORD="$2"; shift 2 ;;
        --db-socket)    DB_SOCKET="$2"; shift 2 ;;
        --db-recreate)  DB_RECREATE=1; shift ;;
        --db-force)     DB_FORCE=1; shift ;;
        --db-admin-user) DB_ADMIN_USER="$2"; shift 2 ;;
        --db-admin-password) DB_ADMIN_PASSWORD="$2"; shift 2 ;;
        --demo)         DO_DEMO=1; shift ;;
        --no-demo-contest) DEMO_CONTEST=0; shift ;;
        -h|--help)      usage; exit 0 ;;
        *)              die "未知参数: $1（用 --help 查看用法）" ;;
    esac
done

# 路径必须是绝对路径；sed 替换用 | 作分隔符，路径里不能有它或换行。
for v in "$PREFIX" "$OJHOME" "$SYSDIR" "$TINI_DIR"; do
    case "$v" in
        /*) ;;
        *) die "路径必须是绝对路径: $v" ;;
    esac
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
info "创建运行目录：$OJHOME/{etc/langs,data,run,var/rootfs}"
run mkdir -p "$OJHOME/etc/langs" "$OJHOME/data" "$OJHOME/run" "$OJHOME/var/rootfs"
if id judge >/dev/null 2>&1 && [ "$DRY" -ne 1 ]; then
    chown judge:judge "$OJHOME/etc" "$OJHOME/data" "$OJHOME/run" "$OJHOME/var" 2>/dev/null || true
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

# ── 全新库初始化（可选，转发给同目录 init_db.sh）────────────────────
if [ "$INIT_DB" -eq 1 ]; then
    [ -n "$DB_PASSWORD" ] || die "--init-db 需要 --db-password"
    [ -f "$SRC_DIR/init_db.sh" ] || die "包内缺少 init_db.sh，无法 --init-db"
    if [ "$DRY" -eq 1 ]; then
        printf '  [dry-run] init_db.sh --db %s --user %s --password **** --host %s:%s --ojhome %s --write-conf\n' \
            "$DB_NAME" "$DB_USER" "$DB_HOST" "$DB_PORT" "$OJHOME"
    else
        info "初始化数据库 '$DB_NAME'（账号 '$DB_USER'）"
        init_args=(--db "$DB_NAME" --user "$DB_USER" --password "$DB_PASSWORD"
                   --host "$DB_HOST" --port "$DB_PORT" --ojhome "$OJHOME" --write-conf)
        [ -n "$DB_ROOT_USER" ] && init_args+=(--root-user "$DB_ROOT_USER")
        [ -n "$DB_ROOT_PASSWORD" ] && init_args+=(--root-password "$DB_ROOT_PASSWORD")
        [ -n "$DB_SOCKET" ] && init_args+=(--socket "$DB_SOCKET")
        [ -n "$DB_ADMIN_USER" ] && init_args+=(--admin-user "$DB_ADMIN_USER")
        [ -n "$DB_ADMIN_PASSWORD" ] && init_args+=(--admin-password "$DB_ADMIN_PASSWORD")
        [ "$DB_RECREATE" -eq 1 ] && init_args+=(--recreate)
        [ "$DB_FORCE" -eq 1 ] && init_args+=(--force)
        bash "$SRC_DIR/init_db.sh" "${init_args[@]}"
    fi
fi

# ── 存量库结构迁移（可选，转发给同目录 migrate.sh）────────────────────
if [ "$MIGRATE_DB" -eq 1 ]; then
    [ -f "$SRC_DIR/migrate.sh" ] || die "包内缺少 migrate.sh，无法 --migrate"
    [ -d "$SRC_DIR/migrations" ] || die "包内缺少 migrations/，无法 --migrate"
    migrate_args=(--ojhome "$OJHOME" --check)
    # 未显式给口令时，交给 migrate.sh 从 judge.conf 读取
    if [ -n "$DB_PASSWORD" ]; then
        migrate_args=(--host "$DB_HOST" --port "$DB_PORT" --db "$DB_NAME" \
                      --user "$DB_USER" --password "$DB_PASSWORD" "${migrate_args[@]}")
    fi
    [ "$DRY" -eq 1 ] && migrate_args+=(--dry-run)
    if [ "$DRY" -eq 1 ]; then
        printf '  [dry-run] migrate.sh %s\n' "${migrate_args[*]}"
    else
        info "迁移存量库 '$DB_NAME'（migrations/ → schema.sql）"
        bash "$SRC_DIR/migrate.sh" "${migrate_args[@]}"
    fi
fi

# ── 示例题库（可选，转发给同目录 demo/install_demo.sh）────────────────
if [ "$DO_DEMO" -eq 1 ]; then
    [ -f "$SRC_DIR/demo/install_demo.sh" ] || die "包内缺少 demo/install_demo.sh，无法 --demo"
    demo_args=(--ojhome "$OJHOME")
    [ "$DEMO_CONTEST" -eq 0 ] && demo_args+=(--no-contest)
    [ "$DRY" -eq 1 ] && demo_args+=(--dry-run)
    if [ "$DRY" -eq 1 ]; then
        printf '  [dry-run] demo/install_demo.sh %s\n' "${demo_args[*]}"
    else
        info "装载示例题库"
        # 说明：install_demo.sh 从 <OJHome>/etc/judge.conf 读取 DB 凭据，
        # 故须在 --init-db（写 judge.conf）之后执行。未初始化数据库时会失败，
        # 这里只告警、不中断安装。
        bash "$SRC_DIR/demo/install_demo.sh" "${demo_args[@]}" \
            || warn "示例题装载失败（请先初始化数据库并确认 judge.conf 凭据）"
    fi
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
if [ "$INIT_DB" -eq 0 ] && [ "$MIGRATE_DB" -eq 0 ]; then
    printf '  - 若为全新库，数据库尚未初始化：\n'
    printf '      sudo ./init_db.sh --db hustoj --user hustoj --password ... --ojhome %s --write-conf\n' "$OJHOME"
    printf '      （或安装时直接加 --init-db --db-password ...）\n'
    printf '  - 若为老版本库（结构不同、数据要保留），迁移到新结构：\n'
    printf '      sudo ./migrate.sh --ojhome %s --check        # 或安装时加 --migrate\n' "$OJHOME"
fi
printf '  - 判题语言运行时（g++/python 等）需另行准备：\n'
printf '      cd langs && sudo ./install_langs.sh --ojhome %s   # 需要 docker\n' "$OJHOME"
printf '  - 语言 rootfs 生成后，重启判题服务：systemctl restart judged-go.service\n'
if [ "$DO_DEMO" -eq 0 ]; then
    printf '  - 需要示例题时可另行装载：\n'
    printf '      sudo ./demo/install_demo.sh --ojhome %s   # 或安装时加 --demo\n' "$OJHOME"
fi
printf '  - nginx 需把对应路径反代到 127.0.0.1:8080（详见 README / 迁移清单）\n'
