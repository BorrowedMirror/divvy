#!/usr/bin/env bash
# Let rkl's overlay receive clicks and keys over Path of Exile 2 under Proton/Wine (X11).
#
# In fullscreen Wine turns the game's cursor clipping (ClipCursor) into an X pointer grab, so every
# click goes to the game even on windows drawn above it. This sets the Wine X11 driver options
# GrabPointer=N and GrabFullscreen=N in the game's Proton prefix, and can restore the previous
# values exactly.
#
#   proton-mouse-grab.sh status   [PREFIX]   show current values
#   proton-mouse-grab.sh disable  [PREFIX]   set both to N (remembers previous values)
#   proton-mouse-grab.sh restore  [PREFIX]   put back the remembered values
#
# PREFIX defaults to Steam's compatdata/2694490/pfx (PoE2) in any Steam library. Quit the game
# first: Wine rewrites user.reg on exit. The change applies the next time the game starts.
set -euo pipefail

APPID=${RKL_POE2_APPID:-2694490}
cmd=${1:-status}
prefix=${2:-}

find_prefix() {
  local roots=("$HOME/.local/share/Steam" "$HOME/.steam/steam" "$HOME/.var/app/com.valvesoftware.Steam/.local/share/Steam")
  local libs=()
  for r in "${roots[@]}"; do
    [ -f "$r/steamapps/libraryfolders.vdf" ] || continue
    libs+=("$r")
    while IFS= read -r p; do libs+=("$p"); done < <(sed -n 's/^[[:space:]]*"path"[[:space:]]*"\(.*\)"/\1/p' "$r/steamapps/libraryfolders.vdf")
  done
  for l in "${libs[@]}"; do
    if [ -d "$l/steamapps/compatdata/$APPID/pfx" ]; then
      echo "$l/steamapps/compatdata/$APPID/pfx"
      return 0
    fi
  done
  return 1
}

if [ -z "$prefix" ]; then
  prefix=$(find_prefix) || { echo "error: no Proton prefix for app $APPID found; pass it explicitly" >&2; exit 1; }
fi
reg="$prefix/user.reg"
state="$prefix/.rkl-mouse-grab.state"
[ -f "$reg" ] || { echo "error: $reg not found" >&2; exit 1; }

if [ "$cmd" != status ] && { pgrep -x wineserver || pgrep -f "PathOfExile[A-Za-z_]*\.exe"; } >/dev/null 2>&1; then
  echo "error: Path of Exile 2 (or its Wine server) is running; quit it first" >&2
  exit 1
fi

python3 -I - "$cmd" "$reg" "$state" <<'PY'
import json, os, re, shutil, sys, time

cmd, reg, state = sys.argv[1:4]
SECTION = r"[Software\\Wine\\X11 Driver]"
KEYS = ("GrabPointer", "GrabFullscreen")

with open(reg, encoding="utf-8", errors="surrogateescape", newline="") as f:
    lines = f.read().split("\n")

def find_section():
    for i, line in enumerate(lines):
        if line.startswith(SECTION + " ") or line == SECTION:
            j = i + 1
            while j < len(lines) and not lines[j].startswith("["):
                j += 1
            return i, j
    return None

def current():
    sec = find_section()
    vals = {k: None for k in KEYS}
    if sec:
        for line in lines[sec[0] + 1 : sec[1]]:
            m = re.match(r'^"(\w+)"="(.*)"$', line)
            if m and m.group(1) in KEYS:
                vals[m.group(1)] = m.group(2)
    return vals

def write(new_vals):
    sec = find_section()
    if sec is None:
        stamp = int(time.time())
        lines.extend([f"{SECTION} {stamp}", f"#time={stamp:x}", ""])
        sec = find_section()
    start, end = sec
    body = [l for l in lines[start + 1 : end] if not re.match(r'^"(%s)"=' % "|".join(KEYS), l)]
    # keep the trailing blank line that separates sections
    while body and body[-1] == "":
        body.pop()
    for k in KEYS:
        if new_vals.get(k) is not None:
            body.append(f'"{k}"="{new_vals[k]}"')
    body.append("")
    lines[start + 1 : end] = body
    backup = reg + ".rkl-backup"
    if not os.path.exists(backup):
        shutil.copy2(reg, backup)
    tmp = reg + ".rkl-tmp"
    with open(tmp, "w", encoding="utf-8", errors="surrogateescape", newline="") as f:
        f.write("\n".join(lines))
    os.replace(tmp, reg)

vals = current()
if cmd == "status":
    for k in KEYS:
        print(f"{k}: {vals[k] if vals[k] is not None else '(unset, Wine default)'}")
    print("saved previous values:", "yes" if os.path.exists(state) else "no")
elif cmd == "disable":
    if not os.path.exists(state):
        with open(state, "w") as f:
            json.dump(vals, f)
    write({k: "N" for k in KEYS})
    print("GrabPointer=N, GrabFullscreen=N set; previous values saved to", state)
elif cmd == "restore":
    if not os.path.exists(state):
        sys.exit("error: nothing to restore (no saved state); values are: %s" % vals)
    with open(state) as f:
        old = json.load(f)
    write(old)
    os.remove(state)
    print("restored:", ", ".join(f"{k}={old[k] if old[k] is not None else '(unset)'}" for k in KEYS))
else:
    sys.exit(f"usage: {os.path.basename(sys.argv[0])} status|disable|restore [PREFIX]")
PY
