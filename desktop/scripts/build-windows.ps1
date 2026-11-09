# Homing · 在 Windows 上打安装包
#
# 为什么要有这个脚本：Tauri 的 Windows 安装程序（NSIS）必须在 Windows 上构建，
# macOS 上没法交叉编译出可靠的 .exe 安装器。所以这一步放到你的 Windows 机器上跑。
#
# 前置（一次性）：
#   1. Node.js 22+            https://nodejs.org/
#   2. Rust（MSVC 工具链）     https://rustup.rs/  安装时选 "Desktop development with C++"
#   3. WebView2 运行时         Win11 自带；Win10 一般也有
#
# 用法（在 desktop 目录下，PowerShell）：
#   powershell -ExecutionPolicy Bypass -File scripts/build-windows.ps1
#
# 产物：
#   desktop/src-tauri/target/release/bundle/nsis/Homing_<版本>_x64-setup.exe
#   脚本最后会把它复制到仓库根目录的 dist/

$ErrorActionPreference = "Stop"
$desktop = Split-Path -Parent $PSScriptRoot
$repo = Split-Path -Parent $desktop

Write-Host "==> 1/4 检查工具链" -ForegroundColor Cyan
node --version
npm --version
rustc --version
if ($LASTEXITCODE -ne 0) { throw "缺少 rustc，请先装 Rust（MSVC 工具链）" }

Write-Host "==> 2/4 安装前端依赖" -ForegroundColor Cyan
Push-Location $desktop
npm install --no-audit --no-fund

Write-Host "==> 3/4 构建控制台页面 + homingd.exe（Node SEA 单文件）" -ForegroundColor Cyan
Push-Location (Join-Path $repo "console")
npm install --no-audit --no-fund
npm run build
Pop-Location

node scripts/build-sidecar.mjs --skip-console
if (-not (Test-Path "src-tauri/binaries/homingd.exe")) {
  throw "homingd.exe 没生成，检查上面的报错"
}

Write-Host "==> 4/4 构建 Tauri 安装包（NSIS）" -ForegroundColor Cyan
npx tauri build --target x86_64-pc-windows-msvc

$setup = Get-ChildItem -Path "src-tauri/target/x86_64-pc-windows-msvc/release/bundle/nsis" -Filter "*.exe" |
  Sort-Object LastWriteTime -Descending | Select-Object -First 1
if (-not $setup) { throw "没找到安装包，看看 src-tauri/target 下的报错" }

$dist = Join-Path $repo "dist"
New-Item -ItemType Directory -Force -Path $dist | Out-Null
Copy-Item $setup.FullName (Join-Path $dist $setup.Name) -Force
Pop-Location

Write-Host ""
Write-Host "完成：$($setup.FullName)" -ForegroundColor Green
Write-Host "已复制到：$(Join-Path $dist $setup.Name)" -ForegroundColor Green
Write-Host ""
Write-Host "装完第一次启动会发生什么：" -ForegroundColor Cyan
Write-Host "  - 托盘出现灰鸽图标，菜单里能看到连接状态"
Write-Host "  - 控制台窗口打开 http://127.0.0.1:18787/console"
Write-Host "  - 如果 18787 上已经有 homingd 在跑（比如你自己开的），App 会直接复用，不会重复拉一个"
