#!/usr/bin/env bash
# Regression check: over a FULLSCREEN, focused game under Xvfb + openbox (openbox puts a focused
# fullscreen client in a layer above "always on top" windows):
#   - the unpinned price panel stacks above the game and leaves the game focused,
#   - the pinned price panel stacks above the game and takes focus,
#   - the settings window covers the game exactly, above it, focused.
# Needs: Xvfb openbox xdotool wmctrl xprop xwininfo dbus-run-session python3, and the binaries
# e2e/scripts/build.sh builds (test-hook app target/go/rkl-e2e, target/e2e/debug/fake-game);
# RKL_E2E_APP / RKL_E2E_FAKE_GAME override them.
set -euo pipefail
cd "$(dirname "$0")/../.."
APP=${RKL_E2E_APP:-target/go/rkl-e2e}
GAMEBIN=${RKL_E2E_FAKE_GAME:-target/e2e/debug/fake-game}
DISP=${STACK_DISPLAY:-:87}
PORT=${STACK_HOOK_PORT:-47188}
Xvfb "$DISP" -screen 0 1920x1080x24 >/dev/null 2>&1 &
XV=$!
trap 'kill $XV 2>/dev/null || true' EXIT
sleep 1
export DISPLAY=$DISP APP GAMEBIN PORT
exec dbus-run-session -- bash -euo pipefail -c '
fail() { echo "FAIL: $*"; exit 1; }
# Focus follows mouse, like many openbox setups: the case the covering panel exists for.
OBRC=$(mktemp); cat >"$OBRC" <<RC
<?xml version="1.0"?>
<openbox_config xmlns="http://openbox.org/3.4/rc">
  <focus><focusNew>yes</focusNew><followMouse>yes</followMouse><underMouse>no</underMouse><focusDelay>0</focusDelay><raiseOnFocus>no</raiseOnFocus></focus>
