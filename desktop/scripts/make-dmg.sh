#!/bin/bash
# 自己造 dmg：Tauri 2 的 bundle_dmg.sh 在 macOS 26 上会因为 grep 正则报错（bad regex '/dev/disk5'），
# 所以 App 由 tauri build --no-bundle 产出，dmg 由这个脚本用 hdiutil 直接做。
#
# 用法：scripts/make-dmg.sh <Homing.app 路径> <输出 dmg 路径>
set -euo pipefail

APP_PATH="${1:?需要 .app 路径}"
OUT_PATH="${2:?需要输出 dmg 路径}"
VOLUME_NAME="Homing"

if [ ! -d "$APP_PATH" ]; then
  echo "找不到 $APP_PATH"
  exit 1
fi

STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

cp -R "$APP_PATH" "$STAGE/Homing.app"
ln -s /Applications "$STAGE/Applications"

mkdir -p "$(dirname "$OUT_PATH")"
rm -f "$OUT_PATH"

hdiutil create \
  -volname "$VOLUME_NAME" \
  -srcfolder "$STAGE" \
  -ov -format UDZO \
  "$OUT_PATH" >/dev/null

echo "已生成：$OUT_PATH （$(du -h "$OUT_PATH" | cut -f1)）"
