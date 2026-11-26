#!/bin/sh
# Codex notify 包装脚本。
#
# Codex 在调用 notify 时会把事件 JSON 追加为最后一个参数。本脚本：
#   1) 把原有 notify 命令放进 CHAIN_NOTIFY，由 codex-notify.mjs 原样转发（原有通知照常工作）；
#   2) 让 codex-notify.mjs 把同一段 JSON POST 到本机中继 127.0.0.1:18787。
# 任何失败都静默退出 0，绝不阻塞 Codex 本身。
#
# 可选环境变量：
#   HOMING_NODE_BIN       指定 node 路径（默认自动查找）
#   HOMING_CHAIN_NOTIFY   原有 notify 命令（默认：若装了 Codex Computer Use，则转发给它）
set -u

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" 2>/dev/null && pwd) || exit 0
NOTIFY_SCRIPT="$SCRIPT_DIR/codex-notify.mjs"

NODE_BIN="${HOMING_NODE_BIN:-}"
if [ -z "$NODE_BIN" ]; then
  for candidate in /opt/homebrew/bin/node /usr/local/bin/node; do
    if [ -x "$candidate" ]; then
      NODE_BIN="$candidate"
      break
    fi
  done
fi
if [ -z "$NODE_BIN" ]; then
  NODE_BIN=$(command -v node 2>/dev/null) || exit 0
fi

# 原有 notify 命令：Codex Computer Use 客户端的 "turn-ended" 通知。
# codex-notify.mjs 会在末尾追加带引号的事件 JSON，等价于原来的
# SkyComputerUseClient turn-ended '<json>' 调用。
if [ -z "${HOMING_CHAIN_NOTIFY+x}" ]; then
  SKY_CLIENT="$HOME/.codex/computer-use/Codex Computer Use.app/Contents/SharedSupport/SkyComputerUseClient.app/Contents/MacOS/SkyComputerUseClient"
  if [ -x "$SKY_CLIENT" ]; then
    CHAIN_NOTIFY="\"$SKY_CLIENT\" turn-ended"
  else
    CHAIN_NOTIFY=""
  fi
else
  CHAIN_NOTIFY="$HOMING_CHAIN_NOTIFY"
fi
export CHAIN_NOTIFY

exec "$NODE_BIN" "$NOTIFY_SCRIPT" "$@" 2>/dev/null
