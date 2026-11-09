# LESSONS

- Codex 的 apply_patch 是 custom_tool_call，不按 name 映射会显示成普通 tool_call；在 internal/agent/codex 里用 parsePatchFiles 映射成 file_change，并保留 files。
- Codex 的退出码不止 "Process exited with code"，常见 "Exit code: 0"；用和 Node 相同的正则 `(?i)(?:Process exited with code|exit code|Exit code:?)\s*(-?\d+)`。
- Codex 注入的系统提示 role=developer，且不都以 `<` 开头；按 role 跳过，用户消息仍按 Node 的 NOISE_PATTERN 过滤，别用前缀 `<` 一刀切。
- verify-go.sh 对照期间当前 Codex 会话仍在写，include_empty 边界条目会有一次性的 last_ts / title 差异；先看 diag-sessions.log 是否只是当前会话。
