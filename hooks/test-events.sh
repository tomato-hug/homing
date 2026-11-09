#!/bin/sh
set -eu

BASE_URL="${BASE_URL:-http://127.0.0.1:18787}"

curl -fsS -X POST "$BASE_URL/hook/claude" \
  -H 'Content-Type: application/json' \
  --data '{
    "hook_event_name": "Stop",
    "session_id": "test-claude-session",
    "cwd": "/Users/example/Projects/claude-demo",
    "last_assistant_message": "Claude Code 测试回复：构建已完成。"
  }'

curl -fsS -X POST "$BASE_URL/hook/codex" \
  -H 'Content-Type: application/json' \
  --data '{
    "type": "agent-turn-complete",
    "thread-id": "test-codex-thread",
    "turn-id": "test-codex-turn",
    "cwd": "/Users/example/Projects/codex-demo",
    "input-messages": ["请检查测试"],
    "last-assistant-message": "Codex 测试回复：测试全部通过。"
  }'

curl -fsS -X POST "$BASE_URL/hook/generic" \
  -H 'Content-Type: application/json' \
  --data '{
    "source": "workbuddy",
    "kind": "needs_input",
    "session_id": "test-workbuddy-session",
    "cwd": "/Users/example/Projects/workbuddy-demo",
    "title": "WorkBuddy 等待输入",
    "text": "WorkBuddy 测试事件：请确认下一步。"
  }'

printf '\n测试事件发送完成。\n'
