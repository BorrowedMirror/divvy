#!/usr/bin/env bash
# Smoke test: fake-game under Xvfb + openbox. Presses Ctrl+C / Ctrl+Alt+C via XTest
# (xdotool) and checks the clipboard with xclip, then Ctrl+V with --log-keys.
# Needs: Xvfb openbox xdotool xclip.
#   e2e/scripts/fake-game-smoke-x11.sh [path/to/fake-game]
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../.." && pwd)
bin=${1:-$repo/target/e2e/fake-game}
disp=${SMOKE_DISPLAY:-:97}
work=$(mktemp -d)
pids=()
cleanup() { for p in "${pids[@]}"; do kill "$p" 2>/dev/null || true; done; rm -rf "$work"; }
trap cleanup EXIT

printf 'Item Class: Stackable Currency\nRarity: Currency\nDivine Orb\n' >"$work/n.txt"
printf 'Item Class: Stackable Currency\nRarity: Currency\nDivine Orb\n--------\nADVANCED\n' >"$work/a.txt"

Xvfb "$disp" -screen 0 1920x1080x24 -nolisten tcp >/dev/null 2>&1 & pids+=($!)
export DISPLAY=$disp
unset WAYLAND_DISPLAY
for _ in $(seq 50); do xdpyinfo >/dev/null 2>&1 && break; sleep 0.1; done
openbox >/dev/null 2>&1 & pids+=($!)
sleep 0.5

"$bin" --item "$work/n.txt" --advanced-item "$work/a.txt" --exit-after 30 --log-keys >"$work/out" 2>"$work/err" & pids+=($!)
for _ in $(seq 100); do grep -q READY "$work/out" && break; sleep 0.1; done
grep -q READY "$work/out" || { echo "FAIL: no READY"; cat "$work/out" "$work/err"; exit 1; }

wid=$(xdotool search --name '^Path of Exile 2$' | head -1)
[ -n "$wid" ] || { echo "FAIL: window 'Path of Exile 2' not found"; exit 1; }
xdotool windowactivate --sync "$wid"

expect() { # <copies> <text-file>
  for _ in $(seq 50); do grep -q "COPIED $1" "$work/out" && break; sleep 0.1; done
  grep -q "COPIED $1" "$work/out" || { echo "FAIL: no 'COPIED $1'"; cat "$work/out" "$work/err"; exit 1; }
  got=$(timeout 3 xclip -o -selection clipboard)
  [ "$got" == "$(cat "$2")" ] || { echo "FAIL: clipboard mismatch: $got"; exit 1; }
}
xdotool key --clearmodifiers ctrl+c
expect 1 "$work/n.txt"
xdotool key --clearmodifiers ctrl+alt+c
expect 2 "$work/a.txt"
# --log-keys: every press is logged, and Ctrl+V prints the clipboard another client owns.
grep -q '^KEY ctrl+KeyC$' "$work/out" || { echo "FAIL: no 'KEY ctrl+KeyC'"; cat "$work/out"; exit 1; }
grep -q '^KEY ctrl+alt+KeyC$' "$work/out" || { echo "FAIL: no 'KEY ctrl+alt+KeyC'"; cat "$work/out"; exit 1; }
printf 'pasted text' | xclip -selection clipboard -i 2>/dev/null
xdotool key --clearmodifiers ctrl+v
for _ in $(seq 50); do grep -q '^PASTE pasted text$' "$work/out" && break; sleep 0.1; done
grep -q '^PASTE pasted text$' "$work/out" || { echo "FAIL: no 'PASTE pasted text'"; cat "$work/out" "$work/err"; exit 1; }
echo "OK: fake-game x11 smoke passed"
