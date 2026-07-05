param(
  [Parameter(Mandatory = $true)][string] $Tag,
  [string] $BinaryName = "Chutes-E2EE-Chat"
)

$ErrorActionPreference = "Stop"

$runnerOS = [string] $env:RUNNER_OS
if (-not $runnerOS) {
  if ($IsWindows -or $env:OS -eq "Windows_NT") {
    $runnerOS = "Windows"
  } else {
    $runnerOS = "Unsupported"
  }
}

if ($runnerOS -ne "Windows") {
  throw "Release artifact packaging currently supports Windows builds only."
}

$releaseDir = Join-Path (Get-Location) "release"
New-Item -ItemType Directory -Force -Path $releaseDir | Out-Null

$source = Join-Path "build/bin" "$BinaryName.exe"
if (-not (Test-Path -LiteralPath $source)) {
  $source = Join-Path "build/bin" $BinaryName
}

if (-not (Test-Path -LiteralPath $source)) {
  throw "Expected Windows build output was not found: $source"
}

$destination = Join-Path $releaseDir "Chutes-E2EE-Chat_${Tag}_windows_amd64.exe"
Copy-Item -LiteralPath $source -Destination $destination -Force

Get-Item -LiteralPath $destination | Select-Object Name, Length | Format-Table -AutoSize
