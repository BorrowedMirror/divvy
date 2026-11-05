#!/usr/bin/env bash
# Smoke test: fake-game as a native Wayland client under headless sway. Presses
# Ctrl+C / Ctrl+Alt+C with wtype (virtual-keyboard protocol) and checks wl-paste.
# Needs: sway wtype wl-clipboard.
#   tools/fake-game/scripts/smoke-wayland.sh [path/to/fake-game]
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../../.." && pwd)
bin=${1:-$repo/target/debug/fake-game}
work=$(mktemp -d)
pids=()
cleanup() { for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done; rm -rf "$work"; }
trap cleanup EXIT

printf 'Item Class: Stackable Currency\nRarity: Currency\nDivine Orb\n' >"$work/n.txt"
printf 'Item Class: Stackable Currency\nRarity: Currency\nDivine Orb\n--------\nADVANCED\n' >"$work/a.txt"

export XDG_RUNTIME_DIR=$work/run
mkdir -m 700 "$XDG_RUNTIME_DIR"
printf 'output HEADLESS-1 resolution 1920x1080\n' >"$work/sway.conf"
unset DISPLAY WAYLAND_DISPLAY
WLR_BACKENDS=headless WLR_RENDERER=pixman WLR_LIBINPUT_NO_DEVICES=1 \
  sway --unsupported-gpu -c "$work/sway.conf" >"$work/sway.log" 2>&1 & pids+=($!)
for _ in $(seq 100); do
  sock=$(find "$XDG_RUNTIME_DIR" -maxdepth 1 -name 'wayland-*' ! -name '*.lock' | head -1)
  [ -n "$sock" ] && break
  sleep 0.1
done
[ -n "${sock:-}" ] || { echo "FAIL: sway did not start"; cat "$work/sway.log"; exit 1; }
export WAYLAND_DISPLAY=$(basename "$sock")

FAKE_GAME_DEBUG=${FAKE_GAME_DEBUG:-0} "$bin" --item "$work/n.txt" --advanced-item "$work/a.txt" --exit-after 30 >"$work/out" 2>"$work/err" & pids+=($!)
for _ in $(seq 100); do grep -q READY "$work/out" && break; sleep 0.1; done
grep -q READY "$work/out" || { echo "FAIL: no READY"; cat "$work/out" "$work/err"; exit 1; }
sleep 0.3 # let sway focus the new toplevel

expect() { # <copies> <text-file>
  for _ in $(seq 50); do grep -q "COPIED $1" "$work/out" && break; sleep 0.1; done
  grep -q "COPIED $1" "$work/out" || { echo "FAIL: no 'COPIED $1'"; cat "$work/out" "$work/err"; exit 1; }
  got=$(timeout 3 wl-paste --no-newline)
  [ "$got" == "$(cat "$2")" ] || { echo "FAIL: clipboard mismatch: $got"; exit 1; }
}
wtype -s 200 -M ctrl c -m ctrl
expect 1 "$work/n.txt"
wtype -s 200 -M ctrl -M alt c -m alt -m ctrl
expect 2 "$work/a.txt"
echo "OK: fake-game wayland smoke passed"
