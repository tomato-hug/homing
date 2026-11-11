#!/usr/bin/env bash
# Homing · 把 18787 从 Node 中继切换到 Go 版（macOS）
#
# 用法：在仓库根目录运行   bash scripts/switch-to-go.sh
#
# 做的事（任何一步失败都会自动切回 Node）：
#   1. 编译 Go 版到 ~/.homing/bin/homing
#   2. 备份 Node 的 LaunchAgent 和数据目录里的关键文件
#   3. 停掉 Node（dev.agentpush.relay），把它的 plist 移到备份目录，登录时不再拉起
#   4. 安装 Go 版的 LaunchAgent（dev.homing.homingd，监听 0.0.0.0:18787）并启动
#   5. 检查本机和局域网地址的 /health
#
# 回滚：bash scripts/rollback-to-node.sh
# 日志：.verify/switch-to-go.log
set -u
cd "$(dirname "$0")/.." || exit 2
REPO="$(pwd)"
mkdir -p .verify
LOG="$REPO/.verify/switch-to-go.log"
: > "$LOG"
exec > >(tee -a "$LOG") 2>&1

UID_N="$(id -u)"
DATA="$HOME/Library/Application Support/Homing"
BACKUP="$DATA/backup"
STAMP="$(date +%Y%m%d-%H%M%S)"
BIN_DIR="$HOME/.homing/bin"
BIN="$BIN_DIR/homing"
LA="$HOME/Library/LaunchAgents"
NODE_LABEL="dev.agentpush.relay"
NODE_PLIST="$LA/$NODE_LABEL.plist"
GO_LABEL="dev.homing.homingd"
GO_PLIST="$LA/$GO_LABEL.plist"
PORT=18787

step() { echo; echo "== $* =="; }
die() { echo "失败：$*"; exit 1; }

health() { curl -fsS -m 2 "http://$1:$PORT/health" >/dev/null 2>&1; }

wait_health() { # host seconds
  for _ in $(seq 1 "$2"); do health "$1" && return 0; sleep 1; done
  return 1
}

wait_port_free() {
  for _ in $(seq 1 20); do
    lsof -nP -iTCP:$PORT -sTCP:LISTEN >/dev/null 2>&1 || return 0
    sleep 0.5
  done
  return 1
}

restore_node() {
  echo
  echo "== 切回 Node =="
  launchctl bootout "gui/$UID_N/$GO_LABEL" >/dev/null 2>&1 || true
  [ -f "$GO_PLIST" ] && mv "$GO_PLIST" "$BACKUP/$GO_LABEL.plist.failed-$STAMP"
  wait_port_free || true
  if [ -f "$BACKUP/$NODE_LABEL.plist.$STAMP" ] && [ ! -f "$NODE_PLIST" ]; then
    cp "$BACKUP/$NODE_LABEL.plist.$STAMP" "$NODE_PLIST"
  fi
  if [ -f "$NODE_PLIST" ]; then
    launchctl bootstrap "gui/$UID_N" "$NODE_PLIST" >/dev/null 2>&1 || launchctl kickstart -k "gui/$UID_N/$NODE_LABEL" >/dev/null 2>&1 || true
  fi
  if wait_health 127.0.0.1 60; then
    echo "Node 已恢复，手机会自动重连。"
  else
    echo "Node 没有在 60 秒内恢复，请把 .verify/switch-to-go.log 发给 Claude。"
  fi
}

step "检查环境"
[ "$(uname -s)" = "Darwin" ] || die "这个脚本只用于 macOS"
command -v go >/dev/null 2>&1 || die "找不到 go（先 brew install go）"
go version
mkdir -p "$BACKUP" "$BIN_DIR"
if [ -f "$GO_PLIST" ] && launchctl print "gui/$UID_N/$GO_LABEL" >/dev/null 2>&1 && health 127.0.0.1; then
  echo "Go 版已经在运行（$GO_LABEL），无需切换。"
  exit 0
fi

step "编译 Go 版"
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
if go build -trimpath -ldflags "-X main.version=$VERSION" -o "$BIN.new" ./cmd/homing; then
  echo "已编译带桌面界面的版本"
elif go build -tags headless -trimpath -ldflags "-X main.version=$VERSION" -o "$BIN.new" ./cmd/homing; then
  echo "桌面界面编译失败，改用无界面版本（服务功能相同）"
