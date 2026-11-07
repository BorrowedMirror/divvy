#!/usr/bin/env bash
# e2e on macOS: builds the web UI, the test-hook app and the fake game, then runs
# `go test -tags e2e ./e2e/...` on the current desktop.
#   e2e/scripts/run-macos.sh [go test args...]      e.g. -run TestPriceCheck
# The price check is triggered over the test hook (`trigger 0`); the app's Cmd+C then has to
# reach the focused fake game, which needs the Accessibility permission for the process that
# runs the tests. Env: SKIP_BUILD=1 to reuse binaries; the RKL_E2E_* knobs in e2e/doc.go.
# Needs: go, pnpm, the Xcode command line tools, cargo (for tools/fake-game).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)

. "$here/build.sh"
e2e_build
export RKL_E2E_TRIGGER=${RKL_E2E_TRIGGER:-hook}
cd "$repo"
go test -tags e2e -count=1 -v ./e2e/... "$@"
