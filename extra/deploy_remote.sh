#!/usr/bin/env bash
#
# 编译 hustoj-go 并发布到远程目标机，随后重启服务。
#
# 用法：
#   extra/deploy_remote.sh [选项]
#
# 选项：
#   --target USER@HOST   目标机（默认 hustoj@10.80.1.11）
#   --port PORT          SSH 端口（默认 22）
#   --judge              同时重启 judged-go（改了 daemon/判题代码时使用）
#   --no-langinfo        跳过 FAQ 语言缓存生成（仅文档/样式改动时可省）
#   -h, --help
#
# 前置条件：
#   - 本机有 Go 工具链；
#   - 能用 SSH 密钥免密登录目标机；
#   - 目标机上的用户有免密 sudo 权限。
#
# 发布内容：
#   - 按目标机架构交叉编译（x86_64 → amd64、aarch64 → arm64）；
#   - 覆盖前把旧二进制备份成 /usr/bin/hustoj.bak-<时间戳>；
#   - 默认只重启 hustoj-web；judged-go 仅在 --judge 时重启。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
MODULE="github.com/sempr/hustoj-go"

TARGET="hustoj@10.80.1.11"
PORT=22
RESTART_JUDGE=0
RUN_LANGINFO=1

REMOTE_BIN="/usr/bin/hustoj"
OJ_HOME="/srv/hustoj"
SSH_OPTS=(-o ConnectTimeout=10)

info() { printf '==> %s\n' "$*"; }
die()  { printf '错误: %s\n' "$*" >&2; exit 1; }

usage() { sed -n '3,/^set /p' "$0" | sed -e 's/^# \{0,1\}//' -e '/^set /d'; }

while [ $# -gt 0 ]; do
    case "$1" in
        --target)     TARGET="$2"; shift 2 ;;
        --port)       PORT="$2"; shift 2 ;;
        --judge)      RESTART_JUDGE=1; shift ;;
        --no-langinfo) RUN_LANGINFO=0; shift ;;
        -h|--help)    usage; exit 0 ;;
        *)            die "未知选项: $1" ;;
    esac
done

[ -n "$TARGET" ] || die "目标机不能为空"
command -v go >/dev/null 2>&1 || die "本机缺少 go"

HOST="${TARGET#*@}"
SSH_CMD=(ssh "${SSH_OPTS[@]}" -p "$PORT" "$TARGET")
SCP_CMD=(scp -q -P "$PORT")

info "检查目标机 $TARGET ..."
"${SSH_CMD[@]}" 'sudo -n true' >/dev/null 2>&1 \
    || die "SSH 免密登录或 sudo 不可用: $TARGET"

REMOTE_ARCH="$("${SSH_CMD[@]}" 'uname -m')"
case "$REMOTE_ARCH" in
    x86_64)          GOARCH=amd64 ;;
    aarch64|arm64)   GOARCH=arm64 ;;
    *)               die "不支持的远程架构: $REMOTE_ARCH" ;;
esac
info "远程架构 $REMOTE_ARCH → GOOS=linux GOARCH=$GOARCH"

VERSION="$(git -C "$ROOT" describe --tags --always --dirty)"
COMMIT="$(git -C "$ROOT" rev-parse --short HEAD)"
DATE="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
LDFLAGS="-X '$MODULE/cmd.Version=$VERSION' -X '$MODULE/cmd.Commit=$COMMIT' -X '$MODULE/cmd.Date=$DATE'"

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT
LOCAL_BIN="$TMP_DIR/hustoj"

info "编译 $VERSION ($COMMIT) ..."
# 内置 tini：静态产物由本机 gcc 编出，因此只有目标架构与本机一致时才内嵌。
# 跨架构发布时不带 embed_tini，目标机需要另行分发对应架构的 tini。
TAGS=()
if [ "$GOARCH" = "$(go env GOARCH)" ]; then
    (cd "$ROOT" && make -C tini static) || die "tini 构建失败"
    TAGS=(-tags embed_tini)
else
    warn "目标架构 $GOARCH 与本机 $(go env GOARCH) 不同，本次不内嵌 tini"
fi
(cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$GOARCH" \
    go build "${TAGS[@]}" -trimpath -ldflags "$LDFLAGS" -o "$LOCAL_BIN" .)

info "本机二进制自检 ..."
"$LOCAL_BIN" version

REMOTE_TMP="/tmp/hustoj-$COMMIT-$$.new"
info "上传到 $TARGET:$REMOTE_TMP ..."
"${SCP_CMD[@]}" "$LOCAL_BIN" "$TARGET:$REMOTE_TMP"

LOCAL_SUM="$(sha256sum "$LOCAL_BIN" | awk '{print $1}')"
REMOTE_SUM="$("${SSH_CMD[@]}" "sha256sum '$REMOTE_TMP'" | awk '{print $1}')"
[ "$LOCAL_SUM" = "$REMOTE_SUM" ] || die "上传校验失败（本机 $LOCAL_SUM，远端 $REMOTE_SUM）"

info "安装并重启服务 ..."
"${SSH_CMD[@]}" bash -s -- \
    "$REMOTE_TMP" "$REMOTE_BIN" "$OJ_HOME" "$RUN_LANGINFO" "$RESTART_JUDGE" "$HOST" <<'REMOTE'
set -euo pipefail
TMP_BIN=$1
BIN=$2
OJHOME=$3
LANGINFO=$4
JUDGE=$5
HOST=$6

ts=$(date +%Y%m%d%H%M%S)
if [ -f "$BIN" ]; then
    sudo cp -a "$BIN" "$BIN.bak-$ts"
    echo "backup: $BIN.bak-$ts"
fi
sudo install -m 0755 "$TMP_BIN" "$BIN"
rm -f "$TMP_BIN"
"$BIN" version

# 落盘内置的 tini（md5 一致时自动跳过），省掉单独分发 /usr/local/bin/tini。
if ! sudo "$BIN" install-tini; then
    echo "警告: install-tini 失败（可能是未内嵌 tini 的跨架构构建）" >&2
fi

if [ "$LANGINFO" = "1" ]; then
    sudo "$BIN" langinfo --ojhome "$OJHOME"
fi

sudo systemctl restart hustoj-web.service
if [ "$JUDGE" = "1" ]; then
    sudo systemctl restart judged-go.service
fi

sleep 2
systemctl is-active hustoj-web.service judged-go.service
curl -fsS -m 5 -o /dev/null -w 'healthz=%{http_code}\n' "http://$HOST:8080/healthz"
REMOTE

info "发布完成: $VERSION"
