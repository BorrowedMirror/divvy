#!/usr/bin/env bash
# Hook-harness e2e on X11 (no WebDriver needed): Xvfb + openbox in a private D-Bus session,
# then e2e/macos-harness drives the app over its test hook. The price check is triggered by a
# real XTest `ctrl+d` (global hotkey) unless RKL_E2E_TRIGGER says otherwise.
#   e2e/scripts/run-hook-x11.sh
# Env: SKIP_BUILD=1 to reuse binaries; E2E_DISPLAY (default :99); RKL_E2E_TRIGGER
# (xdotool|hook|signal, default xdotool); see e2e/macos-harness/src/main.rs.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)

if [ -z "${RKL_E2E_IN_SESSION:-}" ]; then
  for tool in Xvfb openbox xdotool dbus-run-session; do
    command -v "$tool" >/dev/null || { echo "run-hook-x11: missing $tool" >&2; exit 2; }
  done
  if [ -z "${SKIP_BUILD:-}" ]; then
    (cd "$repo" && pnpm install --frozen-lockfile && pnpm --dir web build)
    (cd "$repo" && cargo build -p rkl-desktop --features test-hook,tauri/custom-protocol && cargo build -p fake-game)
  fi
  (cd "$repo" && cargo build --manifest-path e2e/macos-harness/Cargo.toml)
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
export XDG_SESSION_TYPE=x11 GDK_BACKEND=x11 RKL_BACKEND=x11
export RKL_E2E_TRIGGER=${RKL_E2E_TRIGGER:-xdotool}
export RKL_E2E_FOCUS_CMD=${RKL_E2E_FOCUS_CMD:-"xdotool search --sync --name '^Path of Exile 2\$' windowactivate --sync"}
for _ in $(seq 100); do xdotool getdisplaygeometry >/dev/null 2>&1 && break; sleep 0.1; done
openbox >/dev/null 2>&1 & pids+=($!)
sleep 0.5

"$repo/e2e/macos-harness/target/debug/rkl-macos-harness"
