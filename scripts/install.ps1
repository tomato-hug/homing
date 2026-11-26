# Homing installer for Windows 10 / 11 (PowerShell)
#
#   irm https://raw.githubusercontent.com/tomato-hug/homing/main/scripts/install.ps1 | iex
#
# Optional environment variables:
#   $env:HOMING_INSTALL_DIR = "C:\Path\To\Dir"
#   $env:HOMING_VERSION     = "v0.11.6"

$ErrorActionPreference = "Stop"
$Repo = "tomato-hug/homing"
$InstallDir = if ($env:HOMING_INSTALL_DIR) { $env:HOMING_INSTALL_DIR } else { Join-Path $env:LOCALAPPDATA "Programs\Homing" }
$RequestedVersion = $env:HOMING_VERSION

$ArchRaw = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
$Arch = switch ($ArchRaw) {
    "arm64" { "arm64" }
    "x64"   { "amd64" }
    default { throw "Unsupported Windows CPU architecture: $ArchRaw" }
}

if ($RequestedVersion) {
    $Ver = $RequestedVersion.TrimStart("v")
    $Base = "https://github.com/$Repo/releases/download/v$Ver"
    $ZipAsset = "Homing-$Ver-windows-$Arch.zip"
} else {
    $Rel = Invoke-RestMethod -Uri "https://api.github.com/repos/$Repo/releases/latest" -Headers @{ "User-Agent" = "homing-installer" }
    $Ver = $Rel.tag_name.TrimStart("v")
    $Base = "https://github.com/$Repo/releases/download/$($Rel.tag_name)"
    $ZipAsset = "Homing-$Ver-windows-$Arch.zip"
}

$SingleExeAsset = "homing_windows_$Arch.exe"
$TmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("homing-install-" + [guid]::NewGuid().ToString("N"))
New-Item -ItemType Directory -Force -Path $TmpDir | Out-Null

try {
    $ChecksumsFile = Join-Path $TmpDir "checksums.txt"
    Invoke-WebRequest -UseBasicParsing -Uri "$Base/checksums.txt" -OutFile $ChecksumsFile
    $ChecksumsText = Get-Content $ChecksumsFile

    $UseZip = $ChecksumsText | Where-Object { $_ -match "\s+\*?$([regex]::Escape($ZipAsset))$" }
    $AssetName = if ($UseZip) { $ZipAsset } else { $SingleExeAsset }
    $AssetPath = Join-Path $TmpDir $AssetName

    Write-Host "Downloading $AssetName ..." -ForegroundColor Cyan
    Invoke-WebRequest -UseBasicParsing -Uri "$Base/$AssetName" -OutFile $AssetPath

    $MatchLine = $ChecksumsText | Where-Object { $_ -match "^([0-9a-fA-F]{64})\s+\*?$([regex]::Escape($AssetName))$" } | Select-Object -First 1
    if (-not $MatchLine) {
        throw "checksums.txt has no SHA-256 entry for $AssetName; aborting install."
    }
    $ExpectedHash = ($MatchLine -split "\s+")[0].ToLowerInvariant()
    $ActualHash = (Get-FileHash -Algorithm SHA256 -Path $AssetPath).Hash.ToLowerInvariant()
    if ($ExpectedHash -ne $ActualHash) {
        throw "SHA-256 mismatch for $AssetName (expected $ExpectedHash, got $ActualHash)."
    }

    if ($UseZip) {
        $ExtractDir = Join-Path $TmpDir "extracted"
        Expand-Archive -Path $AssetPath -DestinationPath $ExtractDir -Force
        $InstallerScript = Join-Path $ExtractDir "Homing\Install-Homing.ps1"
        & $InstallerScript
    } else {
        New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
        Copy-Item -Force $AssetPath (Join-Path $InstallDir "Homing.exe")
        Copy-Item -Force $AssetPath (Join-Path $InstallDir "homing.exe")
        $UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
        if (($UserPath -split ";") -notcontains $InstallDir) {
            [Environment]::SetEnvironmentVariable("Path", "$UserPath;$InstallDir".Trim(";"), "User")
        }
        Start-Process -FilePath (Join-Path $InstallDir "Homing.exe")
        Write-Host "Homing installed to $InstallDir and started in the system tray!" -ForegroundColor Green
    }
} finally {
    Remove-Item -Recurse -Force $TmpDir -ErrorAction SilentlyContinue
}
