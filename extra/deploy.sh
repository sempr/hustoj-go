#!/bin/bash
#
# 重新编译 hustoj-go 并部署到生产环境，随后重启 systemd 服务。
#
# 用法: ./deploy.sh
#
#   1. 用 Makefile 相同的 ldflags 编译二进制（内嵌版本/提交/日期）。
#   2. 备份老的 /usr/bin/hustoj-go 和 hustoj-go-web。
#   3. 安装新二进制到 /usr/bin/hustoj-go 和 /usr/bin/hustoj-go-web。
#   4. 运行 hustoj-go langinfo --ojhome 生成 FAQ 语言信息缓存。
#   5. 重启 hustoj-go-web 和 judged-go 服务。

set -euo pipefail

MODULE="github.com/sempr/hustoj-go"
JUDGEHOME="/home/judge"
BIN_DIR="/usr/bin"
WEB_BIN="$BIN_DIR/hustoj-go-web"
DAEMON_BIN="$BIN_DIR/hustoj-go"

echo "==> 编译二进制……"
VERSION=$(git -C "$(dirname "$0")/.." describe --tags --always)
COMMIT=$(git -C "$(dirname "$0")/.." rev-parse --short HEAD)
DATE=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS="-X '$MODULE/cmd.Version=$VERSION' -X '$MODULE/cmd.Commit=$COMMIT' -X '$MODULE/cmd.Date=$DATE'"

cd "$(dirname "$0")/.."
go build -trimpath -ldflags "$LDFLAGS" -o hustoj .

echo "==> 备份旧二进制……"
ts=$(date +%Y%m%d%H%M%S)
for f in "$WEB_BIN" "$DAEMON_BIN"; do
    if [ -f "$f" ]; then
        cp -a "$f" "${f}.bak-$ts"
    fi
done

echo "==> 安装新二进制……"
install -m 0755 hustoj "$DAEMON_BIN"
install -m 0755 hustoj "$WEB_BIN"
rm -f hustoj

echo "==> 生成 FAQ 语言信息缓存……"
"$DAEMON_BIN" langinfo --ojhome "$JUDGEHOME"

echo "==> 重启服务……"
systemctl restart judged-go.service
systemctl restart hustoj-go-web.service

echo "==> 部署完成"
systemctl --no-pager status hustoj-go-web.service | head -8
