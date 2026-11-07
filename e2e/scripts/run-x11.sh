#!/usr/bin/env bash
# e2e on X11: Xvfb + openbox inside a private D-Bus session, then `go test -tags e2e ./e2e/...`.
# The price check is a real XTest key press of the global hotkey (RKL_E2E_TRIGGER=xdotool).
#   e2e/scripts/run-x11.sh [go test args...]      e.g. -run TestPriceCheck
# Env: SKIP_BUILD=1 to reuse binaries; E2E_DISPLAY (default :99); the RKL_E2E_* knobs in
# e2e/doc.go.
# Needs: Xvfb openbox xdotool dbus-run-session go pnpm (cargo if fake-game is not built).
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)

if [ -z "${RKL_E2E_IN_SESSION:-}" ]; then
  for tool in Xvfb openbox xdotool dbus-run-session go; do
    command -v "$tool" >/dev/null || { echo "run-x11: missing $tool" >&2; exit 2; }
  done
  . "$here/build.sh"
  e2e_build
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

cd "$repo"
go test -tags e2e -count=1 -v ./e2e/... "$@"