</openbox_config>
RC
openbox --config-file "$OBRC" >/dev/null 2>&1 & sleep 1
CFG=$(mktemp -d); mkdir -p "$CFG/config"
printf "version = 1\n\n[ui]\nonboarding_done = true\n" > "$CFG/config/settings.toml"
LOG=$(mktemp -d)
"$GAMEBIN" --item e2e/fixtures/rare-ring.txt --exit-after 60 >"$LOG/game" 2>&1 & FG=$!
for _ in $(seq 50); do grep -q READY "$LOG/game" && break; sleep 0.1; done
GAME=$(xdotool search --name "^Path of Exile 2$" | head -1)
wmctrl -i -r "$GAME" -b add,fullscreen; xdotool windowactivate --sync "$GAME"; sleep 0.5
# No real trade requests: these checks are about windows, not prices.
NO_NET=http://127.0.0.1:9
RKL_TRADE_BASE_URL=$NO_NET RKL_SCOUT_BASE_URL=$NO_NET RKL_NINJA_BASE_URL=$NO_NET \
RKL_TEST_HOOK=$PORT RKL_CONFIG_DIR=$CFG RKL_LOG=warn "$APP" >"$LOG/app" 2>&1 & PID=$!
trap "kill $FG $PID 2>/dev/null || true" EXIT
for _ in $(seq 100); do (exec 3<>/dev/tcp/127.0.0.1/$PORT) 2>/dev/null && break; sleep 0.1; done
hook() { exec 3<>/dev/tcp/127.0.0.1/$PORT; echo "$1" >&3; head -1 <&3; exec 3>&-; }
# Wails does not title frameless windows on Linux, so the overlay is named after the program
# ("rkl"); report it as "RKL overlay" like the titled settings window ("RKL settings").
name() { local n; n=$(xprop -id "$1" _NET_WM_NAME 2>/dev/null | sed "s/.*= //; s/\"//g"); [ "$n" = rkl ] && n="RKL overlay"; echo "$n"; }
overlay_id() { for w in $(xprop -root _NET_CLIENT_LIST | sed "s/.*# //; s/,//g"); do [ "$(name "$w")" = "RKL overlay" ] && { echo "$w"; return; }; done; }
top() { name "$(xprop -root _NET_CLIENT_LIST_STACKING | sed "s/.*# //; s/.*, //")"; }
active() { name "$(xprop -root _NET_ACTIVE_WINDOW | sed "s/.*# //")"; }
geo() { xwininfo -id "$1" | awk "/Absolute upper-left X/{x=\$NF}/Absolute upper-left Y/{y=\$NF}/Width/{w=\$NF}/Height/{h=\$NF}END{print x\",\"y\",\"w\",\"h}"; }
xdotool windowactivate --sync "$GAME"; sleep 0.3

hook "text $(python3 -c "import json;print(json.dumps(open(\"e2e/fixtures/rare-ring.txt\").read()))")" >/dev/null
sleep 0.8
[ "$(top)" = "RKL overlay" ] || fail "unpinned panel is not above the game (top: $(top))"
[ "$(active)" = "RKL overlay" ] || fail "unpinned panel is not focused (active: $(active))"
echo "ok: unpinned panel above the fullscreen game and focused"

xdotool windowactivate --sync "$GAME"; sleep 0.3
hook hide >/dev/null; xdotool windowactivate --sync "$GAME"; sleep 0.3
hook "tray price_check" >/dev/null; sleep 1.5
[ "$(top)" = "RKL overlay" ] || fail "pinned panel is not above the game (top: $(top))"
[ "$(active)" = "RKL overlay" ] || fail "pinned panel did not take focus (active: $(active))"
echo "ok: pinned panel above the game and focused"

for keys in ctrl+d ctrl+alt+d; do
  hook hide >/dev/null
  xdotool windowactivate --sync "$GAME"; sleep 0.3
  xdotool key --clearmodifiers "$keys"; sleep 1.5
  [ "$(top)" = "RKL overlay" ] || fail "$keys: panel is not above the game (top: $(top))"
  [ "$(active)" = "RKL overlay" ] || fail "$keys: panel is not focused (active: $(active))"
  echo "ok: $keys hotkey shows the panel above the game, focused"
  if [ "$keys" = ctrl+d ]; then
    xdotool key Escape; sleep 0.8
    [ "$(active)" = "Path of Exile 2" ] || fail "Esc did not return focus to the game (active: $(active))"
    echo "ok: Esc closes the unpinned panel and the game gets focus back"
  else
    xdotool key --clearmodifiers "$keys"; sleep 1
    [ "$(active)" = "Path of Exile 2" ] || fail "$keys again did not close the panel (active: $(active))"
    echo "ok: pressing $keys again closes the panel (toggle)"
  fi
done

# Unpinned panel covers the game; with focus-follows-mouse the pointer cannot hand focus back.
hook hide >/dev/null; xdotool windowactivate --sync "$GAME"; xdotool mousemove 300 300; sleep 0.3
xdotool key --clearmodifiers ctrl+d; sleep 1.5
OW=$(overlay_id)
[ -n "$OW" ] || fail "no overlay window"
[ "$(geo "$OW")" = "$(geo "$GAME")" ] || fail "unpinned panel $(geo "$OW") does not cover the game $(geo "$GAME")"
xdotool mousemove 1700 900; sleep 0.6; xdotool mousemove 1750 950; sleep 0.6
[ "$(active)" = "RKL overlay" ] || fail "moving the mouse handed focus away (active: $(active))"
echo "ok: unpinned panel covers the game and keeps focus while the mouse moves"
xdotool click 1; sleep 0.8
[ "$(xwininfo -id "$OW" | awk "/Map State/{print \$NF}")" != IsViewable ] || fail "backdrop click did not close the panel"
echo "ok: clicking the dimmed backdrop closes the panel"

# Pinned panel stays card-sized, so hovering the game focuses it (focus-follows-mouse).
xdotool windowactivate --sync "$GAME"; xdotool mousemove 300 300; sleep 0.3
xdotool key --clearmodifiers ctrl+alt+d; sleep 1.5
W=$(xwininfo -id "$OW" | awk "/Width:/{print \$NF}")
[ "$W" -lt 1000 ] || fail "pinned panel should be card-sized, width $W"
# Into the card, then back out over the game (openbox focuses on pointer enter).
xdotool mousemove --window "$OW" 100 100; sleep 0.6
xdotool mousemove 1700 900; sleep 0.8
[ "$(active)" = "Path of Exile 2" ] || fail "hovering the game did not focus it with a pinned panel (active: $(active))"
echo "ok: pinned panel is card-sized and the game can be hovered back into focus"
xdotool click 1; sleep 1
[ "$(xwininfo -id "$OW" | awk "/Map State/{print \$NF}")" = IsViewable ] || fail "clicking the game closed the pinned panel"
[ "$(top)" = "RKL overlay" ] || fail "clicking the game hid the pinned panel behind it (top: $(top))"
[ "$(active)" = "Path of Exile 2" ] || fail "the game lost focus after being clicked (active: $(active))"
echo "ok: clicking the game keeps the pinned panel on top, and the game focused"
# Proton/Wine holds an X pointer grab while the game is focused, so the WM never sees the
# pointer enter the panel. Hold one the same way; resting the cursor on the card must still
# focus it.
GRAB=$(mktemp --suffix=.py); cat >"$GRAB" <<PY
import ctypes, ctypes.util, sys, time
x = ctypes.CDLL(ctypes.util.find_library("X11"))
x.XOpenDisplay.restype = ctypes.c_void_p
d = x.XOpenDisplay(None)
x.XDefaultRootWindow.argtypes = [ctypes.c_void_p]; x.XDefaultRootWindow.restype = ctypes.c_ulong
x.XGrabPointer.argtypes = [ctypes.c_void_p, ctypes.c_ulong, ctypes.c_int, ctypes.c_uint, ctypes.c_int, ctypes.c_int, ctypes.c_ulong, ctypes.c_ulong, ctypes.c_ulong]
x.XFlush.argtypes = [ctypes.c_void_p]
print("grab", x.XGrabPointer(d, x.XDefaultRootWindow(d), 0, 4|8|64, 1, 1, 0, 0, 0), flush=True)
x.XFlush(d)
time.sleep(float(sys.argv[1]))
PY
xdotool windowactivate --sync "$GAME"; xdotool mousemove 1700 900; sleep 0.3
python3 -I "$GRAB" 6 >"$LOG/grab" 2>&1 & GP=$!
sleep 0.5; grep -q "grab 0" "$LOG/grab" || fail "test helper could not grab the pointer: $(cat "$LOG/grab")"
[ "$(active)" = "Path of Exile 2" ] || fail "game should be focused before hovering (active: $(active))"
xdotool mousemove --window "$OW" 100 100; sleep 0.8
[ "$(active)" = "RKL overlay" ] || fail "resting the cursor on the pinned panel under a pointer grab did not focus it (active: $(active))"
echo "ok: hovering the pinned panel focuses it even while the game holds the pointer"
kill $GP 2>/dev/null || true; wait $GP 2>/dev/null || true
xprop -id "$OW" _COMPTON_SHADOW | grep -q "= 0" || fail "the panel should ask compositors for no drop shadow ($(xprop -id "$OW" _COMPTON_SHADOW))"
echo "ok: the panel opts out of compositor drop shadows"
python3 -I - "$OW" <<'PY' || fail "the pinned panel window is not shaped with rounded corners"
import ctypes, ctypes.util, sys
x = ctypes.CDLL(ctypes.util.find_library("X11")); xe = ctypes.CDLL(ctypes.util.find_library("Xext"))
x.XOpenDisplay.restype = ctypes.c_void_p; d = x.XOpenDisplay(None)
class R(ctypes.Structure): _fields_ = [("x", ctypes.c_short), ("y", ctypes.c_short), ("w", ctypes.c_ushort), ("h", ctypes.c_ushort)]
xe.XShapeGetRectangles.restype = ctypes.POINTER(R)
xe.XShapeGetRectangles.argtypes = [ctypes.c_void_p, ctypes.c_ulong, ctypes.c_int, ctypes.POINTER(ctypes.c_int), ctypes.POINTER(ctypes.c_int)]
n, o = ctypes.c_int(), ctypes.c_int()
rects = xe.XShapeGetRectangles(d, int(sys.argv[1]), 0, ctypes.byref(n), ctypes.byref(o))
spans = [(rects[i].x, rects[i].y, rects[i].w, rects[i].h) for i in range(n.value)]
corner = any(x0 <= 0 < x0 + w and y0 <= 0 < y0 + h for x0, y0, w, h in spans)
sys.exit(0 if n.value > 1 and not corner else 1)
PY
echo "ok: the pinned panel window has rounded corners"
pos() { xwininfo -id "$1" | awk "/Absolute upper-left X/{x=\$NF}/Absolute upper-left Y/{y=\$NF}END{print x\" \"y}"; }
drag() { xdotool mousemove "$1" "$2"; sleep 0.3; xdotool mousedown 1; sleep 0.1
  for _ in 1 2 3 4 5; do xdotool mousemove_relative -- 40 20; sleep 0.03; done; xdotool mouseup 1; sleep 0.5; }
read PX PY < <(pos "$OW")
drag $((PX + 150)) $((PY + 20))
read QX QY < <(pos "$OW")
[ $((QX - PX)) -eq 200 ] && [ $((QY - PY)) -eq 100 ] || fail "dragging the pinned header moved it by ($((QX - PX)),$((QY - PY))), expected (200,100)"
echo "ok: dragging the pinned panel header moves it"
hook hide >/dev/null
xdotool windowactivate --sync "$GAME"; xdotool mousemove 300 300; sleep 0.3
xdotool key --clearmodifiers ctrl+d; sleep 1.5
card() { hook state | python3 -c "import json,sys;c=json.load(sys.stdin)[\"overlay_card\"][0];print(c[0],c[1])"; }
read CX CY < <(card)
drag $((CX + 150)) $((CY + 20))
read DX DY < <(card)
[ $((DX - CX)) -eq 200 ] && [ $((DY - CY)) -eq 100 ] || fail "dragging the unpinned card moved it by ($((DX - CX)),$((DY - CY))), expected (200,100)"
echo "ok: dragging the unpinned card moves it in both directions"
hook hide >/dev/null
hook hide >/dev/null

hook "tray open_settings" >/dev/null; sleep 0.8
[ "$(top)" = "RKL settings" ] || fail "settings not on top (top: $(top))"
[ "$(active)" = "RKL settings" ] || fail "settings not focused (active: $(active))"
SW=$(xdotool search --name "^RKL settings$" | head -1)
[ "$(geo "$SW")" = "$(geo "$GAME")" ] || fail "settings $(geo "$SW") does not cover the game $(geo "$GAME")"
echo "ok: settings overlay covers the game ($(geo "$SW")) and is focused"
xdotool key --clearmodifiers ctrl+space; sleep 1
[ "$(xwininfo -id "$SW" | awk "/Map State/{print \$NF}")" != IsViewable ] || fail "Ctrl+Space did not close the settings overlay"
echo "ok: Ctrl+Space closes the settings overlay (toggle)"
xdotool windowactivate --sync "$GAME"; sleep 0.3
xdotool key --clearmodifiers ctrl+space; sleep 1
[ "$(top)" = "RKL settings" ] || fail "Ctrl+Space did not open the settings overlay (top: $(top))"
echo "ok: Ctrl+Space opens the settings overlay over the game"
hook quit >/dev/null || true
echo "PASS: stacking over a fullscreen game"
'
