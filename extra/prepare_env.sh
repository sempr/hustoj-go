#!/usr/bin/env bash
#
# hustoj-go 目标机「部署环境准备」脚本：安装并启用 MariaDB / Redis / Docker。
#
# 目标机前置（见 docs/deploy-package.md「二、目标机：部署」）：
#   - MariaDB   判题与 Web 共用的数据库（install.sh --init-db / migrate.sh 依赖）
#   - Redis     Web 会话存储（hustoj web 的 redis_url 默认 redis://127.0.0.1:6379/0）
#   - Docker    判题语言运行时构建（langs/install_langs.sh 用它拍平 rootfs）
#
# 本脚本只负责「把三件套装好并跑起来」：装包、enable 并启动服务、写一份
# Docker daemon.json（镜像加速 / live-restore / 日志轮转）。数据库建库建表
# 交给 init_db.sh，语言 rootfs 交给 langs/install_langs.sh。
#
# 要求：Debian/Ubuntu（apt）+ systemd。已在用的软件包会跳过安装，重复执行安全。
#
# 用法：
#   sudo ./prepare_env.sh [选项]
#
# 选项：
#   --no-db            不安装 MariaDB
#   --no-redis         不安装 Redis
#   --no-docker        不安装 Docker
#   --no-update        跳过 apt-get update
#   --no-config        只装包，不写 Docker daemon.json（仍会 enable 启动服务）
#   --docker-mirror URL  Docker 镜像加速地址，可重复；默认内置两个国内镜像
#   --no-docker-mirror   不配置任何镜像加速（仅 live-restore/日志轮转）
#   --skip-systemd     不 enable/启动服务（容器/离线排查用）
#   --force            覆盖已存在的 /etc/docker/daemon.json（默认保留）
#   --dry-run          只打印将要执行的动作
#   -h, --help         显示帮助
#
# 示例：
#   sudo ./prepare_env.sh
#   sudo ./prepare_env.sh --no-db --no-redis          # 只装 Docker
#   sudo ./prepare_env.sh --docker-mirror https://docker.1ms.run
set -euo pipefail

DO_DB=1
DO_REDIS=1
DO_DOCKER=1
DO_UPDATE=1
DO_CONFIG=1
DO_SYSTEMD=1
FORCE=0
DRY=0
MIRRORS_SET=0
DOCKER_MIRRORS=()

# 允许用环境变量覆盖，便于测试与自定义安装路径。
DOCKER_DAEMON_JSON="${DOCKER_DAEMON_JSON:-/etc/docker/daemon.json}"

DB_PKG=mariadb-server
DB_CLIENT_PKG=mariadb-client
REDIS_PKG=redis-server
DOCKER_PKG=docker.io

DB_SERVICE=mariadb
REDIS_SERVICE=redis-server
DOCKER_SERVICE=docker

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
        --no-db)            DO_DB=0; shift ;;
        --no-redis)         DO_REDIS=0; shift ;;
        --no-docker)        DO_DOCKER=0; shift ;;
        --no-update)        DO_UPDATE=0; shift ;;
        --no-config)        DO_CONFIG=0; shift ;;
        --no-docker-mirror) MIRRORS_SET=1; DOCKER_MIRRORS=(); shift ;;
        --docker-mirror)    [ "$MIRRORS_SET" -eq 1 ] || { MIRRORS_SET=1; DOCKER_MIRRORS=(); }
                            DOCKER_MIRRORS+=("$2"); shift 2 ;;
        --skip-systemd)     DO_SYSTEMD=0; shift ;;
        --force)            FORCE=1; shift ;;
        --dry-run)          DRY=1; shift ;;
        -h|--help)          usage; exit 0 ;;
        *)                  die "未知参数: $1（用 --help 查看用法）" ;;
    esac
done

if [ "$MIRRORS_SET" -eq 0 ]; then
    DOCKER_MIRRORS=(
        "https://docker.1ms.run"
        "https://docker.xuanyuan.me"
    )
fi

