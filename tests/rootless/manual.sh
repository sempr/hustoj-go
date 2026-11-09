#!/usr/bin/env bash
#
# 手动验证 rootless 判题链路：
#   1. 以判题用户运行 spawner + binary，确认能进入 user namespace；
#   2. 手动执行 `hustoj judge` 对 sample AC；
#   3. 检查 spawner 运行期间的 cgroup 是否残留。
#
# 需 root 或者已安装 hustoj-spawn file caps；默认在 /home/judge 下跑。
set -euo pipefail

JUDGE_USER="${JUDGE_USER:-judge}"
OJHOME="${OJHOME:-/home/judge}"
BIN="${BIN:-/usr/local/bin/hustoj}"
SPAWNER="${SPAWNER:-/usr/local/libexec/hustoj-spawn}"

if [ "$(id -u)" -eq 0 ]; then
  RUN="sudo -u $JUDGE_USER"
else
  RUN=
fi

# 1) probe namespace + mount
echo "==> namespace probe"
$RUN env HUSTOJ_SPAWN_ALLOW_ANY=1 "$SPAWNER" --home "$OJHOME" --exe /bin/sh -- -c \
  'id; cat /proc/self/uid_map; mount -t tmpfs -o size=4M,nosuid,nodev tmpfs '"$OJHOME"'/run/.rootless-probe && echo MOUNT_OK && umount '"$OJHOME"'/run/.rootless-probe'

mkdir -p "$OJHOME/run/.rootless-probe" 2>/dev/null || true
chown "$JUDGE_USER:$JUDGE_USER" "$OJHOME/run/.rootless-probe" 2>/dev/null || true

# 2) manual judge
if [ -f "$OJHOME/rtest/Main.cpp" ]; then
  echo "==> manual judge"
  $RUN "$BIN" judge \
    --source "$OJHOME/rtest/Main.cpp" \
    --lang 1 \
    --data "$OJHOME/rtest/data" \
    --time 1000 --memory 262144 --spj 0 --ojhome "$OJHOME"
fi

echo "==> residual spawn cgroups (should be empty)"
ls -d /sys/fs/cgroup/hustoj/spawn-* 2>/dev/null || echo "(none)"

echo "DONE"
