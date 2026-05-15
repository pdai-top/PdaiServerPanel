param(
    [switch]$SkipFrontend
)

$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $root

$version = Get-Date -Format "yyMMddHHss"
$ldflags = "-s -w -X main.Version=$version"

function Invoke-Checked {
    param(
        [scriptblock]$Command,
        [string]$Name
    )

    & $Command
    if ($LASTEXITCODE -ne 0) {
        throw "$Name failed with exit code $LASTEXITCODE"
    }
}

if (-not $SkipFrontend) {
    Push-Location "web"
    try {
        Invoke-Checked { npm.cmd run build } "frontend build"
    } finally {
        Pop-Location
    }
}

New-Item -ItemType Directory -Force -Path "dist" | Out-Null
New-Item -ItemType Directory -Force -Path ".gocache" | Out-Null
New-Item -ItemType Directory -Force -Path ".gomodcache" | Out-Null

$env:CGO_ENABLED = "0"
$env:GOCACHE = Join-Path $root ".gocache"
$env:GOMODCACHE = Join-Path $root ".gomodcache"

$env:GOOS = "linux"
$env:GOARCH = "amd64"
Invoke-Checked { go build -trimpath -ldflags $ldflags -o "dist/pdai-linux-amd64" . } "linux amd64 build"

$env:GOARCH = "arm64"
Invoke-Checked { go build -trimpath -ldflags $ldflags -o "dist/pdai-linux-arm64" . } "linux arm64 build"

Write-Host "Built Pdai version $version"
Write-Host "  dist/pdai-linux-amd64"
Write-Host "  dist/pdai-linux-arm64"
