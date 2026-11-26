#!/usr/bin/env python3
"""Rasterize official @lobehub/icons-static-svg agent logos into Android drawables.

Run from the repository root:
    python3 scripts/render-agent-logos.py

Requires Google Chrome (headless screenshot) and console/node_modules installed.
Monochrome SVGs use `currentColor`; they are rendered with a light tint suited to
Homing's dark theme. Output: android/app/src/main/res/drawable-nodpi/agent_logo_*.png
"""
import base64
import pathlib
import subprocess
import sys
import tempfile

ROOT = pathlib.Path(__file__).resolve().parent.parent
ICONS = ROOT / "console/node_modules/@lobehub/icons-static-svg/icons"
OUT = ROOT / "android/app/src/main/res/drawable-nodpi"
CHROME = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
SIZE = 192
MONO_TINT = "#E5E7EB"

# Homing agent id -> lobehub icon file (without .svg)
MAPPING = {
    "codex": "codex-color",
    "claude": "claude-color",
    "workbuddy": "workbuddy-color",
    "kimi": "kimi-color",
    "dsh": "deepseek-color",
    "opencode": "opencode",
    "zcode": "zhipu-color",
    "gemini_cli": "gemini-color",
    "codebuddy": "codebuddy-color",
    "cursor": "cursor",
    "copilot_cli": "githubcopilot",
    "qwen_code": "qwen-color",
    "cline": "cline",
    "windsurf": "windsurf",
    "kiro": "kiro-color",
    "openclaw": "openclaw-color",
    "goose": "goose",
    "roo_code": "roocode",
    "amp": "amp-color",
    "antigravity": "antigravity-color",
    "trae": "trae-color",
    "openhands": "openhands-color",
    "kilo": "kilocode",
    "junie": "junie-color",
    "minimax_design": "minimax-color",
}


def main() -> int:
    if not ICONS.is_dir():
        print(f"missing {ICONS}; run `npm ci` in console/ first", file=sys.stderr)
        return 1
    OUT.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix="homing-logos-") as tmp:
        tmpdir = pathlib.Path(tmp)
        for agent_id, icon in MAPPING.items():
            svg_path = ICONS / f"{icon}.svg"
            if not svg_path.exists():
                print(f"skip {agent_id}: {icon}.svg not found")
                continue
            svg = svg_path.read_text(encoding="utf-8")
            svg = svg.replace('height="1em"', f'height="{SIZE}"').replace('width="1em"', f'width="{SIZE}"')
            html = (
                "<!doctype html><html><head><style>"
                "html,body{margin:0;padding:0;background:transparent;overflow:hidden}"
                f"div{{width:{SIZE}px;height:{SIZE}px;color:{MONO_TINT};display:flex}}"
                f"svg{{width:{SIZE}px;height:{SIZE}px;display:block}}"
                "</style></head><body><div>" + svg + "</div></body></html>"
            )
            page = tmpdir / f"{agent_id}.html"
            page.write_text(html, encoding="utf-8")
            target = OUT / f"agent_logo_{agent_id}.png"
            subprocess.run(
                [
                    CHROME,
                    "--headless=new",
                    "--disable-gpu",
                    "--hide-scrollbars",
                    "--force-device-scale-factor=1",
                    "--default-background-color=00000000",
                    f"--window-size={SIZE},{SIZE}",
                    f"--screenshot={target}",
                    page.as_uri(),
                ],
                check=True,
                stdout=subprocess.DEVNULL,
                stderr=subprocess.DEVNULL,
                timeout=60,
            )
            print(f"rendered {target.relative_to(ROOT)} ({target.stat().st_size} bytes)")
    return 0


if __name__ == "__main__":
    sys.exit(main())
