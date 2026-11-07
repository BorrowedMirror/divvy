#!/usr/bin/env bash
# Builds what the e2e tests run, unless SKIP_BUILD=1: the web UI, the test-hook app
# (target/go/rkl-e2e), the fake game (target/e2e/fake-game) and on Linux the wl-keyboard helper
# (target/e2e/wl-keyboard, used by run-wayland.sh).
# Sourced by run-x11.sh and run-wayland.sh; also runnable on its own.
#   RKL_E2E_APP / RKL_E2E_FAKE_GAME override the binaries (then nothing is built for them).
set -euo pipefail
repo=${repo:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}

e2e_build() {
  [ -n "${SKIP_BUILD:-}" ] && return 0
  local tags=testhook
  [ "$(uname -s)" = Linux ] && tags=gtk3,testhook
  (
    cd "$repo"
    pnpm --dir web install --frozen-lockfile
    pnpm --dir web build
    if [ -z "${RKL_E2E_APP:-}" ]; then
      go build -tags "$tags" -o target/go/rkl-e2e ./cmd/rkl
    fi
    if [ "$(uname -s)" = Linux ]; then
      go build -o target/e2e/wl-keyboard ./e2e/cmd/wl-keyboard
    fi
    if [ -z "${RKL_E2E_FAKE_GAME:-}" ]; then
      go build -o target/e2e/fake-game ./e2e/cmd/fake-game
    fi
  )
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  e2e_build
fi
