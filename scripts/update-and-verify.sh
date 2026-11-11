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
feat: 会议室 R1、启动页鸽子过渡、顶栏单行、Agent 接入判断与新会话状态文案

- 会议室：/api/rooms 系列接口（轮流发言、只读讨论、插话、暂停/继续/加一轮/结束、
  会议纪要），每位参会者复用只读协作派发；安卓新增「会议室」分页与房间页
- 启动页叠在主界面上方，结束时鸽子飞到收件箱顶栏再淡出
- 收件箱顶栏 Homing 固定单行，连接胶囊过长时省略
- 内置 Agent 本机已安装且未手动断开即算已接入；Codex 的 config.toml notify 也算钩子
- 新会话的「在线」改为「可后台启动」等如实描述

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_018qwdgjQ8KAXxf979TQMh5a
MSG
  git log --oneline -1
fi

LAN_IP="$(ipconfig getifaddr en0 2>/dev/null || ipconfig getifaddr en1 2>/dev/null || echo '<电脑IP>')"
step "完成"
echo "手机安装新版：http://${LAN_IP}:${PORT}/apk"
