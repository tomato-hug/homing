#!/bin/sh
# Codex notify 包装脚本。
#
# Codex 在调用 notify 时会把事件 JSON 追加为最后一个参数。本脚本：
#   1) 把原有 notify 命令放进 CHAIN_NOTIFY，由 codex-notify.mjs 原样转发（原有通知照常工作）；
#   2) 让 codex-notify.mjs 把同一段 JSON POST 到本机中继 127.0.0.1:18787。
# 任何失败都静默退出 0，绝不阻塞 Codex 本身。
set -u

NODE_BIN="/opt/homebrew/bin/node"
NOTIFY_SCRIPT="/Users/fanqiege/Projects/agent-push-poc/hooks/codex-notify.mjs"

# 原有 notify 命令：Codex Computer Use 客户端的 "turn-ended" 通知。
# codex-notify.mjs 会在末尾追加带引号的事件 JSON，等价于原来的
# SkyComputerUseClient turn-ended '<json>' 调用。
CHAIN_NOTIFY='"/Users/fanqiege/.codex/computer-use/Codex Computer Use.app/Contents/SharedSupport/SkyComputerUseClient.app/Contents/MacOS/SkyComputerUseClient" turn-ended'
export CHAIN_NOTIFY

exec "$NODE_BIN" "$NOTIFY_SCRIPT" "$@" 2>/dev/null
