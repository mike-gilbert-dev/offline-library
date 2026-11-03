# Builds the UI, cross-compiles the launchers, and assembles dist/drive:
#
#   dist/drive/
#     start-windows.exe
#     start-mac-apple-silicon
#     start-mac-intel
#     start-linux
#     OfflineLibrary/        (copy of test-drive/OfflineLibrary)
#
# Copy the contents of dist/drive to the root of the external drive.
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent

$go = (Get-Command go -ErrorAction SilentlyContinue).Source
if (-not $go) { $go = Join-Path $env:LOCALAPPDATA 'Programs\go\bin\go.exe' }
if (-not (Test-Path $go)) { throw 'Go not found. Install Go or add it to PATH.' }

Write-Host '== Generating test library'
node (Join-Path $root 'scripts\make-test-drive.mjs')
if ($LASTEXITCODE) { throw 'make-test-drive failed' }

Write-Host '== Building UI'
Push-Location (Join-Path $root 'web')
try {
  if (-not (Test-Path node_modules)) { npm install; if ($LASTEXITCODE) { throw 'npm install failed' } }
  npm run check
  if ($LASTEXITCODE) { throw 'UI type check failed' }
  npm run build
  if ($LASTEXITCODE) { throw 'UI build failed' }
} finally { Pop-Location }
New-Item -ItemType File -Force (Join-Path $root 'server\webdist\.gitkeep') | Out-Null

$dist = Join-Path $root 'dist\drive'
if (Test-Path $dist) { Remove-Item -Recurse -Force $dist }
New-Item -ItemType Directory -Force $dist | Out-Null

$targets = @(
  @{ os = 'windows'; arch = 'amd64'; name = 'start-windows.exe' },
  @{ os = 'darwin';  arch = 'arm64'; name = 'start-mac-apple-silicon' },
  @{ os = 'darwin';  arch = 'amd64'; name = 'start-mac-intel' },
  @{ os = 'linux';   arch = 'amd64'; name = 'start-linux' }
)

$saved = @{ GOOS = $env:GOOS; GOARCH = $env:GOARCH; CGO_ENABLED = $env:CGO_ENABLED }
Push-Location (Join-Path $root 'server')
try {
  foreach ($t in $targets) {
    Write-Host "== Building $($t.name)"
    $env:GOOS = $t.os; $env:GOARCH = $t.arch; $env:CGO_ENABLED = '0'
    & $go build -trimpath -ldflags '-s -w' -o (Join-Path $dist $t.name) .
    if ($LASTEXITCODE) { throw "build failed for $($t.name)" }
  }
} finally {
  Pop-Location
  $env:GOOS = $saved.GOOS; $env:GOARCH = $saved.GOARCH; $env:CGO_ENABLED = $saved.CGO_ENABLED
}

Write-Host '== Copying test library'
Copy-Item -Recurse (Join-Path $root 'test-drive\OfflineLibrary') (Join-Path $dist 'OfflineLibrary')

Write-Host ''
Write-Host "Done. Drive layout is in $dist"
Get-ChildItem $dist | Format-Table Name, @{ n = 'Size'; e = { if ($_.PSIsContainer) { '<dir>' } else { '{0:N1} MB' -f ($_.Length / 1MB) } } } -AutoSize
