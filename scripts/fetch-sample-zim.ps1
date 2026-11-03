# Downloads the sample Wikipedia ZIM into the test library and verifies it
# against the SHA-256 that Kiwix publishes. Needs internet; run once.
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$name = 'wikipedia_en_physics_mini_2026-10.zim'   # ~51 MB
$url  = "https://download.kiwix.org/zim/wikipedia/$name"
$dir  = Join-Path $root 'test-drive\OfflineLibrary\packs\wikipedia-physics\content'
$dest = Join-Path $dir $name

New-Item -ItemType Directory -Force $dir | Out-Null
if (-not (Test-Path $dest)) {
  Write-Host "Downloading $name ..."
  curl.exe -sSL --fail -o $dest $url
  if ($LASTEXITCODE) { Remove-Item $dest -ErrorAction SilentlyContinue; throw 'download failed' }
}
$expected = ((curl.exe -sSL --fail "$url.sha256") -split '\s+')[0]
$actual = (Get-FileHash $dest -Algorithm SHA256).Hash.ToLower()
if ($expected -ne $actual) { Remove-Item $dest; throw "checksum mismatch (expected $expected, got $actual)" }
Write-Host "OK: $dest"
node (Join-Path $root 'scripts\make-test-drive.mjs')
