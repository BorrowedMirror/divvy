#!/usr/bin/env bash
# Render the app icons from divine-orb.png (see README.md).
# Needs: ffmpeg, and Go, which runs Wails' icon generator pinned in tools/go.mod for the .ico and .icns.
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

src=$here/divine-orb.png

# render <size> <art-size> <out.png>: the art scaled to art-size, centred on a transparent square.
render() {
	mkdir -p "$(dirname "$3")"
	ffmpeg -loglevel error -y -i "$src" -vf \
		"format=rgba,scale=$2:$2:flags=lanczos,pad=$1:$1:(ow-iw)/2:(oh-ih)/2:color=black@0" \
		-frames:v 1 "$3"
}

# Linux AppImage icon and the window icon.
render 256 256 "$out/build/appicon.png"
render 512 512 "$out/cmd/divvy/icons/icon.png"

# Windows .ico and macOS .icns from one 1024 px render. The .icns uses Apple's transparent
# margin (824/1024); the .ico takes the full-bleed render.
render 1024 1024 "$tmp/full.png"
render 1024 824 "$tmp/macos.png"
mkdir -p "$out/build/windows" "$out/build/darwin"
CGO_ENABLED=0 go -C "$root" tool -modfile=tools/go.mod wails3 generate icons -input "$tmp/full.png" \
	-windowsfilename "$out/build/windows/icon.ico" -macfilename "$tmp/unused.icns" >/dev/null
CGO_ENABLED=0 go -C "$root" tool -modfile=tools/go.mod wails3 generate icons -input "$tmp/macos.png" \
	-macfilename "$out/build/darwin/icons.icns" -windowsfilename "$tmp/unused.ico" >/dev/null

# Tray: the colour art (Windows/Linux), and a black silhouette as the macOS menu-bar template
# (22 pt @2x). macOS uses only the template's alpha.
render 64 64 "$out/cmd/divvy/icons/tray.png"
ffmpeg -loglevel error -y -i "$src" -vf \
	"format=rgba,scale=44:44:flags=lanczos,colorchannelmixer=rr=0:gg=0:bb=0" \
	-frames:v 1 "$out/cmd/divvy/icons/tray-template.png"

# In-app header mark (web UI).
mkdir -p "$out/web/src/assets"
cp "$src" "$out/web/src/assets/divvy-mark.png"

echo "brand: icons written under $out"
