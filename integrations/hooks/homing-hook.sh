#!/bin/sh
# 通用 Homing 钩子：stdin 收 JSON，POST 到本机 /hook/generic。
# 用法：
#   echo '{"session_id":"demo-1","kind":"done","text":"ok"}' | HOMING_AGENT=my-agent ./homing-hook.sh
# 第一个参数也可以指定 agent id；HOMING_HOOK_URL 可指向别的端口。
set -u

BASE_URL="${HOMING_HOOK_URL:-http://127.0.0.1:18787/hook/generic}"
SOURCE="${1:-${HOMING_AGENT:-}}"
KIND="${HOMING_KIND:-}"
payload="$(cat)"

[ -n "$payload" ] || exit 0

curl --connect-timeout 1 --max-time 2 -fsS -X POST "$BASE_URL" \
  -H 'Content-Type: application/json' \
  -H "X-Homing-Agent: ${SOURCE}" \
  -H "X-Homing-Kind: ${KIND}" \
  --data-binary "$payload" >/dev/null 2>&1 || true

# 钩子不能阻塞 agent；解析失败、服务没起、超时都静默放过。
exit 0
