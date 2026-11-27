#!/usr/bin/env python3
"""Build a native Windows Desktop & CLI bundle for Homing (runs natively on Windows or cross-compiled on macOS/Linux)."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import tempfile
import zipfile


INSTALL_PS1 = r"""# Homing Windows One-Click Local Setup (User-space, no Administrator UAC required)
$ErrorActionPreference = "Stop"
$SrcDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$TargetDir = Join-Path $env:LOCALAPPDATA "Programs\Homing"

Write-Host "==> Installing Homing to $TargetDir ..." -ForegroundColor Cyan
New-Item -ItemType Directory -Force -Path $TargetDir | Out-Null

Copy-Item -Force (Join-Path $SrcDir "Homing.exe") (Join-Path $TargetDir "Homing.exe")
Copy-Item -Force (Join-Path $SrcDir "homing-cli.exe") (Join-Path $TargetDir "homing.exe")
if (Test-Path (Join-Path $SrcDir "Homing.ico")) {
    Copy-Item -Force (Join-Path $SrcDir "Homing.ico") (Join-Path $TargetDir "Homing.ico")
}

# Add %LOCALAPPDATA%\Programs\Homing to User PATH if missing
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if (-not $UserPath) { $UserPath = "" }
$PathParts = $UserPath -split ";" | Where-Object { $_ -ne "" }
if ($PathParts -notcontains $TargetDir) {
    $NewPath = ($PathParts + $TargetDir) -join ";"
    [Environment]::SetEnvironmentVariable("Path", $NewPath, "User")
    Write-Host "==> Added $TargetDir to User PATH (`homing` CLI ready in new terminals)." -ForegroundColor Green
}

# Create Start Menu & Desktop Shortcuts
$WshShell = New-Object -ComObject WScript.Shell
$StartMenu = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs"
foreach ($ShortcutDir in @($StartMenu, [Environment]::GetFolderPath("Desktop"))) {
    if ($ShortcutDir -and (Test-Path $ShortcutDir)) {
        $Lnk = $WshShell.CreateShortcut((Join-Path $ShortcutDir "Homing.lnk"))
        $Lnk.TargetPath = Join-Path $TargetDir "Homing.exe"
        $Lnk.WorkingDirectory = $TargetDir
        $Lnk.Description = "Homing — Multi-Agent Away-From-Desk Command Center"
        if (Test-Path (Join-Path $TargetDir "Homing.ico")) {
            $Lnk.IconLocation = Join-Path $TargetDir "Homing.ico"
        }
        $Lnk.Save()
    }
}

Write-Host "==> Starting Homing Desktop (System Tray + Management Console) ..." -ForegroundColor Green
Start-Process -FilePath (Join-Path $TargetDir "Homing.exe")
Write-Host "Done! Look for the Homing pigeon icon in the Windows bottom-right system tray." -ForegroundColor Cyan
"""

UNINSTALL_PS1 = r"""# Uninstall Homing user-space files (keeps %APPDATA%\Homing session history unless -PurgeData is passed)
param([switch]$PurgeData)
$ErrorActionPreference = "SilentlyContinue"
Get-Process -Name "Homing","homing","homing-cli" | Stop-Process -Force
Remove-ItemProperty -Path "HKCU:\Software\Microsoft\Windows\CurrentVersion\Run" -Name "Homing"

