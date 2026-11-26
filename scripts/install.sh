#!/usr/bin/env bash
# Homing installer for macOS and Linux (including WSL).
#
#   curl -fsSL https://raw.githubusercontent.com/tomato-hug/homing/main/scripts/install.sh | sh
#
# Optional environment variables:
#   HOMING_INSTALL_DIR=/path/to/bin
#   HOMING_VERSION=v0.8.0
set -eu

repo="tomato-hug/homing"
install_dir="${HOMING_INSTALL_DIR:-}"
requested_version="${HOMING_VERSION:-}"

usage() {
  cat <<'EOF'
用法: install.sh [--version vX.Y.Z] [--dir /安装目录]

不指定 --dir 时优先使用可写的 /usr/local/bin，否则使用 ~/.local/bin。
安装包会从 GitHub Releases 下载，并用同一 Release 的 checksums.txt 校验 SHA-256。
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || { echo "--version 需要参数" >&2; exit 2; }
      requested_version="$2"
      shift 2
      ;;
    --dir)
      [ "$#" -ge 2 ] || { echo "--dir 需要参数" >&2; exit 2; }
      install_dir="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "不认识的参数：$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *)
    echo "这个安装脚本适用于 macOS 和 Linux；Windows 请在 PowerShell 中运行：" >&2
    echo "  irm https://raw.githubusercontent.com/tomato-hug/homing/main/scripts/install.ps1 | iex" >&2
    exit 1
    ;;
esac

case "$(uname -m)" in
  arm64|aarch64) arch=arm64 ;;
  x86_64|amd64) arch=amd64 ;;
  *)
    echo "不支持的 CPU 架构：$(uname -m)" >&2
    exit 1
    ;;
esac

asset="homing_${os}_${arch}"
if [ -n "$requested_version" ]; then
  version="${requested_version#v}"
  base="https://github.com/${repo}/releases/download/v${version}"
else
  base="https://github.com/${repo}/releases/latest/download"
fi

if [ -z "$install_dir" ]; then
  if [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
    install_dir=/usr/local/bin
  else
    install_dir="${HOME}/.local/bin"
  fi
fi

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

echo "下载 ${asset}…"
curl -fsSL --retry 3 --retry-delay 1 "$base/$asset" -o "$tmp/$asset"
curl -fsSL --retry 3 --retry-delay 1 "$base/checksums.txt" -o "$tmp/checksums.txt"

expected="$(awk -v name="$asset" '$2 == name || $2 == "*" name {print $1; exit}' "$tmp/checksums.txt")"
if [ -z "$expected" ]; then
  echo "checksums.txt 中没有 ${asset}，拒绝安装。" >&2
  exit 1
fi

if command -v sha256sum >/dev/null 2>&1; then
  actual="$(sha256sum "$tmp/$asset" | awk '{print $1}')"
else
  actual="$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')"
fi

if [ "$expected" != "$actual" ]; then
  echo "SHA-256 校验失败，下载文件已丢弃。" >&2
  exit 1
fi

mkdir -p "$install_dir"
target="$install_dir/homing"
cp "$tmp/$asset" "$tmp/homing.install"
chmod 0755 "$tmp/homing.install"
mv "$tmp/homing.install" "$target"

echo "Homing 已安装到 $target"
"$target" version || true
case ":${PATH}:" in
  *":${install_dir}:"*) ;;
  *)
    echo
    echo "${install_dir} 不在 PATH 中。把下面一行加入 shell 配置："
    echo "  export PATH=\"${install_dir}:\$PATH\""
    ;;
esac