else
  die "go build 失败，Node 未改动"
fi
"$BIN.new" version || die "新编译的程序无法运行，Node 未改动"
[ -f "$BIN" ] && cp "$BIN" "$BACKUP/homing.$STAMP"
mv "$BIN.new" "$BIN"
echo "$BIN"

step "备份"
if [ -f "$NODE_PLIST" ]; then
  cp "$NODE_PLIST" "$BACKUP/$NODE_LABEL.plist.$STAMP"
  echo "Node LaunchAgent -> $BACKUP/$NODE_LABEL.plist.$STAMP"
else
  echo "没有找到 $NODE_PLIST（Node 可能是手动启动的）"
fi
( cd "$DATA" && tar -czf "$BACKUP/data-$STAMP.tgz" --exclude ./backup --exclude ./bin --exclude './*.log' . ) \
  && echo "数据目录 -> $BACKUP/data-$STAMP.tgz" \
  || die "数据目录备份失败，Node 未改动"

step "停止 Node"
launchctl bootout "gui/$UID_N/$NODE_LABEL" >/dev/null 2>&1 || true
if [ -f "$NODE_PLIST" ]; then
  mv "$NODE_PLIST" "$BACKUP/$NODE_LABEL.plist.disabled-$STAMP"
fi
if ! wait_port_free; then
  PID="$(lsof -nP -t -iTCP:$PORT -sTCP:LISTEN | head -1)"
  echo "端口 $PORT 仍被占用：$(ps -o command= -p "$PID")"
  restore_node
  die "端口没有释放，已切回 Node"
fi
echo "18787 已空出"

step "安装并启动 Go 版"
cat > "$GO_PLIST" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>$GO_LABEL</string>
	<key>ProgramArguments</key>
	<array>
		<string>$BIN</string>
		<string>serve</string>
		<string>--addr</string>
		<string>0.0.0.0:$PORT</string>
	</array>
	<key>WorkingDirectory</key>
	<string>$DATA</string>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:$HOME/.local/bin</string>
		<key>APK_PATH</key>
		<string>$REPO/android/app/build/outputs/apk/debug/app-debug.apk</string>
	</dict>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>ProcessType</key>
	<string>Background</string>
	<key>StandardOutPath</key>
	<string>$DATA/homingd.log</string>
	<key>StandardErrorPath</key>
	<string>$DATA/homingd.log</string>
</dict>
</plist>
PLIST
plutil -lint "$GO_PLIST" >/dev/null || { restore_node; die "plist 格式错误，已切回 Node"; }
launchctl bootstrap "gui/$UID_N" "$GO_PLIST" || { restore_node; die "launchctl bootstrap 失败，已切回 Node"; }

step "验证"
if ! wait_health 127.0.0.1 30; then
  echo "Go 版 30 秒内没有响应，homingd.log 末尾："
  tail -30 "$DATA/homingd.log"
  restore_node
  die "Go 版没有起来，已切回 Node"
fi
echo "本机 127.0.0.1:$PORT 正常"
LAN_IP="$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || true)"
if [ -n "$LAN_IP" ]; then
  if health "$LAN_IP"; then
    echo "局域网 $LAN_IP:$PORT 正常（手机可以直连）"
  else
    echo "警告：局域网地址 $LAN_IP:$PORT 访问不到。检查 系统设置 → 网络 → 防火墙 是否拦截了 homing。"
  fi
else
  echo "没有查到局域网 IP（未连 Wi‑Fi？），跳过局域网检查"
fi
lsof -nP -iTCP:$PORT -sTCP:LISTEN | tail -n +2
if curl -fsS -m 3 -o /dev/null -I "http://127.0.0.1:$PORT/apk"; then
  echo "安卓安装包可从 http://${LAN_IP:-<电脑IP>}:$PORT/apk 下载"
else
  echo "提示：/apk 暂不可用（还没构建过 android/app/build/outputs/apk/debug/app-debug.apk）"
fi

step "完成"
echo "Go 版已接管 $PORT，并会在登录时自动启动（$GO_PLIST）。"
echo "下一步：手机上完全退出 Homing 再打开，不用改任何设置。"
echo "回滚：bash scripts/rollback-to-node.sh"
