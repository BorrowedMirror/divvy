#!/usr/bin/env bash
# Hook-harness e2e on headless sway (no WebDriver needed), inside a private D-Bus session.
#   e2e/scripts/run-hook-wayland.sh            Wayland-native app + fake-game (virtual keyboard,
#                                              wlr-data-control clipboard, layer-shell overlay)
#   E2E_XWAYLAND=1 e2e/scripts/run-hook-wayland.sh
#                                              app + fake-game as XWayland clients (X11 backend)
# Wayland-native: global hotkeys cannot be injected, so the price check is triggered over the
# test hook (`trigger 0`) or SIGUSR2 (RKL_E2E_TRIGGER=signal); the app's own copy chord
# still has to reach the focused fake game. Under XWayland the default trigger is a real XTest
# `ctrl+d` (xdotool) caught by the app's X11 global hotkey grab.
# Needs: sway swaymsg dbus-run-session xkbcli (+ Xwayland xdotool for E2E_XWAYLAND=1).
# Env: SKIP_BUILD=1; RKL_E2E_TRIGGER; see e2e/macos-harness/src/main.rs.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)

if [ -z "${RKL_E2E_IN_SESSION:-}" ]; then
  for tool in sway swaymsg dbus-run-session; do
    command -v "$tool" >/dev/null || { echo "run-hook-wayland: missing $tool" >&2; exit 2; }
  done
  if [ -n "${E2E_XWAYLAND:-}" ]; then
    for tool in Xwayland xdotool; do
      command -v "$tool" >/dev/null || { echo "run-hook-wayland: missing $tool" >&2; exit 2; }
    done
  fi
  if [ -z "${SKIP_BUILD:-}" ]; then
    (cd "$repo" && pnpm install --frozen-lockfile && pnpm --dir web build)
    (cd "$repo" && cargo build -p rkl-desktop --features test-hook,tauri/custom-protocol && cargo build -p fake-game)
  fi
  (cd "$repo" && cargo build --manifest-path e2e/macos-harness/Cargo.toml)
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
[ -n "$sock" ] || { echo "run-hook-wayland: sway did not start" >&2; cat "$work/sway.log" >&2; exit 1; }
export WAYLAND_DISPLAY=$(basename "$sock")
export SWAYSOCK=
for _ in $(seq 50); do
  SWAYSOCK=$(find "$XDG_RUNTIME_DIR" -maxdepth 1 -name 'sway-ipc.*' | head -1)
  [ -n "$SWAYSOCK" ] && break
  sleep 0.1
done

# Headless sway has no input devices, so its seat has no keyboard and sends clients a
# `no_keymap` keymap; enigo (the app's virtual-keyboard injector) then cannot map modifiers and
# Ctrl+Alt+C arrives without modifier state (and Xwayland's core keyboard has no modifier map,
# so XTest chords never reach X clients). A real session always has a keyboard: emulate one
# with a long-lived virtual keyboard carrying a full `us` keymap (e2e/macos-harness wl-keyboard).
{
  command -v xkbcli >/dev/null || { echo "run-hook-wayland: missing xkbcli (libxkbcommon tools)" >&2; exit 2; }
  xkbcli compile-keymap --layout us >"$work/us.xkb"
  mkfifo "$work/kbd.fifo"
  "$repo/e2e/macos-harness/target/debug/wl-keyboard" "$work/us.xkb" >"$work/kbd.fifo" & pids+=($!)
  read -r -t 10 line <"$work/kbd.fifo" && [ "$line" = READY ] \
    || { echo "run-hook-wayland: wl-keyboard did not start" >&2; exit 1; }
}

portal_dir=${PORTAL_LIBEXEC:-/usr/lib}
if [ -x "$portal_dir/xdg-desktop-portal-wlr" ]; then
  "$portal_dir/xdg-desktop-portal-wlr" >"$work/portal-wlr.log" 2>&1 & pids+=($!)
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
  [ -n "$DISPLAY" ] || { echo "run-hook-wayland: no Xwayland display" >&2; cat "$work/sway.log" >&2; exit 1; }
  # Both clients go through XWayland: hide the Wayland socket from them.
  wl=$WAYLAND_DISPLAY
  unset WAYLAND_DISPLAY
  export GDK_BACKEND=x11 RKL_BACKEND=x11 RKL_E2E_TRIGGER=${RKL_E2E_TRIGGER:-xdotool}
  export RKL_E2E_FOCUS_CMD=${RKL_E2E_FOCUS_CMD:-"WAYLAND_DISPLAY=$wl $focus"}
else
  export GDK_BACKEND=wayland RKL_BACKEND=wayland RKL_E2E_TRIGGER=${RKL_E2E_TRIGGER:-hook}
  export RKL_E2E_FOCUS_CMD=${RKL_E2E_FOCUS_CMD:-$focus}
fi

status=0
"$repo/e2e/macos-harness/target/debug/rkl-macos-harness" || status=$?
[ "$status" -eq 0 ] || { echo "--- sway.log (tail)" >&2; tail -20 "$work/sway.log" >&2; }
exit "$status"
