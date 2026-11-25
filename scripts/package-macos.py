#!/usr/bin/env python3
"""Build a native Homing.app and an integrity/provenance manifest on macOS."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import plistlib
import re
import shutil
import subprocess
import tempfile


def run(args, **kwargs):
    subprocess.run(args, check=True, **kwargs)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--build-number", default="1")
    parser.add_argument("--commit", required=True, help="Full commit of the clean source snapshot")
    parser.add_argument("--output", required=True, type=Path)
    parser.add_argument("--identity", default="-", help="codesign identity; '-' is ad hoc for CI")
    args = parser.parse_args()
    if platform.system() != "Darwin":
        parser.error("native macOS packaging must run on macOS")
    match = re.fullmatch(r"(\d+\.\d+\.\d+)(?:[-+][A-Za-z0-9.+-]+)?", args.version)
    if not match or not args.build_number.isdigit():
        parser.error("use a semantic version and a numeric build number")
    if not re.fullmatch(r"[a-f0-9]{40}", args.commit):
        parser.error("--commit must be a full Git commit hash")
    root = Path(__file__).resolve().parent.parent
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    arch = {"arm64": "arm64", "x86_64": "amd64"}[platform.machine()]
    basename = f"Homing-{args.version}-macos-{arch}"
    app_target = output / "Homing.app"
    zip_target = output / (basename + ".app.zip")
    if app_target.exists() or zip_target.exists():
        parser.error("output already contains this app; choose an empty output directory")
    env = dict(os.environ)
    env["CGO_ENABLED"] = "1"
    for key in ("CGO_CFLAGS", "CGO_LDFLAGS"):
        env[key] = (env.get(key, "") + " -mmacosx-version-min=13.0").strip()
    run(["bash", "scripts/build-console.sh"], cwd=root, env=env)
    with tempfile.TemporaryDirectory(prefix="homing-macos-") as tmp:
        app = Path(tmp) / "Homing.app"
        contents = app / "Contents"
        binary = contents / "MacOS" / "homing"
        binary.parent.mkdir(parents=True)
        (contents / "Resources").mkdir()
        run(["go", "build", "-buildvcs=false", "-trimpath", "-ldflags",
             f"-s -w -X main.version={args.version} -extldflags=-mmacosx-version-min=13.0",
             "-o", str(binary), "./cmd/homing"], cwd=root, env=env)
        info = {
            "CFBundleExecutable": "homing", "CFBundleIdentifier": "dev.homing.desktop",
            "CFBundleName": "Homing", "CFBundleDisplayName": "Homing",
            "CFBundlePackageType": "APPL", "CFBundleIconFile": "Homing.icns",
            "CFBundleShortVersionString": match.group(1), "CFBundleVersion": args.build_number,
            "LSMinimumSystemVersion": "13.0", "LSUIElement": True,
            "NSHighResolutionCapable": True,
            "NSLocalNetworkUsageDescription": "Homing connects your paired devices to your local Agent service.",
            "NSAppTransportSecurity": {"NSAllowsLocalNetworking": True},
        }
        (contents / "Info.plist").write_bytes(plistlib.dumps(info))
        shutil.copy2(root / "desktop/src-tauri/icons/icon.icns", contents / "Resources/Homing.icns")
        signing = ["codesign", "--force", "--sign", args.identity]
        if args.identity != "-":
            signing += ["--options", "runtime", "--timestamp"]
        run(signing + [str(app)])
        run(["codesign", "--verify", "--deep", "--strict", str(app)])
        run(["ditto", str(app), str(app_target)])
    run(["ditto", "-c", "-k", "--sequesterRsrc", "--keepParent", str(app_target), str(zip_target)])
    digest = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
    binary = app_target / "Contents/MacOS/homing"
    manifest = {
        "product": "Homing", "version": args.version, "build_number": args.build_number,
        "commit": args.commit, "platform": "darwin", "arch": arch,
        "minimum_macos": "13.0", "binary_sha256": digest(binary),
        "signing": "ad-hoc" if args.identity == "-" else "Developer ID (verify certificate identity)",
        "notarized": False, "assets": {zip_target.name: digest(zip_target)},
    }
    (output / "BUILD-INFO.json").write_text(json.dumps(manifest, indent=2) + "\n")
    (output / "SHA256SUMS").write_text(f"{digest(zip_target)}  {zip_target.name}\n")
    print(json.dumps(manifest, indent=2))


if __name__ == "__main__":
    main()
