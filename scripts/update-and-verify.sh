#!/usr/bin/env bash
# Homing · 验证并更新（macOS）
#
# 用法：在仓库根目录运行   bash scripts/update-and-verify.sh
#
# 依次：Go vet + test → 安卓单元测试 + 构建 APK → 编译新的 homing 并替换
# ~/.homing/bin/homing → 重启 dev.homing.homingd → 检查 /health 与 /api/rooms →
# 全部通过后 git 提交。任何一步失败就停下；新服务起不来时自动换回旧程序。
# 日志：.verify/update.log
set -u
cd "$(dirname "$0")/.." || exit 2
mkdir -p .verify
exec > >(tee "$PWD/.verify/update.log") 2>&1

UID_N="$(id -u)"
LABEL="dev.homing.homingd"
BIN="$HOME/.homing/bin/homing"
PORT=18787
DATA="$HOME/Library/Application Support/Homing"

step() { echo; echo "== $* =="; }
die() { echo; echo "失败：$*"; echo "（服务未改动，或已恢复旧版本）"; exit 1; }
health() { curl -fsS -m 2 "http://127.0.0.1:${PORT}/health" >/dev/null 2>&1; }

step "控制台构建"
if command -v npm >/dev/null 2>&1; then
  ( cd console && { [ -d node_modules ] || npm ci --no-audit --no-fund; } ) || die "控制台依赖安装失败"
  bash scripts/build-console.sh || die "控制台构建没通过（tsc / vite）"
else
  echo "警告：这台电脑没有 npm，跳过控制台构建；网页仍是上一次构建的版本"
fi

step "Go 检查与测试"
go vet ./... || die "go vet 没通过"
go test ./... > .verify/go-test.log 2>&1
rc=$?
grep -v "no test files" .verify/go-test.log
[ "$rc" = 0 ] || die "go test 没通过（输出见上方）"

step "安卓单元测试与构建"
( cd android && ./gradlew :app:testDebugUnitTest :app:assembleDebug --console=plain -q ) \
  || die "安卓测试或构建没通过"
APK="android/app/build/outputs/apk/debug/app-debug.apk"
ls -la "$APK"

step "编译新的 homing"
VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
go build -trimpath -ldflags "-X main.version=${VERSION}" -o "${BIN}.new" ./cmd/homing 2>&1 | grep -v "ld: warning" || true
[ -x "${BIN}.new" ] || die "go build 失败"
"${BIN}.new" version >/dev/null || die "新程序无法运行"

step "替换并重启服务"
cp "$BIN" "${BIN}.prev"
mv "${BIN}.new" "$BIN"
launchctl kickstart -k "gui/${UID_N}/${LABEL}" || die "launchctl kickstart 失败"
ok=0
for _ in $(seq 1 30); do health && { ok=1; break; }; sleep 1; done
if [ "$ok" != 1 ]; then
  echo "新服务 30 秒内没有响应，homingd.log 末尾："
  tail -30 "$DATA/homingd.log"
  mv "${BIN}.prev" "$BIN"
  launchctl kickstart -k "gui/${UID_N}/${LABEL}"
  die "已换回旧程序"
fi
echo "服务正常：$("$BIN" version)"
TOKEN="$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1]))["token"])' "$DATA/config.json" 2>/dev/null)"
if [ -n "$TOKEN" ]; then
  code="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${TOKEN}" "http://127.0.0.1:${PORT}/api/rooms")"
  echo "GET /api/rooms -> ${code}"
  [ "$code" = "200" ] || echo "警告：会议室接口没有返回 200"
  code="$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer ${TOKEN}" "http://127.0.0.1:${PORT}/api/flow")"
  echo "GET /api/flow -> ${code}"
  code="$(curl -s -o /dev/null -w '%{http_code}' "http://127.0.0.1:${PORT}/get")"
  echo "GET /get -> ${code}"
  curl -s -H "Authorization: Bearer ${TOKEN}" "http://127.0.0.1:${PORT}/api/agent-catalog" \
    | python3 -c 'import json,sys
d=json.load(sys.stdin)
for a in d.get("agents",[]):
    if a.get("connected") or a.get("detected"):
        print("  %-12s 已接入=%-5s 已检测=%-5s 钩子=%s" % (a["id"], a.get("connected"), a.get("detected"), a.get("hook_installed")))' \
    || true
fi

step "提交"
git add -A
if git diff --cached --quiet; then
  echo "没有需要提交的改动"
else
  git commit -q -F - <<'MSG'
feat: 控制台首页改为 Agent（检测/一键安装/接入/手机配对）、实时流可视化、灰鸽 v2 动画

- 控制台首页 = Agent：本机已装的一键接入；没装的在确认后由本机执行官方安装命令
  （POST /api/agent-catalog/{id}/setup，只允许本机控制台调用），桌面应用给下载页
- 手机配对改走端到端加密的 /api/cloud/pair/*：二维码指向 /get 落地页，相机扫了能
  下载 APK 并跳回 App 配对；电脑上核对四个校验词（以前控制台的二维码 App 不认）
- 新增「实时流」页（参考 Magpie 路由视图）：/api/flow 与 /api/flow/stream 把 hook
  事件、回复、@ 协作、结果回传、会议发言、推送画成手机 ↔ Homing ↔ Agent 之间的数据包
- 灰鸽 v2 八个场景落地：控制台侧栏（点按轻点、离线打盹、等你处理、收信）、首页、
  实时流；安卓启动页收信、顶栏状态鸽、配对成功、我的页轻点；托盘图标点击播放 tap 帧
- 安卓首页：连接状态并入概览卡，三个 64dp 数字块；顶栏按钮 48dp、图标 24dp；
  支持 homing://pair 深链与 /get 链接扫码；版本 0.11.0

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018qwdgjQ8KAXxf979TQMh5a
MSG
  git log --oneline -1
fi

LAN_IP="$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || echo '<电脑IP>')"
step "完成"
echo "手机安装新版：http://${LAN_IP}:${PORT}/apk"
