#!/usr/bin/env bash
# Homing · 从 Go 版切回 Node 中继（macOS）
#
# 用法：bash scripts/rollback-to-node.sh [--restore-data]
#   --restore-data  同时把数据目录恢复到切换前的备份（会丢掉切换之后的新事件和配对）
#
# 使用 scripts/switch-to-go.sh 留下的最新备份。日志：.verify/rollback-to-node.log
set -u
cd "$(dirname "$0")/.." || exit 2
mkdir -p .verify
exec > >(tee "$PWD/.verify/rollback-to-node.log") 2>&1

UID_N="$(id -u)"
DATA="$HOME/Library/Application Support/Homing"
BACKUP="$DATA/backup"
LA="$HOME/Library/LaunchAgents"
NODE_LABEL="dev.agentpush.relay"
GO_LABEL="dev.homing.homingd"
PORT=18787
STAMP="$(date +%Y%m%d-%H%M%S)"

NODE_BACKUP="$(ls -t "$BACKUP/$NODE_LABEL".plist.* 2>/dev/null | head -1)"
[ -n "$NODE_BACKUP" ] || { echo "找不到 Node LaunchAgent 的备份（${BACKUP}），无法回滚"; exit 1; }
echo "使用备份：$NODE_BACKUP"

echo "== 停止 Go 版 =="
launchctl bootout "gui/$UID_N/$GO_LABEL" >/dev/null 2>&1 || true
[ -f "$LA/$GO_LABEL.plist" ] && mv "$LA/$GO_LABEL.plist" "$BACKUP/$GO_LABEL.plist.rolledback-$STAMP"
for _ in $(seq 1 20); do
  lsof -nP -iTCP:$PORT -sTCP:LISTEN >/dev/null 2>&1 || break
  sleep 0.5
done

if [ "${1:-}" = "--restore-data" ]; then
  DATA_BACKUP="$(ls -t "$BACKUP"/data-*.tgz 2>/dev/null | head -1)"
  if [ -n "$DATA_BACKUP" ]; then
    echo "== 恢复数据目录：$DATA_BACKUP =="
    ( cd "$DATA" && tar -czf "$BACKUP/data-before-rollback-$STAMP.tgz" --exclude ./backup --exclude ./bin . )
    ( cd "$DATA" && tar -xzf "$DATA_BACKUP" )
  else
    echo "没有数据备份，跳过"
  fi
fi

echo "== 启动 Node =="
cp "$NODE_BACKUP" "$LA/$NODE_LABEL.plist"
launchctl bootstrap "gui/$UID_N" "$LA/$NODE_LABEL.plist" >/dev/null 2>&1 || launchctl kickstart -k "gui/$UID_N/$NODE_LABEL"
for _ in $(seq 1 60); do
  if curl -fsS -m 2 "http://127.0.0.1:$PORT/health" >/dev/null 2>&1; then
    echo "Node 已恢复，手机会自动重连。"
    lsof -nP -iTCP:$PORT -sTCP:LISTEN | tail -n +2
    exit 0
  fi
  sleep 1
done
echo "Node 没有在 60 秒内恢复，请把 .verify/rollback-to-node.log 发给 Claude。"
exit 1
