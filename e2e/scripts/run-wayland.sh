#!/usr/bin/env bash
# e2e on headless sway inside a private D-Bus session, then `go test -tags e2e ./e2e/...`.
#   e2e/scripts/run-wayland.sh [go test args...]
#       Wayland-native app and fake game (virtual-keyboard copy chord, layer-shell overlay).
#       Global hotkeys cannot be injected here, so the price check is triggered over the test
#       hook (RKL_E2E_TRIGGER=hook, or signal for SIGUSR2/SIGUSR1); the app's own copy chord
#       still has to reach the focused fake game.
#   E2E_XWAYLAND=1 e2e/scripts/run-wayland.sh
#       app and fake game as XWayland clients (the X11 backend); the price check is a real XTest
#       key press caught by the app's X11 hotkey grab (RKL_E2E_TRIGGER=xdotool).
# Env: SKIP_BUILD=1; PORTAL_LIBEXEC (where xdg-desktop-portal(-wlr) live, default /usr/lib);
# the RKL_E2E_* knobs in e2e/doc.go.
# Needs: sway swaymsg dbus-run-session go pnpm, the wayland-client and xkbcommon headers (for
# the wl-keyboard helper and the fake game's Wayland window), xdg-desktop-portal +
# xdg-desktop-portal-wlr (optional), Xwayland + xdotool for E2E_XWAYLAND=1.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)

if [ -z "${RKL_E2E_IN_SESSION:-}" ]; then
  for tool in sway swaymsg dbus-run-session go; do
    command -v "$tool" >/dev/null || { echo "run-wayland: missing $tool" >&2; exit 2; }
  done
  if [ -n "${E2E_XWAYLAND:-}" ]; then
    for tool in Xwayland xdotool; do
      command -v "$tool" >/dev/null || { echo "run-wayland: missing $tool" >&2; exit 2; }
    done
  fi
  . "$here/build.sh"
  e2e_build
  export RKL_E2E_IN_SESSION=1
  exec dbus-run-session -- "$0" "$@"
fi

work=$(mktemp -d)
pids=()
cleanup() { for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done; rm -rf "$work"; }
trap cleanup EXIT

export XDG_RUNTIME_DIR=$work/run
mkdir -m 700 "$XDG_RUNTIME_DIR"
unset DISPLAY WAYLAND_DISPLAY
xwayland=disable
[ -n "${E2E_XWAYLAND:-}" ] && xwayland=enable
cat >"$work/sway.conf" <<CONF
output HEADLESS-1 resolution 1920x1080
xwayland $xwayland
exec dbus-update-activation-environment WAYLAND_DISPLAY DISPLAY XDG_CURRENT_DESKTOP
CONF
export XDG_CURRENT_DESKTOP=sway XDG_SESSION_TYPE=wayland
WLR_BACKENDS=headless WLR_RENDERER=pixman WLR_LIBINPUT_NO_DEVICES=1 \
  sway --unsupported-gpu -c "$work/sway.conf" >"$work/sway.log" 2>&1 & pids+=($!)
sock=
for _ in $(seq 100); do
  sock=$(find "$XDG_RUNTIME_DIR" -maxdepth 1 -name 'wayland-*' ! -name '*.lock' | head -1)
  [ -n "$sock" ] && break
  sleep 0.1
done
[ -n "$sock" ] || { echo "run-wayland: sway did not start" >&2; cat "$work/sway.log" >&2; exit 1; }
WAYLAND_DISPLAY=$(basename "$sock")
export WAYLAND_DISPLAY
export SWAYSOCK=
for _ in $(seq 50); do
  SWAYSOCK=$(find "$XDG_RUNTIME_DIR" -maxdepth 1 -name 'sway-ipc.*' | head -1)
  [ -n "$SWAYSOCK" ] && break
  sleep 0.1
done
[ -n "$SWAYSOCK" ] || { echo "run-wayland: no sway IPC socket" >&2; exit 1; }

# Headless sway has no input devices, so its seat has no keyboard and sends clients a
# `no_keymap` keymap: modifiers of injected chords get lost and Xwayland's core keyboard has no
# modifier map, so XTest chords never reach X clients. A real session always has a keyboard:
# emulate one with a long-lived virtual keyboard carrying a full keymap (e2e/cmd/wl-keyboard).
kbd=${RKL_E2E_WL_KEYBOARD:-$repo/target/e2e/wl-keyboard}
[ -x "$kbd" ] || { echo "run-wayland: $kbd missing; go build -o target/e2e/wl-keyboard ./e2e/cmd/wl-keyboard" >&2; exit 2; }
mkfifo "$work/kbd.fifo"
XKB_DEFAULT_LAYOUT=us "$kbd" >"$work/kbd.fifo" & pids+=($!)
read -r -t 10 line <"$work/kbd.fifo" && [ "$line" = READY ] \
  || { echo "run-wayland: wl-keyboard did not start" >&2; exit 1; }

portal_dir=${PORTAL_LIBEXEC:-/usr/lib}
if [ -x "$portal_dir/xdg-desktop-portal-wlr" ]; then
  "$portal_dir/xdg-desktop-portal-wlr" >"$work/portal-wlr.log" 2>&1 & pids+=($!)
  "$portal_dir/xdg-desktop-portal" -r >"$work/portal.log" 2>&1 & pids+=($!)
else
  echo "run-wayland: xdg-desktop-portal-wlr not found in $portal_dir (set PORTAL_LIBEXEC); continuing" >&2
fi

focus="swaymsg -q '[title=\"^Path of Exile 2\$\"] focus'"
if [ -n "${E2E_XWAYLAND:-}" ]; then
  # sway exports DISPLAY to the commands it execs once Xwayland is ready; ask it.
  export DISPLAY=
  for _ in $(seq 100); do
    swaymsg -q exec "echo \$DISPLAY > $work/display" >/dev/null 2>&1 || true
    sleep 0.1
    DISPLAY=$(cat "$work/display" 2>/dev/null || true)
    [ -n "$DISPLAY" ] && break
  done
  [ -n "$DISPLAY" ] || { echo "run-wayland: no Xwayland display" >&2; cat "$work/sway.log" >&2; exit 1; }
  # Both clients go through XWayland: hide the Wayland socket from them (and the tests).
  wl=$WAYLAND_DISPLAY
  unset WAYLAND_DISPLAY
  export GDK_BACKEND=x11 RKL_BACKEND=x11 RKL_E2E_TRIGGER=${RKL_E2E_TRIGGER:-xdotool}
  export RKL_E2E_FOCUS_CMD=${RKL_E2E_FOCUS_CMD:-"WAYLAND_DISPLAY=$wl $focus"}
else
  export GDK_BACKEND=wayland RKL_BACKEND=wayland RKL_E2E_TRIGGER=${RKL_E2E_TRIGGER:-hook}
  export RKL_E2E_FOCUS_CMD=${RKL_E2E_FOCUS_CMD:-$focus}
fi

cd "$repo"
status=0
go test -tags e2e -count=1 -v ./e2e/... "$@" || status=$?
[ "$status" -eq 0 ] || { echo "--- sway.log (tail)" >&2; tail -20 "$work/sway.log" >&2; }
exit "$status"
