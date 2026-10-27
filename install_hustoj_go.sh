#!/usr/bin/env bash
#
# HUSTOJ-Go 一键安装脚本（本地 / 远程两种模式）
#
# 脚本随发布包一起分发。在解压后的发布包目录里运行时会自动使用本地模式；
# 在仓库/构建机运行时，默认把 dist/ 下的部署包推到 10.80.1.12 上执行。
#
# 完成内容：
#   - MariaDB / Redis / Docker / nginx（Ubuntu/Debian + systemd）
#   - hustoj-go 二进制、systemd 服务，OJ 根目录 /srv/hustoj
#   - 初始化 hustoj 库 + 触发器 + admin 管理员
#   - 示例题（5 题）+ 示例比赛 + 多语言 AC / 部分分 / 各类判罚演示提交
#   - 12 种判题运行时（C/C++/Pascal/Java/Bash/Python/PHP/Perl/C#/
#     TypeScript/Go/Rust，Docker rootfs）
#   - nginx 80 端口反向代理到 127.0.0.1:8080（不配 SSL）
#   - admin 密码写入 /root/admin.pass.txt（0600）
#
# 用法：
#   # A. 目标机上：解压发布包后直接执行（自动本地模式）
#   sudo ./install_hustoj_go.sh --local
#
#   # B. 构建机上：推送到远程目标
#   ./install_hustoj_go.sh                         # 部署到 hustoj@10.80.1.12
#   ./install_hustoj_go.sh --target root@10.80.1.12
#   ./install_hustoj_go.sh --local --package ./dist/hustoj-go-*.tar.gz
#
# 常用选项：
#   --target USER@HOST   远程目标（默认 hustoj@10.80.1.12）
#   --local              在目标机本机执行（需 root）
#   --package FILE|DIR   部署包 tar.gz 或已解压的包目录；缺省在脚本目录/dist
#                        找最新的 amd64 包，找不到且本机有 Go 时自动构建
#   --ojhome DIR         OJ 根目录（默认 /srv/hustoj）
#   --db-name NAME       数据库名（默认 hustoj）
#   --db-user USER       数据库账号（默认 hustoj）
#   --langs "0 1 6"      安装的判题语言 id（默认随包全部 12 种）
#   --no-samples         不装载演示提交（题目/比赛仍会装载）
#   --no-docker-mirror   不写 Docker 镜像加速配置
#   --skip-apt           跳过 apt-get install（软件已装好时）
#   --help
#
# 说明：
#   * 数据库账号密码随机生成，只写入 /srv/hustoj/etc/judge.conf（0600）。
#   * admin 密码首次随机生成；重复执行时沿用 /root/admin.pass.txt 并校正库中口令。
#   * 数据库已有表时不会重建；示例题/比赛装载是幂等的，演示提交按源码去重。
#
set -euo pipefail

SELF="$(readlink -f "$0")"
SELF_DIR="$(cd "$(dirname "$SELF")" && pwd)"

# 发布包内同时带有本脚本、install.sh 和 bin/hustoj；检测到这种布局时
# 默认本地安装，省掉 --package 参数。
BUNDLED=0
if [ -f "$SELF_DIR/install.sh" ] && [ -f "$SELF_DIR/bin/hustoj" ]; then
    BUNDLED=1
fi

MODE=remote
TARGET_SET=0
TARGET="hustoj@10.80.1.12"
OJHOME=/srv/hustoj
DB_NAME=hustoj
DB_USER=hustoj
LANG_IDS="0 1 2 3 5 6 7 8 9 16 17 25"
PACKAGE=""
DO_SAMPLES=1
DO_APT=1
DOCKER_MIRROR=1
ADMIN_USER=admin

info() { printf '\n==> %s\n' "$*"; }
warn() { printf '警告: %s\n' "$*" >&2; }
die()  { printf '错误: %s\n' "$*" >&2; exit 1; }

usage() { sed -n '2,/^set /p' "$SELF" | sed -e 's/^# \{0,1\}//' -e '/^set /d'; }

