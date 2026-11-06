# Downloads a large file with resume and retry, keeping the PC awake while it
# runs (no power settings are changed; the request ends with this process).
#
#   download-resumable.ps1 -Url <url> -Dest <final path>
#
# Data goes to "<Dest>.part" and is renamed only after the download completes,
# so a library scan never sees a half-written archive. Re-run the same command
# to resume after an interruption.
param(
  [Parameter(Mandatory)] [string]$Url,
  [Parameter(Mandatory)] [string]$Dest
)
$ErrorActionPreference = 'Stop'

Add-Type -Namespace OLib -Name Power -MemberDefinition '[DllImport("kernel32.dll")] public static extern uint SetThreadExecutionState(uint esFlags);'
$ES_CONTINUOUS = [uint32]'0x80000000'; $ES_SYSTEM_REQUIRED = [uint32]'0x00000001'
[void][OLib.Power]::SetThreadExecutionState($ES_CONTINUOUS -bor $ES_SYSTEM_REQUIRED)

$part = "$Dest.part"
New-Item -ItemType Directory -Force (Split-Path $Dest) | Out-Null
try {
  for ($attempt = 1; $attempt -le 100; $attempt++) {
    $have = if (Test-Path $part) { (Get-Item $part).Length } else { 0 }
    Write-Host ("[{0}] attempt {1}, resuming at {2:N2} GB" -f (Get-Date -Format s), $attempt, ($have / 1GB))
    # -C - resumes from the current .part size; stalls under 100 KB/s for 2 min abort and retry.
    curl.exe -L -sS --fail -C - --speed-limit 102400 --speed-time 120 -o $part $Url
    if ($LASTEXITCODE -eq 0) { break }
    Write-Host "curl exited with $LASTEXITCODE; retrying in 15 s"
    Start-Sleep -Seconds 15
  }
  if ($LASTEXITCODE -ne 0) { throw 'download did not complete after 100 attempts' }
  Move-Item -Force $part $Dest
  Write-Host ("[{0}] done: {1} ({2:N2} GB)" -f (Get-Date -Format s), $Dest, ((Get-Item $Dest).Length / 1GB))
} finally {
  [void][OLib.Power]::SetThreadExecutionState($ES_CONTINUOUS)
}
