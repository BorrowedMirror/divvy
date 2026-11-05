#!/usr/bin/env bash
# e2e on native Wayland: headless sway + xdg-desktop-portal(-wlr) inside a private D-Bus
# session. Global hotkeys cannot be injected reliably here, so the price check is
# triggered over the test hook (`trigger 0`); the app's own copy chord still has to reach
# the fake game, which runs as a native Wayland client.
#   e2e/scripts/run-wayland.sh [wdio args...]
# Env: SKIP_BUILD=1; see e2e/lib/env.ts.
# Needs: sway swaymsg xkbcli xdg-desktop-portal xdg-desktop-portal-wlr dbus-run-session
#        tauri-driver WebKitWebDriver pnpm cargo.
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)

if [ -z "${RKL_E2E_IN_SESSION:-}" ]; then
  for tool in sway swaymsg xkbcli dbus-run-session; do
    command -v "$tool" >/dev/null || { echo "run-wayland: missing $tool" >&2; exit 2; }
  done
  if [ -z "${SKIP_BUILD:-}" ]; then
    (cd "$repo" && pnpm install --frozen-lockfile && pnpm --dir web build)
    (cd "$repo" && cargo build -p rkl-desktop --features test-hook,tauri/custom-protocol && cargo build -p fake-game)
  fi
  (cd "$repo" && cargo build --manifest-path e2e/macos-harness/Cargo.toml --bin wl-keyboard)
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
cat >"$work/sway.conf" <<'EOF'
output HEADLESS-1 resolution 1920x1080
exec dbus-update-activation-environment WAYLAND_DISPLAY XDG_CURRENT_DESKTOP
EOF
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
export WAYLAND_DISPLAY=$(basename "$sock")
export SWAYSOCK=
for _ in $(seq 50); do
  SWAYSOCK=$(find "$XDG_RUNTIME_DIR" -maxdepth 1 -name 'sway-ipc.*' | head -1)
  [ -n "$SWAYSOCK" ] && break
  sleep 0.1
done
[ -n "$SWAYSOCK" ] || { echo "run-wayland: no sway IPC socket" >&2; exit 1; }

# Headless sway has no input devices, so its seat has no keyboard and sends clients a
# `no_keymap` keymap; enigo (the app's virtual-keyboard injector) then cannot map modifiers and
# the copy chord arrives without modifier state, so the fake game never copies. A real session
# always has a keyboard: emulate one with a long-lived virtual keyboard carrying a full `us`
# keymap (e2e/macos-harness wl-keyboard), as run-hook-wayland.sh does.
xkbcli compile-keymap --layout us >"$work/us.xkb"
mkfifo "$work/kbd.fifo"
"$repo/e2e/macos-harness/target/debug/wl-keyboard" "$work/us.xkb" >"$work/kbd.fifo" & pids+=($!)
read -r -t 10 line <"$work/kbd.fifo" && [ "$line" = READY ] \
  || { echo "run-wayland: wl-keyboard did not start" >&2; exit 1; }

portal_dir=${PORTAL_LIBEXEC:-/usr/lib}
if [ -x "$portal_dir/xdg-desktop-portal-wlr" ]; then
  "$portal_dir/xdg-desktop-portal-wlr" >"$work/portal-wlr.log" 2>&1 & pids+=($!)
  "$portal_dir/xdg-desktop-portal" -r >"$work/portal.log" 2>&1 & pids+=($!)
else
  echo "run-wayland: xdg-desktop-portal-wlr not found in $portal_dir (set PORTAL_LIBEXEC); continuing" >&2
fi

export GDK_BACKEND=wayland RKL_BACKEND=wayland RKL_E2E_TRIGGER=${RKL_E2E_TRIGGER:-hook}
# The hook trigger skips the hotkey, so the specs focus the game first (e2e/lib/app.ts).
export RKL_E2E_FOCUS_CMD=${RKL_E2E_FOCUS_CMD:-"swaymsg -q '[title=\"^Path of Exile 2\$\"] focus'"}
cd "$repo/e2e"
status=0
pnpm exec wdio run ./wdio.conf.ts "$@" || status=$?
[ "$status" -eq 0 ] || { echo "--- sway.log (tail)" >&2; tail -20 "$work/sway.log" >&2; }
exit "$status"