while [ $# -gt 0 ]; do
    case "$1" in
        --target)             TARGET="$2"; MODE=remote; TARGET_SET=1; shift 2 ;;
        --local)              MODE=local; shift ;;
        --package)            PACKAGE="$2"; shift 2 ;;
        --ojhome)             OJHOME="$2"; shift 2 ;;
        --db-name)            DB_NAME="$2"; shift 2 ;;
        --db-user)            DB_USER="$2"; shift 2 ;;
        --langs)              LANG_IDS="$2"; shift 2 ;;
        --no-samples)         DO_SAMPLES=0; shift ;;
        --no-docker-mirror)   DOCKER_MIRROR=0; shift ;;
        --skip-apt)           DO_APT=0; shift ;;
        -h|--help)            usage; exit 0 ;;
        *)                    die "未知参数: $1（用 --help 查看用法）" ;;
    esac
done

for v in "$OJHOME"; do
    case "$v" in /*) ;; *) die "路径必须是绝对路径: $v" ;; esac
done

if [ "$BUNDLED" -eq 1 ] && [ "$TARGET_SET" -eq 0 ]; then
    MODE=local
fi

find_package() {
    local p="$1"
    if [ -n "$p" ]; then
        if [ -d "$p" ]; then
            [ -x "$p/install.sh" ] || die "目录不是完整的部署包（缺少 install.sh）: $p"
            ( cd "$p" && pwd )
            return 0
        fi
        [ -f "$p" ] || die "找不到部署包: $p"
        readlink -f "$p"
        return 0
    fi
    local cand
    cand="$(find "$SELF_DIR/dist" "$SELF_DIR" -maxdepth 1 -type f \
        -name 'hustoj-go-*-linux-amd64.tar.gz' -printf '%T@ %p\n' 2>/dev/null \
        | sort -nr | head -1 | cut -d' ' -f2- || true)"
    if [ -n "$cand" ]; then
        readlink -f "$cand"
        return 0
    fi
    if command -v go >/dev/null 2>&1 && [ -f "$SELF_DIR/extra/package.sh" ]; then
        info "本机没有部署包，使用 Go 构建 linux/amd64 包" >&2
        ( cd "$SELF_DIR" && bash extra/package.sh linux/amd64 --out "$SELF_DIR/dist" >&2 )
        cand="$(find "$SELF_DIR/dist" -maxdepth 1 -type f \
            -name 'hustoj-go-*-linux-amd64.tar.gz' -printf '%T@ %p\n' \
            | sort -nr | head -1 | cut -d' ' -f2-)"
        [ -n "$cand" ] || die "构建完成但没找到部署包"
        readlink -f "$cand"
        return 0
    fi
    die "找不到部署包；请用 --package 指定，或在有 Go 的构建机上运行"
}

# ── 远程模式：把脚本和部署包送到目标机，再以 root 执行本地模式 ────────
if [ "$MODE" = "remote" ]; then
    command -v ssh >/dev/null 2>&1 || die "需要 ssh"
    command -v scp >/dev/null 2>&1 || die "需要 scp"
    PACKAGE="$(find_package "$PACKAGE")"
    [ -f "$PACKAGE" ] || die "远程模式需要 tar.gz 部署包；已解压目录请在被控机上用 --local"

    remote_dir="/tmp/hustoj-oneclick"
    info "目标：$TARGET"
    info "部署包：$PACKAGE"

    ssh -o BatchMode=yes "$TARGET" "rm -rf '$remote_dir' && mkdir -p '$remote_dir'" \
        || die "无法通过 SSH 连接 $TARGET（请确认公钥登录可用）"
    scp -q -o BatchMode=yes "$PACKAGE" "$SELF" "$TARGET:$remote_dir/" \
        || die "复制文件到 $TARGET 失败"

    remote_args=(--local --package "$remote_dir/$(basename "$PACKAGE")"
                 --ojhome "$OJHOME" --db-name "$DB_NAME" --db-user "$DB_USER"
                 --langs "$LANG_IDS")
    [ "$DO_SAMPLES" -eq 1 ] || remote_args+=(--no-samples)
    [ "$DOCKER_MIRROR" -eq 1 ] || remote_args+=(--no-docker-mirror)
    [ "$DO_APT" -eq 1 ] || remote_args+=(--skip-apt)

    # shellcheck disable=SC2029
    ssh "$TARGET" "sudo -n bash '$remote_dir/$(basename "$SELF")' $(printf '%q ' "${remote_args[@]}")" \
        || die "远程安装失败；可在目标机查看 journalctl -u hustoj-web -u judged-go"
    exit 0
fi

# ── 本地模式 ────────────────────────────────────────────────────────
[ "$(id -u)" -eq 0 ] || die "本地模式需要 root（sudo ./$(basename "$SELF") --local ...）"
command -v apt-get >/dev/null 2>&1 || die "仅支持 Debian/Ubuntu（未找到 apt-get）"
command -v systemctl >/dev/null 2>&1 || die "需要 systemd"

# 发布包内运行时，默认使用当前目录，无需再找 tar.gz。
if [ -z "$PACKAGE" ] && [ "$BUNDLED" -eq 1 ]; then
    PACKAGE="$SELF_DIR"
fi
PACKAGE="$(find_package "$PACKAGE")"
info "本地安装：$PACKAGE -> $OJHOME"

WORK=""
cleanup() { if [ -n "$WORK" ]; then rm -rf "$WORK"; fi; }
trap cleanup EXIT

if [ -d "$PACKAGE" ]; then
    PKG="$PACKAGE"
else
    WORK="$(mktemp -d /tmp/hustoj-install.XXXXXX)"
    tar -xzf "$PACKAGE" -C "$WORK" --strip-components=1
    PKG="$WORK"
fi
[ -x "$PKG/install.sh" ] || die "部署包不完整（缺少 install.sh）"

export DEBIAN_FRONTEND=noninteractive

if [ "$DO_APT" -eq 1 ]; then
    info "安装系统依赖（MariaDB / Redis / Docker / nginx）"
    apt-get update -qq
    apt-get install -y -qq mariadb-server mariadb-client redis-server docker.io \
        nginx curl ca-certificates openssl >/dev/null
fi

info "启用系统服务"
systemctl enable --now mariadb.service redis-server.service docker.service nginx.service

if [ "$DOCKER_MIRROR" -eq 1 ] && [ ! -f /etc/docker/daemon.json ]; then
    info "写入 Docker 镜像加速配置"
    mkdir -p /etc/docker
    cat > /etc/docker/daemon.json <<'EOF'
{
	"registry-mirrors": ["https://docker.1ms.run", "https://docker.xuanyuan.me"],
	"live-restore": true,
	"log-opts": {"max-size": "512m", "max-file": "3"}
}
EOF
    systemctl restart docker.service
fi

info "创建运行用户与目录"
id judge >/dev/null 2>&1 || useradd --system --home-dir "$OJHOME" --shell /usr/sbin/nologin judge
mkdir -p "$OJHOME/src/web" "$OJHOME/data" "$OJHOME/etc/langs" "$OJHOME/run" "$OJHOME/var/rootfs"
chown -R judge:judge "$OJHOME/src" "$OJHOME/data" 2>/dev/null || true

# ── 随机口令 ─────────────────────────────────────────────────────────
rand_hex() { openssl rand -hex 16; }
rand_pass() { openssl rand -base64 24 | tr -dc 'A-Za-z0-9' | cut -c1-16; }

ADMIN_PASS_FILE=/root/admin.pass.txt
if [ -s "$ADMIN_PASS_FILE" ]; then
    ADMIN_PASS="$(head -n1 "$ADMIN_PASS_FILE" | tr -d '\r\n')"
else
    ADMIN_PASS="$(rand_pass)"
fi
[ -n "$ADMIN_PASS" ] || ADMIN_PASS="$(rand_pass)"

DB_PASS=""
if [ -f "$OJHOME/etc/judge.conf" ]; then
    DB_PASS="$(sed -n 's/^OJ_PASSWORD=//p' "$OJHOME/etc/judge.conf" | head -1 | tr -d '\r')"
fi
[ -n "$DB_PASS" ] || DB_PASS="$(rand_hex)"

# ── 数据库是否已初始化 ───────────────────────────────────────────────
DB_TABLES=0
if command -v mysql >/dev/null 2>&1; then
    DB_TABLES="$(mysql -N -B -e \
        "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='$DB_NAME'" 2>/dev/null || echo 0)"
fi

install_args=(--ojhome "$OJHOME" --demo)
if [ "${DB_TABLES:-0}" -eq 0 ]; then
    info "初始化数据库（新库）"
    install_args+=(--init-db --db-password "$DB_PASS"
                   --db-admin-user "$ADMIN_USER" --db-admin-password "$ADMIN_PASS")
else
    info "检测到数据库 '$DB_NAME' 已有 ${DB_TABLES} 张表，跳过初始化"
    if [ ! -f "$OJHOME/etc/judge.conf" ]; then
        die "数据库已存在但缺少 $OJHOME/etc/judge.conf；请先恢复配置或手动初始化"
    fi
fi

info "安装 hustoj-go 并装载示例题/比赛"
( cd "$PKG" && ./install.sh "${install_args[@]}" )

# ── 校正 admin 口令并落盘 ────────────────────────────────────────────
info "校正管理员 '$ADMIN_USER' 口令"
sql_tmp="$(mktemp)"
cat > "$sql_tmp" <<EOF
INSERT INTO users (user_id, password, reg_time, nick, email, defunct)
VALUES ('$ADMIN_USER', MD5('$ADMIN_PASS'), NOW(), '$ADMIN_USER', '', 'N')
ON DUPLICATE KEY UPDATE password=VALUES(password), defunct='N';
INSERT INTO privilege (user_id, rightstr, valuestr, defunct)
SELECT '$ADMIN_USER', 'administrator', 'true', 'N'
WHERE NOT EXISTS (SELECT 1 FROM privilege WHERE user_id='$ADMIN_USER' AND rightstr='administrator');
EOF
mysql "$DB_NAME" < "$sql_tmp"
rm -f "$sql_tmp"
umask 077
printf '%s\n' "$ADMIN_PASS" > "$ADMIN_PASS_FILE"
chmod 0600 "$ADMIN_PASS_FILE"
chown root:root "$ADMIN_PASS_FILE"
info "管理员密码已写入 $ADMIN_PASS_FILE"

# ── 判题语言运行时 ───────────────────────────────────────────────────
info "准备判题语言：$LANG_IDS"
LANGS_SRC="$PKG/langs"
# 兼容旧版安装：早期生成的 *.lang.toml 可能把 base 指向 Docker overlay2
# 的镜像层路径。镜像层被清理后判题会报 "language rootfs not found"；
# 这里统一改指已拍平的 $OJHOME/var/rootfs/<id>。
for f in "$OJHOME"/etc/langs/*.lang.toml; do
    [ -f "$f" ] || continue
    id="$(basename "$f" .lang.toml)"
    root="$OJHOME/var/rootfs/$id"
    if [ -d "$root" ] && grep -q '^base *= *"/var/lib/docker/' "$f"; then
        tmp="$(mktemp)"
        sed "s|^base *=.*|base = \"$root\"|" "$f" > "$tmp"
        install -m 0644 "$tmp" "$f"
        rm -f "$tmp"
        info "修正 $id.lang.toml 的 base -> $root"
    fi
done
langs_ready=1
for id in $LANG_IDS; do
    if [ ! -d "$OJHOME/var/rootfs/$id" ] || [ ! -f "$OJHOME/etc/langs/$id.lang.toml" ]; then
        langs_ready=0
        break
    fi
done
if [ "$langs_ready" -eq 1 ]; then
    info "语言运行时已存在，跳过 Docker 构建"
else
(
    cd "$LANGS_SRC"
    # 只保留需要的纯数字语言目录；etc/ 等辅助目录必须保留。
    for d in */; do
        id="${d%/}"
        [ -n "$id" ] || continue
        case "$id" in *[!0-9]*) continue ;; esac
        keep=0
        for want in $LANG_IDS; do [ "$id" = "$want" ] && keep=1 && break; done
        [ "$keep" -eq 1 ] || rm -rf "$id"
    done
    # 语言注册表里去掉未安装语言的配置，避免 Web 把它们列为可用语言。
    for f in etc/langs/*.lang.toml; do
        id="$(basename "$f" .lang.toml)"
        keep=0
        for want in $LANG_IDS; do [ "$id" = "$want" ] && keep=1 && break; done
        [ "$keep" -eq 1 ] || rm -f "$f"
    done
    ./install_langs.sh --ojhome "$OJHOME"
)
fi
systemctl restart judged-go.service

# ── nginx 反向代理（无 SSL）─────────────────────────────────────────
info "配置 nginx（80 -> 127.0.0.1:8080）"
cat > /etc/nginx/sites-available/hustoj <<'EOF'
server {
    listen 80 default_server;
    listen [::]:80 default_server;
    server_name _;

    client_max_body_size 64m;
    access_log /var/log/nginx/hustoj.access.log;
    error_log  /var/log/nginx/hustoj.error.log;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header Upgrade           $http_upgrade;
        proxy_set_header Connection        "upgrade";
        proxy_read_timeout 120s;
        proxy_send_timeout 120s;
    }
}
EOF
rm -f /etc/nginx/sites-enabled/default
ln -sfn /etc/nginx/sites-available/hustoj /etc/nginx/sites-enabled/hustoj
nginx -t
systemctl enable --now nginx.service
systemctl reload nginx.service

# ── 演示提交（按源码去重，可重复执行）───────────────────────────────
if [ "$DO_SAMPLES" -eq 1 ]; then
    existing_solutions="$(mysql -N -B "$DB_NAME" -e \
        "SELECT COUNT(*) FROM solution WHERE user_id='$ADMIN_USER'" 2>/dev/null || echo 0)"
    info "生成演示提交（多语言 AC / 部分分 / 各类判罚；已有 $existing_solutions 条，脚本按源码去重）"
    ( cd "$PKG" && ./demo/seed_samples.sh --ojhome "$OJHOME" --pid-base 1000 --user "$ADMIN_USER" )

    info "等待判题完成（最多 10 分钟）"
    judged=0
    for _ in $(seq 1 120); do
        pending="$(mysql -N -B "$DB_NAME" -e \
            "SELECT COUNT(*) FROM solution WHERE result=0 OR result IS NULL" 2>/dev/null || echo 1)"
        if [ "${pending:-1}" -eq 0 ]; then judged=1; break; fi
        sleep 5
    done
    if [ "$judged" -eq 1 ]; then
        info "判题队列已清空"
    else
        warn "仍有提交未判完；查看 journalctl -u judged-go.service -n 100"
    fi
fi

# ── 自检 ────────────────────────────────────────────────────────────
info "自检"
health=0
for _ in 1 2 3 4 5; do
    curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1 && health=1 && break
    sleep 1
done
[ "$health" -eq 1 ] || warn "Web /healthz 未通过"
for path in / /problemset.php /status.php /contest.php /ranklist.php; do
    code="$(curl -sS -o /dev/null -w '%{http_code}' "http://127.0.0.1$path" || true)"
    printf '  %-18s %s\n' "$path" "$code"
done
printf '  题目数：%s  比赛数：%s  提交数：%s  管理员：%s\n' \
    "$(mysql -N -B "$DB_NAME" -e 'SELECT COUNT(*) FROM problem')" \
    "$(mysql -N -B "$DB_NAME" -e 'SELECT COUNT(*) FROM contest')" \
    "$(mysql -N -B "$DB_NAME" -e 'SELECT COUNT(*) FROM solution')" \
    "$ADMIN_USER"

ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
printf '\n安装完成。\n'
printf '  访问地址：http://%s/\n' "${ip:-<本机IP>}"
printf '  管理员：  %s\n' "$ADMIN_USER"
printf '  密码文件：%s\n' "$ADMIN_PASS_FILE"
printf '  OJ 根目录：%s\n' "$OJHOME"
