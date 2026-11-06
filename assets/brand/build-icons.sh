#!/usr/bin/env bash
# Regenerate the brand SVGs and the Tauri icon set from them.
# Needs: python3, rsvg-convert (librsvg), ImageMagick 7 (`magick`), and `cargo tauri` (for .icns).
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
icons=$here/../../apps/desktop/src-tauri/icons
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

python3 -I "$here/generate.py" "$here"

# render <svg-name> <size> <out.png>
render() { rsvg-convert -w "$2" -h "$2" "$here/$1.svg" -o "$3"; }

# Bundle PNGs. 32px uses the flat small-size master; larger sizes use the full tile.
render rkl-app-icon-32 32 "$icons/32x32.png"
render rkl-app-icon 128 "$icons/128x128.png"
render rkl-app-icon 256 "$icons/128x128@2x.png"
render rkl-app-icon 512 "$icons/icon.png"

# Windows .ico with the dedicated 16/24/32 masters.
render rkl-app-icon-16 16 "$tmp/16.png"
render rkl-app-icon-16 24 "$tmp/24.png"
render rkl-app-icon-32 32 "$tmp/32.png"
for s in 48 64 128 256; do render rkl-app-icon "$s" "$tmp/$s.png"; done
magick "$tmp"/{16,24,32,48,64,128,256}.png "$icons/icon.ico"

# macOS .icns from the tile with Apple's transparent margin (824/1024).
render rkl-app-icon-macos 1024 "$tmp/macos.png"
cargo tauri icon "$tmp/macos.png" --output "$tmp/tauri" >/dev/null 2>&1
cp "$tmp/tauri/icon.icns" "$icons/icon.icns"

# Tray: colour glyph with a dark outline (Windows/Linux), black template glyph (macOS menu bar, 22pt @2x).
render rkl-glyph-16 64 "$icons/tray.png"
render rkl-glyph-mono 44 "$icons/tray-template.png"

# In-app header mark (web UI).
cp "$here/rkl-glyph.svg" "$here/../../web/src/assets/rkl-glyph.svg"

echo "brand: icons written to $icons"
