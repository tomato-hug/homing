#!/usr/bin/env bash
# Homing · Go 版一键自检 + Node/Go 对照
# 用法：在仓库根目录运行  bash scripts/verify-go.sh
#
# 做的事：build / vet / test → 把 Node 的数据目录拷一份新快照到 /tmp/homing-parity →
# 用这份快照在 127.0.0.1:18788 临时起一个 Go 服务 → 跑 parity.sh 和会话诊断 →
# 停掉这个临时服务。不改 Node（18787）的任何数据，也不碰真实数据目录。
# 结果写到 .verify/（*.log 已被 .gitignore 忽略）。
set -u
cd "$(dirname "$0")/.." || exit 2
mkdir -p .verify
LOG=".verify/go-verify.log"
: > "$LOG"
exec > >(tee -a "$LOG") 2>&1

NODE_DATA="$HOME/Library/Application Support/Homing"
SNAP="/tmp/homing-parity"
BIN="/tmp/homing-parity-bin"
GO_PID=""
cleanup() { [[ -n "$GO_PID" ]] && kill "$GO_PID" 2>/dev/null; }
trap cleanup EXIT

echo "== 时间 =="; date
echo "== git =="; git rev-parse --short HEAD; git status --short | head -30
echo "== go 版本 =="; go version || { echo "找不到 go：先 brew install go 或把 go 加进 PATH"; exit 3; }
echo "== go build ./... =="; go build ./... ; echo "build exit=$?"
echo "== go vet ./... =="; go vet ./... ; echo "vet exit=$?"
echo "== go test ./... =="; go test ./... 2>&1 | grep -v "no test files" | tail -60; echo "test exit=${PIPESTATUS[0]}"

echo "== Node 中继（18787）=="
for i in $(seq 1 90); do
  curl -fsS -m 2 http://127.0.0.1:18787/health >/dev/null 2>&1 && { echo "Node /health 正常（等了约 ${i}s）"; break; }
  sleep 1
done
if ! curl -fsS -m 3 http://127.0.0.1:18787/health >/dev/null 2>&1; then
  echo "Node 90 秒内没响应 /health，homingd.log 末尾："
  tail -40 "$NODE_DATA/homingd.log" 2>/dev/null
fi
NODE_PID="$(lsof -nP -t -iTCP:18787 -sTCP:LISTEN 2>/dev/null | head -1)"
if [[ -n "$NODE_PID" ]]; then
  echo "pid=$NODE_PID 启动于: $(ps -o lstart= -p "$NODE_PID")"
  echo "命令: $(ps -o command= -p "$NODE_PID")"
  echo "仓库 relay/server.mjs 修改于: $(stat -f '%Sm' relay/server.mjs)"
else
  echo "18787 没在监听"
fi

if lsof -nP -iTCP:18788 -sTCP:LISTEN >/dev/null 2>&1; then
  echo "== 18788 已被占用：先关掉之前手动起的 homing serve（Ctrl+C），再重跑本脚本 =="
  exit 4
fi

echo "== 新快照 + 临时 Go 服务（18788）=="
rm -rf "$SNAP" && cp -R "$NODE_DATA" "$SNAP" || { echo "拷贝数据目录失败"; exit 5; }
go build -o "$BIN" ./cmd/homing || { echo "编译 cmd/homing 失败"; exit 6; }
HOMING_DATA_DIR="$SNAP" "$BIN" serve --addr 127.0.0.1:18788 --data-dir "$SNAP" > .verify/go-serve.log 2>&1 &
GO_PID=$!
for _ in $(seq 1 40); do
  curl -fsS -m 1 http://127.0.0.1:18788/health >/dev/null 2>&1 && break
  sleep 0.5
done
if ! curl -fsS -m 2 http://127.0.0.1:18788/health >/dev/null 2>&1; then
  echo "Go 服务没起来，日志末尾："; tail -20 .verify/go-serve.log; exit 7
fi
echo "Go 服务 pid=$GO_PID 已就绪"

if curl -fsS -m 3 http://127.0.0.1:18787/health >/dev/null 2>&1; then
  echo "== 对照（18787 vs 18788）：完整输出在 .verify/parity-full.log =="
  bash scripts/parity.sh 18787 18788 > .verify/parity-full.log 2>&1
  grep -E "^(OK|DIFF|FAIL|SKIP)|个接口有差异" .verify/parity-full.log
  echo "== 会话诊断：.verify/diag-sessions.log =="
  bash scripts/diag-sessions.sh >/dev/null 2>&1; head -3 .verify/diag-sessions.log
else
  echo "== 对照跳过：18787（Node）没响应 =="
fi
echo "== 完成 =="
