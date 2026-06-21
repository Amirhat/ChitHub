#!/usr/bin/env bash
# Assemble ChitHub.app from the GoReleaser darwin universal binary and wrap it
# in a .dmg. Run from the repo root, after `goreleaser release`.
#   ./packaging/make-dmg.sh v0.1.0
set -euo pipefail

TAG="${1:-dev}"
VER="${TAG#v}"

# Locate the darwin binary GoReleaser produced (prefer the universal one).
BIN="$(find dist -type f -name chithub -path '*darwin*all*' | head -1 || true)"
[ -n "$BIN" ] || BIN="$(find dist -type f -name chithub -path '*darwin*' | head -1 || true)"
if [ -z "$BIN" ]; then
  echo "error: no darwin chithub binary found under dist/" >&2
  exit 1
fi
echo "Using binary: $BIN"

APP="dist/ChitHub.app"
rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$BIN" "$APP/Contents/MacOS/chithub"
chmod +x "$APP/Contents/MacOS/chithub"
sed "s/__VERSION__/${VER}/g" packaging/Info.plist > "$APP/Contents/Info.plist"
if [ -f packaging/icon.icns ]; then
  cp packaging/icon.icns "$APP/Contents/Resources/icon.icns"
fi

# Sign the app. With a real Developer ID (MACOS_SIGN_IDENTITY set), use it so the
# app can also be notarized for a warning-free launch; otherwise ad-hoc sign,
# which is enough for the app to run cleanly on Apple Silicon (no "damaged"
# error — the user still right-click → Open once, as it's unsigned by Apple).
SIGN_ID="${MACOS_SIGN_IDENTITY:--}"
echo "Signing with identity: $SIGN_ID"
codesign --force --deep --options runtime --sign "$SIGN_ID" "$APP" 2>/dev/null \
  || codesign --force --deep --sign - "$APP" \
  || echo "warning: codesign failed"

DMG="dist/ChitHub-${TAG}.dmg"
rm -f "$DMG"

if command -v create-dmg >/dev/null 2>&1; then
  create-dmg \
    --volname "ChitHub" \
    --window-size 540 380 \
    --icon-size 110 \
    --icon "ChitHub.app" 150 180 \
    --app-drop-link 390 180 \
    "$DMG" "$APP"
else
  # Fallback: a plain compressed image.
  hdiutil create -volname "ChitHub" -srcfolder "$APP" -ov -format UDZO "$DMG"
fi

echo "Built $DMG"
