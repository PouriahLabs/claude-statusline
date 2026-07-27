# claude-statusline installer (Windows)
#
#   irm https://raw.githubusercontent.com/PouriahLabs/claude-statusline/main/install.ps1 | iex
#
# Downloads the latest release, installs to %LOCALAPPDATA%\Programs, then runs
# the interactive wizard. Nothing on your system is changed without a prompt.

$ErrorActionPreference = 'Stop'

# Windows PowerShell 5.1 renders a progress bar per chunk in Invoke-WebRequest,
# which dominates the runtime of the download -- a few seconds becomes a minute.
# PowerShell 7 doesn't need this, but setting it is harmless there.
$ProgressPreference = 'SilentlyContinue'

# 5.1 on older Windows builds still negotiates TLS 1.0, which github.com refuses.
[Net.ServicePointManager]::SecurityProtocol =
    [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$Repo   = 'PouriahLabs/claude-statusline'
$Bin    = 'claude-statusline'
$Prefix = Join-Path $env:LOCALAPPDATA "Programs\$Bin"

$arch = if ([Environment]::Is64BitOperatingSystem) {
    if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'arm64' } else { 'amd64' }
} else { throw '32-bit Windows is not supported' }

Write-Host 'Finding the latest release...'
$rel = Invoke-RestMethod "https://api.github.com/repos/$Repo/releases/latest"
$tag = $rel.tag_name
$version = $tag.TrimStart('v')
$asset = "${Bin}_${version}_windows_${arch}.zip"
$url = "https://github.com/$Repo/releases/download/$tag/$asset"

$tmp = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid())
New-Item -ItemType Directory -Force -Path $tmp | Out-Null
try {
    Write-Host "Downloading $asset..."
    $zip = Join-Path $tmp 'pkg.zip'
    Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing

    # Verify against the published checksums when available.
    #
    # Only the fetch is allowed to fail softly -- checksums.txt may be missing
    # or the network may drop. The comparison sits outside the catch on purpose:
    # a mismatch used to be thrown into this handler and printed as "skipping
    # verification", so a tampered archive installed itself with a warning.
    # Being unable to check is not the same as checking and finding it wrong.
    # checksums.txt is downloaded to a file rather than read from .Content.
    # GitHub serves release assets as application/octet-stream whatever the API
    # metadata says, and Windows PowerShell 5.1 hands back a byte[] rather than
    # a string for that content type. Splitting a byte array on newlines matches
    # nothing and reports no error, so every Windows install announced "no
    # checksum published" and went ahead unverified. -OutFile does not care what
    # the content type is.
    $sumsFile = Join-Path $tmp 'checksums.txt'
    $haveSums = $false
    try {
        Invoke-WebRequest -Uri "https://github.com/$Repo/releases/download/$tag/checksums.txt" `
                          -OutFile $sumsFile -UseBasicParsing
        $haveSums = $true
    } catch { Write-Host "  (could not fetch checksums.txt: $($_.Exception.Message))" }

    if ($haveSums) {
        # Lines are "<sha256>  <filename>". Match the filename exactly rather
        # than by substring: claude-statusline_1.0.2_windows_amd64.zip is a
        # substring of nothing else today, but a future .zip.sig or .sbom would
        # match first and verify the wrong hash.
        $lines = @(Get-Content $sumsFile)
        $want  = $null
        foreach ($line in $lines) {
            $f = $line.Trim() -split '\s+'
            if ($f.Count -ge 2 -and $f[-1] -eq $asset) { $want = $f[0]; break }
        }
        if ($want) {
            $got = (Get-FileHash $zip -Algorithm SHA256).Hash.ToLower()
            if ($want.ToLower() -ne $got) {
                throw "checksum mismatch for $asset (expected $($want.ToLower()), got $got) -- refusing to install"
            }
            Write-Host 'Checksum OK.'
        } else {
            # Say what was actually read. The old silence made a parsing failure
            # indistinguishable from a release that published no checksums.
            Write-Host "  (no checksum published for $asset -- read $($lines.Count) entries from checksums.txt)"
        }
    }

    Expand-Archive -Path $zip -DestinationPath $tmp -Force
    New-Item -ItemType Directory -Force -Path $Prefix | Out-Null
    Copy-Item (Join-Path $tmp "$Bin.exe") (Join-Path $Prefix "$Bin.exe") -Force
    Write-Host "Installed $Prefix\$Bin.exe"

    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if ($userPath -notlike "*$Prefix*") {
        [Environment]::SetEnvironmentVariable('Path', "$userPath;$Prefix", 'User')
        Write-Host "Added $Prefix to your user PATH (restart your terminal to pick it up)."
    }

    Write-Host ''
    & (Join-Path $Prefix "$Bin.exe") init
} finally {
    Remove-Item $tmp -Recurse -Force -ErrorAction SilentlyContinue
}
