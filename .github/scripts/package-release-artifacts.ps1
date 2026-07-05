param(
  [Parameter(Mandatory = $true)][string] $Tag,
  [string] $BinaryName = "Chutes-E2EE-Chat"
)

$ErrorActionPreference = "Stop"

$releaseDir = Join-Path (Get-Location) "release"
New-Item -ItemType Directory -Force -Path $releaseDir | Out-Null
$runnerOS = [string] $env:RUNNER_OS

if (-not $runnerOS) {
  if ($IsWindows -or $env:OS -eq "Windows_NT") {
    $runnerOS = "Windows"
  } elseif ($IsMacOS) {
    $runnerOS = "macOS"
  } else {
    $runnerOS = "Linux"
  }
}

function Assert-PathExists {
  param([Parameter(Mandatory = $true)][string] $Path)

  if (-not (Test-Path -LiteralPath $Path)) {
    throw "Expected build output was not found: $Path"
  }
}

switch ($runnerOS) {
  "Windows" {
    $source = Join-Path "build/bin" "$BinaryName.exe"
    if (-not (Test-Path -LiteralPath $source)) {
      $source = Join-Path "build/bin" $BinaryName
    }
    Assert-PathExists $source
    Copy-Item -LiteralPath $source -Destination (Join-Path $releaseDir "Chutes-E2EE-Chat_${Tag}_windows_amd64.exe") -Force
  }
  "macOS" {
    $source = Join-Path "build/bin" "$BinaryName.app"
    $destination = Join-Path $releaseDir "Chutes-E2EE-Chat_${Tag}_macos_universal.zip"
    Assert-PathExists $source
    & ditto -c -k --sequesterRsrc --keepParent $source $destination
    if ($LASTEXITCODE -ne 0) {
      throw "Failed to archive macOS app bundle."
    }
  }
  default {
    $source = Join-Path "build/bin" $BinaryName
    $destination = Join-Path $releaseDir "Chutes-E2EE-Chat_${Tag}_linux_amd64.tar.gz"
    Assert-PathExists $source
    & tar -czf $destination -C "build/bin" $BinaryName
    if ($LASTEXITCODE -ne 0) {
      throw "Failed to archive Linux binary."
    }
  }
}

$files = Get-ChildItem -LiteralPath $releaseDir -File
if (-not $files) {
  throw "No release artifacts were produced."
}

$files | Select-Object Name, Length | Format-Table -AutoSize
