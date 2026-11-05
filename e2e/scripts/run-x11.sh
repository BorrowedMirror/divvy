#!/usr/bin/env bash
# e2e on X11: Xvfb + openbox inside a private D-Bus session, real XTest hotkey presses.
#   e2e/scripts/run-x11.sh [wdio args...]      e.g. --spec ./specs/price-check.e2e.ts
# Env: SKIP_BUILD=1 to reuse binaries; E2E_DISPLAY (default :99); see e2e/lib/env.ts.
# Needs: Xvfb openbox xdotool dbus-run-session tauri-driver WebKitWebDriver pnpm cargo.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)

if [ -z "${RKL_E2E_IN_SESSION:-}" ]; then
  for tool in Xvfb openbox xdotool dbus-run-session; do
    command -v "$tool" >/dev/null || { echo "run-x11: missing $tool" >&2; exit 2; }
  done
  if [ -z "${SKIP_BUILD:-}" ]; then
    (cd "$repo" && pnpm install --frozen-lockfile && pnpm --dir web build)
    # custom-protocol: serve web/dist from the binary instead of the Vite dev server.
    (cd "$repo" && cargo build -p rkl-desktop --features test-hook,tauri/custom-protocol && cargo build -p fake-game)
  fi
  export RKL_E2E_IN_SESSION=1
  exec dbus-run-session -- "$0" "$@"
fi

disp=${E2E_DISPLAY:-:99}
pids=()
cleanup() { for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done; }
trap cleanup EXIT

Xvfb "$disp" -screen 0 1920x1080x24 -nolisten tcp >/dev/null 2>&1 & pids+=($!)
export DISPLAY=$disp
unset WAYLAND_DISPLAY
export XDG_SESSION_TYPE=x11 GDK_BACKEND=x11 RKL_BACKEND=x11 RKL_E2E_TRIGGER=${RKL_E2E_TRIGGER:-xdotool}
for _ in $(seq 100); do xdotool getdisplaygeometry >/dev/null 2>&1 && break; sleep 0.1; done
openbox >/dev/null 2>&1 & pids+=($!)
sleep 0.5

cd "$repo/e2e"
pnpm exec wdio run ./wdio.conf.ts "$@"
