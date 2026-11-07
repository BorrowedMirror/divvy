# e2e on Windows: builds the web UI, the test-hook app and the fake game, then runs
# `go test -tags e2e ./e2e/...` on the current desktop.
#   pwsh e2e/scripts/run-windows.ps1 [-SkipBuild] [go test args...]      e.g. -run TestPriceCheck
# The price check is triggered over the test hook (`trigger 0`); the app's SendInput Ctrl+C
# then has to reach the focused fake game window. Knobs: the RKL_E2E_* variables in e2e/doc.go.
# Needs: go, pnpm, cargo (for tools/fake-game).
param(
  [switch]$SkipBuild,
  [Parameter(ValueFromRemainingArguments = $true)][string[]]$GoTestArgs
)
$ErrorActionPreference = "Stop"
$repo = Resolve-Path (Join-Path $PSScriptRoot "..\..")

Push-Location $repo
try {
  if (-not $SkipBuild) {
    pnpm install --frozen-lockfile; if ($LASTEXITCODE) { throw "pnpm install failed" }
    pnpm --dir web build; if ($LASTEXITCODE) { throw "web build failed" }
    if (-not $env:RKL_E2E_APP) {
      go build -tags testhook -o target/go/rkl-e2e.exe ./cmd/rkl; if ($LASTEXITCODE) { throw "app build failed" }
    }
    if (-not $env:RKL_E2E_FAKE_GAME -and -not (Test-Path target/e2e/debug/fake-game.exe)) {
      cargo build -p fake-game --target-dir target/e2e; if ($LASTEXITCODE) { throw "fake-game build failed" }
    }
  }
  $env:RKL_BACKEND = "windows"
  if (-not $env:RKL_E2E_TRIGGER) { $env:RKL_E2E_TRIGGER = "hook" }
  go test -tags e2e -count=1 -v ./e2e/... @GoTestArgs
  exit $LASTEXITCODE
} finally {
  Pop-Location
}
