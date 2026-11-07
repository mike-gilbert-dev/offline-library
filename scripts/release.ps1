# Builds, signs and (with -Publish) publishes a release.
#
#   release.ps1 -Version v0.2.1 -Notes notes.md            build + sign + zip only (check the output)
#   release.ps1 -Version v0.2.1 -Notes notes.md -Publish   also tag, push and create the GitHub release
#
# Signing uses the private key in %USERPROFILE%\.offline-library\release-signing.key
# (override with -Key). That key must never be committed or uploaded.
param(
  [Parameter(Mandatory)] [string]$Version,
  [Parameter(Mandatory)] [string]$Notes,
  [string]$Key = (Join-Path $env:USERPROFILE '.offline-library\release-signing.key'),
  [switch]$Publish
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
if ($Version -notmatch '^v\d+\.\d+\.\d+$') { throw "Version must look like v1.2.3 (got '$Version')" }
if (-not (Test-Path $Notes)) { throw "Release notes file not found: $Notes" }
if (-not (Test-Path $Key)) { throw "Signing key not found at $Key" }
$Notes = (Resolve-Path $Notes).Path

if ($Publish) {
  Push-Location $root
  try {
    if (git status --porcelain) { throw 'Commit or stash your changes first: a release must match a commit.' }
    if ((git rev-parse --abbrev-ref HEAD) -ne 'main') { throw 'Releases are made from main.' }
    if (git tag --list $Version) { throw "Tag $Version already exists." }
  } finally { Pop-Location }
}

# 1. Build all launchers with the version embedded (also writes release.json).
& (Join-Path $PSScriptRoot 'build.ps1') -Version $Version
$dist = Join-Path $root 'dist\drive'

# 2. Sign release.json, then verify it against the public key compiled into
#    the launcher, so a wrong or mismatched key can never be shipped.
$go = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $go) { $go = Join-Path $env:LOCALAPPDATA 'Programs\go\bin\go.exe' }
$signer = Join-Path $root 'tools\releasesign.exe'
Push-Location (Join-Path $root 'server')
try {
  & $go build -o $signer ./cmd/releasesign
  if ($LASTEXITCODE) { throw 'building releasesign failed' }
} finally { Pop-Location }

Write-Host '== Signing release.json'
& $signer sign -key $Key -in (Join-Path $dist 'release.json')
if ($LASTEXITCODE) { throw 'signing failed' }
$embedded = [regex]::Match((Get-Content -Raw (Join-Path $root 'server\releasekey.go')), 'mustPublicKey\("([^"]+)"\)').Groups[1].Value
& $signer verify -pub $embedded -in (Join-Path $dist 'release.json')
if ($LASTEXITCODE) { throw 'release.json does not verify against the public key in server/releasekey.go' }

# 3. Zip for manual installs.
$launchers = Get-ChildItem $dist -File | Where-Object { $_.Name -like 'start-*' -or $_.Name -like '*.command' }
$zip = Join-Path $root "dist\offline-library-$Version.zip"
Remove-Item $zip -ErrorAction SilentlyContinue
Compress-Archive -Path (@($launchers.FullName) + (Join-Path $dist 'SHA256SUMS.txt')) -DestinationPath $zip

# Release assets: the zip, each launcher (the updater downloads these by
# name), checksums, and the signed manifest. GitHub turns spaces in asset
# names into dots, so the .command helper ships inside the zip, and the
# updater maps names through release.json.
$assets = @($zip) + @($launchers | Where-Object { $_.Name -notlike '* *' } | ForEach-Object FullName) +
  @('SHA256SUMS.txt', 'release.json', 'release.json.sig' | ForEach-Object { Join-Path $dist $_ })
Write-Host ''
Write-Host "Release $Version is ready:"
$assets | ForEach-Object { '  {0,-45} {1,8:N1} MB' -f (Split-Path $_ -Leaf), ((Get-Item $_).Length / 1MB) }

if (-not $Publish) {
  Write-Host ''
  Write-Host 'Not published (run again with -Publish to tag, push and create the GitHub release).'
  return
}

# 4. Tag, push and publish.
Push-Location $root
try {
  git tag -a $Version -m "Offline Library $Version"
  git push origin main
  git push origin $Version
  gh release create $Version --title "Offline Library $Version" --notes-file $Notes @assets
  if ($LASTEXITCODE) { throw 'gh release create failed' }
} finally { Pop-Location }