$TargetDir = Join-Path $env:LOCALAPPDATA "Programs\Homing"
Remove-Item -Recurse -Force $TargetDir
Remove-Item -Force (Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\Homing.lnk")
Remove-Item -Force (Join-Path ([Environment]::GetFolderPath("Desktop")) "Homing.lnk")

if ($PurgeData) {
    Remove-Item -Recurse -Force (Join-Path $env:APPDATA "Homing")
}
Write-Host "Homing uninstalled." -ForegroundColor Green
"""

README_TXT = """Homing for Windows (Desktop Tray + Management Console + CLI)
=============================================================

Contents:
  - Homing.exe        Native Windows Desktop Window + Bottom-Right System Tray App (no black console window)
  - homing-cli.exe    Command-line / Headless Daemon / TUI / MCP binary (installed as `homing.exe` on PATH)
  - Install-Homing.ps1  Right-click -> "Run with PowerShell" to install to %LOCALAPPDATA%\\Programs\\Homing,
                        create Start Menu & Desktop shortcuts, and add `homing` to your User PATH.
  - Uninstall-Homing.ps1  Cleanly removes shortcuts, autostart registry entry, and binaries.

Quick Start (Portable):
  Double-click `Homing.exe` directly!
  - Left-click the bottom-right Homing tray icon to open the Quick Away-Guardian Popover.
  - Click "Open Console" (打开主窗口) or right-click the tray icon for the full management console,
    Windows Keep-Awake switch, Away Mode, and QR phone pairing.

Requirements:
  - Windows 10 (1809+) or Windows 11 (Microsoft Edge WebView2 Runtime is pre-installed on Windows 10/11).
"""


def run(args, **kwargs):
    subprocess.run(args, check=True, **kwargs)


def sha256_file(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            h.update(chunk)
    return h.hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--build-number", default="1")
    parser.add_argument("--commit", required=True, help="Full commit hash")
    parser.add_argument("--arch", default="amd64", choices=["amd64", "arm64"])
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--skip-console-build", action="store_true")
    args = parser.parse_args()

    if not re.fullmatch(r"(\d+\.\d+\.\d+)(?:[-+][A-Za-z0-9.+-]+)?", args.version) or not args.build_number.isdigit():
        parser.error("use a semantic version and a numeric build number")
    if not re.fullmatch(r"[a-f0-9]{40}", args.commit):
        parser.error("--commit must be a full Git commit hash")

    root = Path(__file__).resolve().parent.parent
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    basename = f"Homing-{args.version}-windows-{args.arch}"
    zip_target = output / f"{basename}.zip"
    if zip_target.exists():
        parser.error(f"output already contains {zip_target.name}; choose an empty output directory")

    if not args.skip_console_build:
        run(["bash", "scripts/build-console.sh"], cwd=root)

    syso_path = root / "cmd" / "homing" / f"rsrc_windows_{args.arch}.syso"
    with tempfile.TemporaryDirectory(prefix="homing-win-") as tmp:
        stage = Path(tmp) / "Homing"
        stage.mkdir(parents=True)
        ico_path = stage / "Homing.ico"
        try:
            host_env = dict(os.environ)
            host_env.pop("GOOS", None)
            host_env.pop("GOARCH", None)
            # 1. Generate Windows .syso (embedded multi-size icon + High-DPI PerMonitorV2 manifest)
            run(
                [
                    "go", "run", "./build/winres",
                    "-arch", args.arch,
                    "-out", str(syso_path),
                    "-ico", str(ico_path),
                ],
                cwd=root,
                env=host_env,
            )

            win_env = dict(os.environ)
            win_env["GOOS"] = "windows"
            win_env["GOARCH"] = args.arch
            win_env["CGO_ENABLED"] = "0"

            # 2. Build Homing.exe (Windows GUI subsystem — zero console window flash, tray + main window)
            gui_exe = stage / "Homing.exe"
            run(
                [
                    "go", "build", "-buildvcs=false", "-trimpath",
                    "-ldflags", f"-H=windowsgui -s -w -X main.version={args.version}",
                    "-o", str(gui_exe), "./cmd/homing",
                ],
                cwd=root,
                env=win_env,
            )

            # 3. Build homing-cli.exe (Console subsystem — ideal for PowerShell, `homing tui`, `homing mcp`)
            cli_exe = stage / "homing-cli.exe"
            run(
                [
                    "go", "build", "-buildvcs=false", "-trimpath",
                    "-ldflags", f"-s -w -X main.version={args.version}",
                    "-o", str(cli_exe), "./cmd/homing",
                ],
                cwd=root,
                env=win_env,
            )

            # Also copy standalone GUI exe for `homing update` / direct single-file downloaders
            standalone_gui = output / f"homing_windows_{args.arch}.exe"
            shutil.copy2(gui_exe, standalone_gui)
        finally:
            if syso_path.exists():
                syso_path.unlink()

        (stage / "Install-Homing.ps1").write_text(INSTALL_PS1, encoding="utf-8-sig")
        (stage / "Uninstall-Homing.ps1").write_text(UNINSTALL_PS1, encoding="utf-8-sig")
        (stage / "README-Windows.txt").write_text(README_TXT, encoding="utf-8")

        with zipfile.ZipFile(zip_target, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as zf:
            for item in sorted(stage.rglob("*")):
                if item.is_file():
                    zf.write(item, arcname=str(Path("Homing") / item.relative_to(stage)))

        assets = {
            zip_target.name: sha256_file(zip_target),
            standalone_gui.name: sha256_file(standalone_gui),
        }
        sums_text = "".join(f"{digest}  {name}\n" for name, digest in assets.items())
        (output / f"SHA256SUMS-windows-{args.arch}").write_text(sums_text, encoding="utf-8")

        manifest = {
            "product": "Homing",
            "version": args.version,
            "build_number": args.build_number,
            "commit": args.commit,
            "platform": "windows",
            "arch": args.arch,
            "minimum_windows": "10.0.17763",
            "dpi_awareness": "PerMonitorV2",
            "assets": assets,
        }
        (output / f"BUILD-INFO-windows-{args.arch}.json").write_text(
            json.dumps(manifest, indent=2) + "\n",
            encoding="utf-8",
        )
        print(json.dumps(manifest, indent=2))


if __name__ == "__main__":
    main()
