#!/usr/bin/env bash
# Rasterize icon.svg into a macOS icon.icns. Needs rsvg-convert + iconutil.
set -euo pipefail
cd "$(dirname "$0")"

SVG="icon.svg"
SET="icon.iconset"
rm -rf "$SET"; mkdir "$SET"

gen() { rsvg-convert -w "$1" -h "$1" "$SVG" -o "$SET/$2"; }
gen 16   icon_16x16.png
gen 32   icon_16x16@2x.png
gen 32   icon_32x32.png
gen 64   icon_32x32@2x.png
gen 128  icon_128x128.png
gen 256  icon_128x128@2x.png
gen 256  icon_256x256.png
gen 512  icon_256x256@2x.png
gen 512  icon_512x512.png
gen 1024 icon_512x512@2x.png

iconutil -c icns "$SET" -o icon.icns
rm -rf "$SET"
echo "built $(pwd)/icon.icns"
