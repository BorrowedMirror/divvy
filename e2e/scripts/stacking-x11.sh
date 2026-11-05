#!/usr/bin/env bash
# Regression check: over a FULLSCREEN, focused game under Xvfb + openbox (openbox puts a focused
# fullscreen client in a layer above "always on top" windows):
#   - the unpinned price panel stacks above the game and leaves the game focused,
#   - the pinned price panel stacks above the game and takes focus,
#   - the settings window covers the game exactly, above it, focused.
# Needs: Xvfb openbox xdotool wmctrl xprop xwininfo dbus-run-session python3, and binaries built
# with `cargo build -p kalandra-desktop --features test-hook,tauri/custom-protocol && cargo build -p fake-game`.
set -euo pipefail
cd "$(dirname "$0")/../.."
APP=${KALANDRA_APP_BIN:-target/debug/kalandra}
GAMEBIN=${KALANDRA_FAKE_GAME_BIN:-target/debug/fake-game}
DISP=${STACK_DISPLAY:-:87}
PORT=${STACK_HOOK_PORT:-47188}
Xvfb "$DISP" -screen 0 1920x1080x24 >/dev/null 2>&1 &
XV=$!
trap 'kill $XV 2>/dev/null || true' EXIT
sleep 1
export DISPLAY=$DISP APP GAMEBIN PORT
exec dbus-run-session -- bash -euo pipefail -c '
fail() { echo "FAIL: $*"; exit 1; }
openbox >/dev/null 2>&1 & sleep 1
CFG=$(mktemp -d); mkdir -p "$CFG/config"
printf "version = 1\n\n[ui]\nonboarding_done = true\n" > "$CFG/config/settings.toml"
LOG=$(mktemp -d)
"$GAMEBIN" --item e2e/fixtures/rare-ring.txt --exit-after 60 >"$LOG/game" 2>&1 & FG=$!
for _ in $(seq 50); do grep -q READY "$LOG/game" && break; sleep 0.1; done
GAME=$(xdotool search --name "^Path of Exile 2$" | head -1)
wmctrl -i -r "$GAME" -b add,fullscreen; xdotool windowactivate --sync "$GAME"; sleep 0.5
KALANDRA_TEST_HOOK=$PORT KALANDRA_CONFIG_DIR=$CFG KALANDRA_LOG=warn "$APP" >"$LOG/app" 2>&1 & PID=$!
trap "kill $FG $PID 2>/dev/null || true" EXIT
for _ in $(seq 100); do (exec 3<>/dev/tcp/127.0.0.1/$PORT) 2>/dev/null && break; sleep 0.1; done
hook() { exec 3<>/dev/tcp/127.0.0.1/$PORT; echo "$1" >&3; head -1 <&3; exec 3>&-; }
name() { xprop -id "$1" _NET_WM_NAME 2>/dev/null | sed "s/.*= //; s/\"//g"; }
top() { name "$(xprop -root _NET_CLIENT_LIST_STACKING | sed "s/.*# //; s/.*, //")"; }
active() { name "$(xprop -root _NET_ACTIVE_WINDOW | sed "s/.*# //")"; }
xdotool windowactivate --sync "$GAME"; sleep 0.3

hook "text $(python3 -c "import json;print(json.dumps(open(\"e2e/fixtures/rare-ring.txt\").read()))")" >/dev/null
sleep 0.8
[ "$(top)" = "kalandra overlay" ] || fail "unpinned panel is not above the game (top: $(top))"
[ "$(active)" = "Path of Exile 2" ] || fail "unpinned panel stole focus (active: $(active))"
echo "ok: unpinned panel above the fullscreen game, game keeps focus"

xdotool windowactivate --sync "$GAME"; sleep 0.3
hook "tray price_check" >/dev/null; sleep 1.5
[ "$(top)" = "kalandra overlay" ] || fail "pinned panel is not above the game (top: $(top))"
[ "$(active)" = "kalandra overlay" ] || fail "pinned panel did not take focus (active: $(active))"
echo "ok: pinned panel above the game and focused"

hook "tray open_settings" >/dev/null; sleep 0.8
[ "$(top)" = "kalandra settings" ] || fail "settings not on top (top: $(top))"
[ "$(active)" = "kalandra settings" ] || fail "settings not focused (active: $(active))"
geo() { xwininfo -id "$1" | awk "/Absolute upper-left X/{x=\$NF}/Absolute upper-left Y/{y=\$NF}/Width/{w=\$NF}/Height/{h=\$NF}END{print x\",\"y\",\"w\",\"h}"; }
SW=$(xdotool search --name "^kalandra settings$" | head -1)
[ "$(geo "$SW")" = "$(geo "$GAME")" ] || fail "settings $(geo "$SW") does not cover the game $(geo "$GAME")"
echo "ok: settings overlay covers the game ($(geo "$SW")) and is focused"
hook quit >/dev/null || true
echo "PASS: stacking over a fullscreen game"
'