if [ "$DO_DB" -eq 0 ] && [ "$DO_REDIS" -eq 0 ] && [ "$DO_DOCKER" -eq 0 ]; then
    die "三件套都被 --no-* 关掉了，没有可做的事"
fi

if [ "$DRY" -ne 1 ] && [ "$(id -u)" -ne 0 ]; then
    die "请以 root 运行（sudo ./prepare_env.sh）"
fi

command -v apt-get >/dev/null 2>&1 \
    || die "仅支持 Debian/Ubuntu（未找到 apt-get）；其它发行版请手动安装 MariaDB/Redis/Docker"

# 没有 systemd（容器等）时不强行 enable；--skip-systemd 可显式跳过。
if [ "$DO_SYSTEMD" -eq 1 ] && [ ! -d /run/systemd/system ]; then
    warn "未检测到运行中的 systemd，改为只安装不 enable/启动（用 --skip-systemd 可抑制本提示）"
    DO_SYSTEMD=0
fi

# ── 组装软件包列表 ───────────────────────────────────────────────────
PACKAGES=()
if [ "$DO_DB" -eq 1 ]; then
    PACKAGES+=("$DB_PKG" "$DB_CLIENT_PKG")
fi
if [ "$DO_REDIS" -eq 1 ]; then
    PACKAGES+=("$REDIS_PKG")
fi
if [ "$DO_DOCKER" -eq 1 ]; then
    PACKAGES+=("$DOCKER_PKG")
fi

info "将要安装: ${PACKAGES[*]}"
if [ "$DO_UPDATE" -eq 1 ]; then
    run env DEBIAN_FRONTEND=noninteractive apt-get update -qq || warn "apt-get update 失败，继续尝试安装"
else
    info "已跳过 apt-get update（--no-update）"
fi

# apt-get 只在前置缺失时才需要；已装齐时几乎无操作，天然幂等。
if ! run env DEBIAN_FRONTEND=noninteractive apt-get install -y "${PACKAGES[@]}"; then
    die "软件包安装失败（${PACKAGES[*]}）；常见原因：dpkg 被中断（dpkg --configure -a）、apt 源不可用或磁盘不足"
fi

