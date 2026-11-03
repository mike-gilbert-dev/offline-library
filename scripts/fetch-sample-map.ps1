# Builds the sample map pack (test-drive/.../packs/regional-maps):
#   1. Copies Protomaps fonts and sprites (OFL / MIT) into the pack.
#   2. Extracts a region from the Protomaps OpenStreetMap planet build with
#      the official pmtiles CLI. Only the needed byte ranges are downloaded.
#   3. Writes light and dark MapLibre styles and regenerates the manifests.
#
# Needs internet. Requires tools\pmtiles\pmtiles.exe (from
# https://github.com/protomaps/go-pmtiles/releases).
param(
  # minLon,minLat,maxLon,maxLat. The default is Washington, D.C. and surroundings.
  [string]$Bbox = '-77.25,38.78,-76.88,39.03',
  [int]$MaxZoom = 15,
  [string]$Build = '20261102'
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$pack = Join-Path $root 'test-drive\OfflineLibrary\packs\regional-maps'
$pmtiles = Join-Path $root 'tools\pmtiles\pmtiles.exe'
if (-not (Test-Path $pmtiles)) { throw "pmtiles CLI not found at $pmtiles" }

# 1. Fonts and sprites, downloaded into a scratch folder and copied into the pack.
$dl = Join-Path $env:TEMP 'olib-basemaps-assets'
if (-not (Test-Path "$dl\x")) {
  New-Item -ItemType Directory -Force $dl | Out-Null
  curl.exe -sSL --fail -o "$dl\assets.zip" https://github.com/protomaps/basemaps-assets/archive/refs/heads/main.zip
  if ($LASTEXITCODE) { throw 'asset download failed' }
  Expand-Archive "$dl\assets.zip" -DestinationPath "$dl\x" -Force
}
$src = (Get-ChildItem "$dl\x" -Directory | Select-Object -First 1).FullName
$assets = Join-Path $pack 'assets'
if (Test-Path $assets) { Remove-Item -Recurse -Force $assets }
New-Item -ItemType Directory -Force "$assets\fonts", "$assets\sprites" | Out-Null
foreach ($font in 'Noto Sans Regular', 'Noto Sans Medium', 'Noto Sans Italic') {
  Copy-Item -Recurse (Join-Path $src "fonts\$font") "$assets\fonts\$font"
}
Copy-Item (Join-Path $src 'fonts\OFL.txt') "$assets\fonts\OFL.txt"
foreach ($f in 'light', 'dark') {
  foreach ($ext in '.json', '.png', '@2x.json', '@2x.png') { Copy-Item (Join-Path $src "sprites\v4\$f$ext") "$assets\sprites\$f$ext" }
}

# 2. Region extract
New-Item -ItemType Directory -Force "$pack\content" | Out-Null
$out = Join-Path $pack 'content\region.pmtiles'
if (Test-Path $out) { Remove-Item $out }
& $pmtiles extract "https://build.protomaps.com/$Build.pmtiles" $out "--bbox=$Bbox" "--maxzoom=$MaxZoom"
if ($LASTEXITCODE) { throw 'pmtiles extract failed' }
& $pmtiles verify $out
if ($LASTEXITCODE) { throw 'pmtiles verify failed' }

# 3. Styles and manifests
node (Join-Path $root 'scripts\make-map-style.mjs') $pack en
node (Join-Path $root 'scripts\make-test-drive.mjs')
