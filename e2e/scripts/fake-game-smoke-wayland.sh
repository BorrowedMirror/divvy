#!/usr/bin/env bash
# Smoke test: fake-game as a native Wayland client under headless sway. Presses
# Ctrl+C / Ctrl+Alt+C with wtype (virtual-keyboard protocol) and checks wl-paste, then
# Ctrl+V with --log-keys.
# Needs: sway wtype wl-clipboard.
#   e2e/scripts/fake-game-smoke-wayland.sh [path/to/fake-game]
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
bin=${1:-$repo/target/e2e/fake-game}
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
WAYLAND_DISPLAY=$(basename "$sock")
export WAYLAND_DISPLAY

FAKE_GAME_DEBUG=${FAKE_GAME_DEBUG:-0} "$bin" --item "$work/n.txt" --advanced-item "$work/a.txt" --exit-after 30 --log-keys >"$work/out" 2>"$work/err" & pids+=($!)
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
# --log-keys: every press is logged, and Ctrl+V prints the clipboard another client owns.
grep -q '^KEY ctrl+KeyC$' "$work/out" || { echo "FAIL: no 'KEY ctrl+KeyC'"; cat "$work/out"; exit 1; }
grep -q '^KEY ctrl+alt+KeyC$' "$work/out" || { echo "FAIL: no 'KEY ctrl+alt+KeyC'"; cat "$work/out"; exit 1; }
printf 'pasted text' | wl-copy 2>/dev/null
wtype -s 200 -M ctrl v -m ctrl
for _ in $(seq 50); do grep -q '^PASTE pasted text$' "$work/out" && break; sleep 0.1; done
grep -q '^PASTE pasted text$' "$work/out" || { echo "FAIL: no 'PASTE pasted text'"; cat "$work/out" "$work/err"; exit 1; }
echo "OK: fake-game wayland smoke passed"