# ── Docker 基础配置（镜像加速 / live-restore / 日志轮转）──────────────
CHANGED_DOCKER_CONF=0
write_docker_conf() {
    if [ -e "$DOCKER_DAEMON_JSON" ] && [ "$FORCE" -ne 1 ]; then
        info "保留现有 $DOCKER_DAEMON_JSON（--force 可覆盖）"
        return 0
    fi
    if [ "$DRY" -eq 1 ]; then
        printf '  [dry-run] 生成 %s（registry-mirrors=%s）\n' \
            "$DOCKER_DAEMON_JSON" "${DOCKER_MIRRORS[*]:-（无）}"
        return 0
    fi

    info "写入 $DOCKER_DAEMON_JSON"
    mkdir -p "$(dirname "$DOCKER_DAEMON_JSON")"
    if [ -e "$DOCKER_DAEMON_JSON" ]; then
        cp -a "$DOCKER_DAEMON_JSON" "$DOCKER_DAEMON_JSON.bak-$(date +%Y%m%d%H%M%S)"
    fi

    local i n
    {
        printf '{\n'
        if [ "${#DOCKER_MIRRORS[@]}" -gt 0 ]; then
            printf '\t"registry-mirrors": [\n'
            n=${#DOCKER_MIRRORS[@]}
            for ((i = 0; i < n; i++)); do
                if [ "$i" -lt $((n - 1)) ]; then
                    printf '\t\t"%s",\n' "${DOCKER_MIRRORS[$i]}"
                else
                    printf '\t\t"%s"\n' "${DOCKER_MIRRORS[$i]}"
                fi
            done
            printf '\t],\n'
        fi
        printf '\t"live-restore": true,\n'
        printf '\t"log-opts": {\n\t\t"max-size": "512m",\n\t\t"max-file": "3"\n\t}\n'
        printf '}\n'
    } > "$DOCKER_DAEMON_JSON"
    CHANGED_DOCKER_CONF=1
}

if [ "$DO_DOCKER" -eq 1 ] && [ "$DO_CONFIG" -eq 1 ]; then
    write_docker_conf
fi

# ── 启用并启动服务 ───────────────────────────────────────────────────
start_services() {
    [ "$DO_SYSTEMD" -eq 1 ] || { warn "已跳过 systemd 操作（--skip-systemd / 无 systemd）"; return 0; }

    local svc
    for svc in "$@"; do
        info "enable --now $svc.service"
        if [ "$DRY" -eq 1 ]; then
            printf '  [dry-run] systemctl enable --now %s.service\n' "$svc"
            continue
        fi
        systemctl enable --now "$svc.service" || warn "$svc.service 启动失败，请查看：journalctl -u $svc.service -n 50"
    done

    # daemon.json 变更后 Docker 需重启才会生效。
    if [ "$CHANGED_DOCKER_CONF" -eq 1 ]; then
        info "重启 docker.service 使 daemon.json 生效"
        systemctl restart "$DOCKER_SERVICE.service" || warn "docker 重启失败"
    fi
}

SERVICES=()
[ "$DO_DB" -eq 1 ] && SERVICES+=("$DB_SERVICE")
[ "$DO_REDIS" -eq 1 ] && SERVICES+=("$REDIS_SERVICE")
[ "$DO_DOCKER" -eq 1 ] && SERVICES+=("$DOCKER_SERVICE")
start_services "${SERVICES[@]}"

# ── 自检 ─────────────────────────────────────────────────────────────
if [ "$DRY" -ne 1 ]; then
    info "自检"
    if [ "$DO_DB" -eq 1 ]; then
        if command -v mariadb >/dev/null 2>&1 || command -v mysql >/dev/null 2>&1; then
            mysql_bin="$(command -v mariadb || command -v mysql)"
            if "$mysql_bin" -e 'SELECT 1' >/dev/null 2>&1; then
                info "  MariaDB 可连接（$mysql_bin）"
            else
                warn "  MariaDB 连接失败；本地默认走 unix socket，root 口令或 socket 路径见 /etc/mysql"
            fi
        else
            warn "  未找到 mysql/mariadb 客户端"
        fi
    fi
    if [ "$DO_REDIS" -eq 1 ]; then
        if command -v redis-cli >/dev/null 2>&1; then
            if [ "$(redis-cli ping 2>/dev/null || true)" = "PONG" ]; then
                info "  Redis 可连接（redis-cli ping -> PONG）"
            else
                warn "  Redis 连接失败，请查看：journalctl -u $REDIS_SERVICE -n 50"
            fi
        else
            warn "  未找到 redis-cli"
        fi
    fi
    if [ "$DO_DOCKER" -eq 1 ]; then
        if command -v docker >/dev/null 2>&1; then
            info "  Docker 版本：$(docker --version 2>/dev/null || echo 未知)"
        else
            warn "  未找到 docker"
        fi
    fi
fi

# ── 后续步骤提示 ─────────────────────────────────────────────────────
printf '\n环境准备完成。\n'
if [ "$DO_DOCKER" -eq 1 ] && [ "$DO_CONFIG" -eq 1 ] && [ "$CHANGED_DOCKER_CONF" -eq 1 ]; then
    printf '  - Docker 镜像加速已写入 %s（现用镜像：%s）\n' \
        "$DOCKER_DAEMON_JSON" "${DOCKER_MIRRORS[*]:-无}"
fi
printf '  - 部署 hustoj-go：sudo ./install.sh --init-db --db-password ...\n'
if [ "$DO_DB" -eq 1 ]; then
    printf '  - 全新库初始化（若未在 install.sh 里做）：\n'
    printf '      sudo ./init_db.sh --db hustoj --user hustoj --password ... --ojhome /home/judge --write-conf\n'
fi
if [ "$DO_DOCKER" -eq 1 ]; then
    printf '  - 判题语言运行时：cd langs && sudo ./install_langs.sh --ojhome /home/judge\n'
fi
printf '  - nginx 反代到 127.0.0.1:8080（详见 README / docs/web-cutover-checklist.md）\n'
