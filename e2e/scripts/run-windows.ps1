# e2e on Windows: tauri-driver + msedgedriver (must match the installed WebView2 version).
#   pwsh e2e/scripts/run-windows.ps1 [-SkipBuild] [wdio args...]
# The global hotkey is triggered over the test hook (`trigger 0`); the app's SendInput
# Ctrl+C then has to reach the focused fake game window.
param(
  [switch]$SkipBuild,
  [Parameter(ValueFromRemainingArguments = $true)][string[]]$WdioArgs
)
$ErrorActionPreference = "Stop"
$repo = Resolve-Path (Join-Path $PSScriptRoot "..\..")

if (-not (Get-Command tauri-driver -ErrorAction SilentlyContinue)) {
  throw "run-windows: tauri-driver not found; cargo install tauri-driver --locked"
}
if (-not $env:KALANDRA_NATIVE_DRIVER -and -not (Get-Command msedgedriver -ErrorAction SilentlyContinue)) {
  throw "run-windows: msedgedriver not in PATH. Download the version matching WebView2 " +
    "(see 'Get-AppxPackage *WebView2*' or edge://version) or set KALANDRA_NATIVE_DRIVER."
}

Push-Location $repo
try {
  if (-not $SkipBuild) {
    pnpm install --frozen-lockfile; if ($LASTEXITCODE) { throw "pnpm install failed" }
    pnpm --dir web build; if ($LASTEXITCODE) { throw "web build failed" }
    cargo build -p kalandra-desktop --features "test-hook,tauri/custom-protocol"; if ($LASTEXITCODE) { throw "app build failed" }
    cargo build -p fake-game; if ($LASTEXITCODE) { throw "fake-game build failed" }
  }
  $env:KALANDRA_BACKEND = "windows"
  if (-not $env:KALANDRA_E2E_TRIGGER) { $env:KALANDRA_E2E_TRIGGER = "hook" }
  Set-Location (Join-Path $repo "e2e")
  pnpm exec wdio run ./wdio.conf.ts @WdioArgs
  exit $LASTEXITCODE
} finally {
  Pop-Location
}
