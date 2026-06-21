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

# Sign the app. With a real Developer ID (MACOS_SIGN_IDENTITY set) use hardened
# runtime + timestamp so the app can be notarized for a warning-free launch.
# Otherwise ad-hoc sign (no Apple account needed): enough to run cleanly on
# Apple Silicon (no "damaged" error) — the user just right-click → Open once.
if [ -n "${MACOS_SIGN_IDENTITY:-}" ]; then
  echo "Signing with Developer ID: $MACOS_SIGN_IDENTITY"
  codesign --force --deep --options runtime --timestamp --sign "$MACOS_SIGN_IDENTITY" "$APP"
else
  echo "Ad-hoc signing (no Developer ID set)"
  codesign --force --deep --sign - "$APP" || echo "warning: codesign failed"
fi

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
