# ==============================================================================
# WebLimbAI Universal Windows PowerShell Installer
# Usage: irm https://raw.githubusercontent.com/anishraj836/WebLimbAI/main/scripts/install.ps1 | iex
# ==============================================================================

$ErrorActionPreference = "Stop"

$Repo = "anishraj836/WebLimbAI"
$BinaryName = "weblimb.exe"
$AltBinaryName = "lightlimbs.exe"

# 1. Architecture Detection
$Arch = $env:PROCESSOR_ARCHITECTURE.ToLower()
switch ($Arch) {
    "amd64"   { $TargetArch = "amd64" }
    "x86"     { $TargetArch = "amd64" } # Default to 64-bit binary on modern systems
    "arm64"   { $TargetArch = "arm64" }
    default   {
        Write-Error "Unsupported architecture: $Arch. WebLimbAI supports amd64 and arm64."
        exit 1
    }
}

$Os = "windows"
$ZipName = "weblimb_${Os}_${TargetArch}.zip"
$ReleaseUrl = "https://github.com/$Repo/releases/latest/download"
$DownloadUrl = "$ReleaseUrl/$ZipName"
$ChecksumUrl = "$ReleaseUrl/checksums.txt"

# 2. Setup Temporary Directory
$TempDir = Join-Path ([System.IO.Path]::GetTempPath()) ([System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $TempDir -Force | Out-Null

try {
    $ZipPath = Join-Path $TempDir $ZipName
    $ChecksumPath = Join-Path $TempDir "checksums.txt"

    Write-Host "Downloading WebLimbAI for Windows ($TargetArch)..."
    try {
        Invoke-WebRequest -Uri $DownloadUrl -OutFile $ZipPath -UseBasicParsing
    } catch {
        # Fallback to agentlimbs zipball if weblimb not yet published
        $FallbackZip = "agentlimbs_${Os}_${TargetArch}.zip"
        $FallbackUrl = "$ReleaseUrl/$FallbackZip"
        Write-Host "Trying fallback release asset: $FallbackZip..."
        Invoke-WebRequest -Uri $FallbackUrl -OutFile $ZipPath -UseBasicParsing
    }

    # 3. Checksum Verification (Best Effort)
    try {
        Invoke-WebRequest -Uri $ChecksumUrl -OutFile $ChecksumPath -UseBasicParsing
        if (Test-Path $ChecksumPath) {
            $ChecksumLines = Get-Content $ChecksumPath
            $ExpectedHash = ""
            foreach ($line in $ChecksumLines) {
                if ($line -match $ZipName -or $line -match $FallbackZip) {
                    $ExpectedHash = ($line -split '\s+')[0].Trim()
                    break
                }
            }

            if ($ExpectedHash) {
                Write-Host "Verifying SHA-256 checksum integrity..."
                $ActualHash = (Get-FileHash -Path $ZipPath -Algorithm SHA256).Hash.ToLower()
                if ($ActualHash -ne $ExpectedHash.ToLower()) {
                    Write-Error "Checksum verification failed! Expected: $ExpectedHash, Actual: $ActualHash"
                    exit 1
                }
                Write-Host "Checksum verified: $ActualHash"
            }
        }
    } catch {
        # Checksum download is best-effort
    }

    # 4. Extract Archive
    Write-Host "Extracting archive..."
    Expand-Archive -Path $ZipPath -DestinationPath $TempDir -Force

    $ExtractedBin = Join-Path $TempDir "weblimb.exe"
    if (-not (Test-Path $ExtractedBin)) {
        $Candidates = @("lightlimbs.exe", "agentlimbs.exe", "agentlimbs-light.exe", "weblimb", "lightlimbs")
        foreach ($c in $Candidates) {
            $candPath = Join-Path $TempDir $c
            if (Test-Path $candPath) {
                $ExtractedBin = $candPath
                break
            }
        }
    }

    if (-not (Test-Path $ExtractedBin)) {
        Write-Error "Failed to locate binary in extracted archive."
        exit 1
    }

    # 5. Target Installation Directory
    $InstallDir = Join-Path $env:USERPROFILE ".local\bin"
    if (-not (Test-Path $InstallDir)) {
        New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    }

    $TargetBinPath = Join-Path $InstallDir $BinaryName
    $TargetAltPath = Join-Path $InstallDir $AltBinaryName

    Copy-Item -Path $ExtractedBin -Destination $TargetBinPath -Force
    Copy-Item -Path $ExtractedBin -Destination $TargetAltPath -Force

    Write-Host ""
    Write-Host "WebLimbAI installed successfully to: $TargetBinPath"

    # 6. PATH Verification & Guidance
    $UserPath = [System.Environment]::GetEnvironmentVariable("Path", [System.EnvironmentVariableTarget]::User)
    $PathHasDir = ($env:PATH -split ';') -contains $InstallDir -or ($UserPath -split ';') -contains $InstallDir

    if (-not $PathHasDir) {
        Write-Host ""
        Write-Host "NOTE: '$InstallDir' is not currently in your user PATH."
        Write-Host "To add it automatically, run the following PowerShell command:"
        Write-Host "  [System.Environment]::SetEnvironmentVariable('Path', `$UserPath + ';$InstallDir', [System.EnvironmentVariableTarget]::User)"
        Write-Host ""
    }

    Write-Host "Next Steps:"
    Write-Host "   1. Auto-configure AI IDEs (Claude Desktop & Cursor):"
    Write-Host "      $TargetBinPath init-mcp"
    Write-Host ""
    Write-Host "   2. Scrape clean Markdown directly in PowerShell:"
    Write-Host "      $TargetBinPath scrape https://go.dev -j"
    Write-Host ""
    Write-Host "   3. Search documentation locally:"
    Write-Host "      $TargetBinPath search `"goroutine scheduler`""
    Write-Host ""
} finally {
    if (Test-Path $TempDir) {
        Remove-Item -Path $TempDir -Recurse -Force -ErrorAction SilentlyContinue
    }
}
