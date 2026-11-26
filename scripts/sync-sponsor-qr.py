#!/usr/bin/env python3
"""
Sync WeChat Reward & Alipay QR images across docs, Desktop Console, and Android assets.

Usage:
  # Generate clean initial placeholder cards (before maintainer adds personal QR images):
  python3 scripts/sync-sponsor-qr.py --init-placeholders

  # Replace all 3 surfaces in one command with real QR images:
  python3 scripts/sync-sponsor-qr.py --wechat ~/Downloads/wechat.png --alipay ~/Downloads/alipay.png
"""

import argparse
import os
import shutil
import subprocess
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

TARGETS = {
    "wechat": [
        ROOT / "docs" / "assets" / "sponsor" / "wechat-reward.png",
        ROOT / "console" / "public" / "sponsor" / "wechat-reward.png",
        ROOT / "web" / "console" / "sponsor" / "wechat-reward.png",
        ROOT / "android" / "app" / "src" / "main" / "assets" / "sponsor" / "wechat_reward.png",
    ],
    "alipay": [
        ROOT / "docs" / "assets" / "sponsor" / "alipay-reward.png",
        ROOT / "console" / "public" / "sponsor" / "alipay-reward.png",
        ROOT / "web" / "console" / "sponsor" / "alipay-reward.png",
        ROOT / "android" / "app" / "src" / "main" / "assets" / "sponsor" / "alipay_reward.png",
    ],
}

CHROME_BIN = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"


def render_placeholder_png(kind: str, out_path: Path) -> None:
    is_wechat = kind == "wechat"
    accent = "#07C160" if is_wechat else "#1677FF"
    title = "微信赞赏码" if is_wechat else "支付宝收款码"
    sub = "开源发布前替换此图即可生效"
    cmd_hint = f"scripts/sync-sponsor-qr.py --{kind} <图片>"

    svg = f"""<svg xmlns="http://www.w3.org/2000/svg" width="520" height="600" viewBox="0 0 520 600">
  <rect width="520" height="600" rx="36" fill="#0F172A"/>
  <rect x="16" y="16" width="488" height="568" rx="28" fill="#131C31" stroke="{accent}" stroke-width="3" stroke-opacity="0.55"/>
  <rect x="150" y="44" width="220" height="42" rx="21" fill="{accent}" fill-opacity="0.16"/>
  <text x="260" y="72" text-anchor="middle" font-family="-apple-system, PingFang SC, sans-serif" font-size="22" font-weight="700" fill="{accent}">{title}</text>
  <rect x="90" y="114" width="340" height="340" rx="24" fill="#F8FAFC"/>
  <rect x="118" y="142" width="76" height="76" rx="12" fill="none" stroke="{accent}" stroke-width="10"/>
  <rect x="140" y="164" width="32" height="32" rx="5" fill="{accent}"/>
  <rect x="326" y="142" width="76" height="76" rx="12" fill="none" stroke="{accent}" stroke-width="10"/>
  <rect x="348" y="164" width="32" height="32" rx="5" fill="{accent}"/>
  <rect x="118" y="350" width="76" height="76" rx="12" fill="none" stroke="{accent}" stroke-width="10"/>
  <rect x="140" y="372" width="32" height="32" rx="5" fill="{accent}"/>
  <circle cx="260" cy="284" r="44" fill="{accent}" fill-opacity="0.14"/>
  <text x="260" y="292" text-anchor="middle" font-family="-apple-system, PingFang SC, sans-serif" font-size="24" font-weight="700" fill="#0F172A">Homing ☕</text>
  <text x="260" y="502" text-anchor="middle" font-family="-apple-system, PingFang SC, sans-serif" font-size="20" font-weight="600" fill="#E2E8F0">{sub}</text>
  <text x="260" y="538" text-anchor="middle" font-family="ui-monospace, SFMono-Regular, Menlo, monospace" font-size="14" fill="#94A3B8">{cmd_hint}</text>
</svg>"""

    out_path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory() as tmp:
        html_path = Path(tmp) / "card.html"
        html_path.write_text(
            f"<!doctype html><html><body style='margin:0;background:transparent'>{svg}</body></html>",
            encoding="utf-8",
        )
        subprocess.run(
            [
                CHROME_BIN,
                "--headless=new",
                "--disable-gpu",
                "--hide-scrollbars",
                "--default-background-color=00000000",
                "--window-size=520,600",
                f"--screenshot={out_path}",
                f"file://{html_path}",
            ],
            check=True,
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )


def copy_to_targets(kind: str, src: Path) -> None:
    if not src.is_file():
        raise SystemExit(f"Source file not found: {src}")
    with tempfile.TemporaryDirectory() as tmp:
        normalized = src
        raw_head = src.read_bytes()[:8]
        if raw_head != b"\x89PNG\r\n\x1a\n":
            converted = Path(tmp) / f"{kind}.png"
            if shutil.which("sips"):
                subprocess.run(
                    ["sips", "-s", "format", "png", str(src), "--out", str(converted)],
                    check=True,
                    stdout=subprocess.DEVNULL,
                    stderr=subprocess.DEVNULL,
                )
                normalized = converted
        for dst in TARGETS[kind]:
            dst.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(normalized, dst)
            print(f"Updated {dst.relative_to(ROOT)}")


def main() -> None:
    parser = argparse.ArgumentParser(description="Sync sponsor QR assets across Homing")
    parser.add_argument("--wechat", type=Path, help="Path to WeChat reward QR PNG/JPG")
    parser.add_argument("--alipay", type=Path, help="Path to Alipay QR PNG/JPG")
    parser.add_argument("--init-placeholders", action="store_true", help="Generate clean placeholder QR cards")
    args = parser.parse_args()

    if args.init_placeholders:
        with tempfile.TemporaryDirectory() as tmp:
            for kind in ("wechat", "alipay"):
                rendered = Path(tmp) / f"{kind}.png"
                render_placeholder_png(kind, rendered)
                copy_to_targets(kind, rendered)
        return

    if not args.wechat and not args.alipay:
        parser.error("Provide --wechat and/or --alipay, or --init-placeholders")

    if args.wechat:
        copy_to_targets("wechat", args.wechat)
    if args.alipay:
        copy_to_targets("alipay", args.alipay)


if __name__ == "__main__":
    main()
