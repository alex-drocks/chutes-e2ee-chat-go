$ErrorActionPreference = "Stop"

function Assert-SemVer {
  param([Parameter(Mandatory = $true)][string] $Version)

  if ($Version -notmatch '^\d+\.\d+\.\d+$') {
    throw "Version '$Version' must use MAJOR.MINOR.PATCH format."
  }
}

function Test-GitRef {
  param([Parameter(Mandatory = $true)][string] $Ref)

  git rev-parse -q --verify $Ref *> $null
  $exists = $LASTEXITCODE -eq 0
  $global:LASTEXITCODE = 0
  return $exists
}

function Write-OutputValue {
  param(
    [Parameter(Mandatory = $true)][string] $Name,
    [Parameter(Mandatory = $true)][string] $Value
  )

  "$Name=$Value" | Out-File -FilePath $env:GITHUB_OUTPUT -Append -Encoding utf8
}

git fetch --tags --force

$eventName = [string] $env:GITHUB_EVENT_NAME
$createTag = "false"

if ($eventName -eq "push") {
  $tag = [string] $env:GITHUB_REF_NAME
  if ($tag -notmatch '^v(\d+\.\d+\.\d+)$') {
    throw "Release tags must look like v0.1.0."
  }
  $version = $Matches[1]
} else {
  $explicitVersion = ([string] $env:RELEASE_VERSION_INPUT).Trim()

  if ($explicitVersion) {
    $version = $explicitVersion.TrimStart("v")
    Assert-SemVer $version
    $tag = "v$version"
    if (-not (Test-GitRef "refs/tags/$tag")) {
      $createTag = "true"
    }
  } else {
    $latest = $null
    $tags = git tag --list "v[0-9]*" --sort=-v:refname

    foreach ($candidate in $tags) {
      if ($candidate -match '^v(\d+)\.(\d+)\.(\d+)$') {
        $latest = @{
          major = [int] $Matches[1]
          minor = [int] $Matches[2]
          patch = [int] $Matches[3]
        }
        break
      }
    }

    if (-not $latest) {
      $packageJson = Get-Content -Raw -LiteralPath package.json | ConvertFrom-Json
      if ($packageJson.version -notmatch '^(\d+)\.(\d+)\.(\d+)$') {
        throw "package.json version must use MAJOR.MINOR.PATCH format."
      }
      $latest = @{
        major = [int] $Matches[1]
        minor = [int] $Matches[2]
        patch = [int] $Matches[3]
      }
    }

    $bump = ([string] $env:RELEASE_BUMP).Trim()
    if (-not $bump) {
      $bump = "patch"
    }

    switch ($bump) {
      "major" {
        $latest.major += 1
        $latest.minor = 0
        $latest.patch = 0
      }
      "minor" {
        $latest.minor += 1
        $latest.patch = 0
      }
      "patch" {
        $latest.patch += 1
      }
      default {
        throw "Unsupported version bump '$bump'."
      }
    }

    $version = "$($latest.major).$($latest.minor).$($latest.patch)"
    $tag = "v$version"
    if (Test-GitRef "refs/tags/$tag") {
      throw "Tag $tag already exists. Use the explicit version input to rebuild an existing release."
    }
    $createTag = "true"
  }
}

Write-OutputValue "version" $version
Write-OutputValue "tag" $tag
Write-OutputValue "create_tag" $createTag
