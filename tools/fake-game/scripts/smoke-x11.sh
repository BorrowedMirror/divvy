#!/usr/bin/env bash
# Smoke test: fake-game under Xvfb + openbox. Presses Ctrl+C / Ctrl+Alt+C via XTest
# (xdotool) and checks the clipboard with xclip. Needs: Xvfb openbox xdotool xclip.
#   tools/fake-game/scripts/smoke-x11.sh [path/to/fake-game]
set -euo pipefail
here=$(cd "$(dirname "$0")" && pwd)
repo=$(cd "$here/../../.." && pwd)
bin=${1:-$repo/target/debug/fake-game}
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

"$bin" --item "$work/n.txt" --advanced-item "$work/a.txt" --exit-after 30 >"$work/out" 2>"$work/err" & pids+=($!)
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
echo "OK: fake-game x11 smoke passed"
