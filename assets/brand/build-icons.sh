#!/usr/bin/env bash
# Regenerate the brand SVGs and the app icons rendered from them (see README.md).
# Needs: python3, rsvg-convert (librsvg), ImageMagick 7 (`magick`) and Go, which runs Wails'
# icon generator pinned in tools/go.mod for the .icns.
#
#   ./assets/brand/build-icons.sh            write into the repository
#   ./assets/brand/build-icons.sh <dir>      write the same tree under <dir> (a dry run)
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
out=${1:-$root}
mkdir -p "$out"
out=$(cd "$out" && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

svg=$out/assets/brand
python3 -I "$here/generate.py" "$svg"

# render <svg-name> <size> <out.png>
render() {
	mkdir -p "$(dirname "$3")"
	rsvg-convert -w "$2" -h "$2" "$svg/$1.svg" -o "$3"
}

# Linux AppImage icon and the window icon.
render rkl-app-icon 256 "$out/build/appicon.png"
render rkl-app-icon 512 "$out/cmd/rkl/icons/icon.png"

# Windows .ico with the dedicated 16/24/32 masters.
render rkl-app-icon-16 16 "$tmp/16.png"
render rkl-app-icon-16 24 "$tmp/24.png"
render rkl-app-icon-32 32 "$tmp/32.png"
for s in 48 64 128 256; do render rkl-app-icon "$s" "$tmp/$s.png"; done
mkdir -p "$out/build/windows"
magick "$tmp"/{16,24,32,48,64,128,256}.png "$out/build/windows/icon.ico"

# macOS .icns from the tile with Apple's transparent margin (824/1024). Wails' generator also
# writes a .ico, which is not wanted here (the one above has the small masters).
render rkl-app-icon-macos 1024 "$tmp/macos.png"
mkdir -p "$out/build/darwin"
go -C "$root" tool -modfile=tools/go.mod wails3 generate icons -input "$tmp/macos.png" \
	-macfilename "$out/build/darwin/icons.icns" -windowsfilename "$tmp/unused.ico" >/dev/null

# Tray: colour glyph with a dark outline (Windows/Linux), black template glyph (macOS menu bar, 22pt @2x).
render rkl-glyph-16 64 "$out/cmd/rkl/icons/tray.png"
render rkl-glyph-mono 44 "$out/cmd/rkl/icons/tray-template.png"

# In-app header mark (web UI).
mkdir -p "$out/web/src/assets"
cp "$svg/rkl-glyph.svg" "$out/web/src/assets/rkl-glyph.svg"

echo "brand: SVGs and icons written under $out"
