#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${APP_VERSION:?APP_VERSION is required}"
APP="$ROOT/build/bin/ding-ssh.app"
DIST="$ROOT/dist"

if [[ ! -d "$APP" ]]; then
  echo "missing $APP" >&2
  exit 1
fi

# Detect the actual binary architecture rather than the build host architecture.
BINARY_ARCH="$(lipo -archs "$APP/Contents/MacOS/ding-ssh")"
case "$BINARY_ARCH" in
  arm64) DETECTED_ARCH=arm64 ;;
  x86_64) DETECTED_ARCH=amd64 ;;
  *) echo "expected a single arm64 or x86_64 binary, got: $BINARY_ARCH" >&2; exit 1 ;;
esac
ARCH="${APP_ARCH:-$DETECTED_ARCH}"
if [[ "$ARCH" != "$DETECTED_ARCH" ]]; then
  echo "APP_ARCH=$ARCH does not match binary architecture $DETECTED_ARCH" >&2
  exit 1
fi

mkdir -p "$DIST"
chmod +x "$APP/Contents/MacOS/"* || true

ZIP="$DIST/ding-ssh-${VERSION}-macos-${ARCH}.app.zip"
rm -f "$ZIP"
ditto -c -k --keepParent "$APP" "$ZIP"

STAGE="$ROOT/build/dmg-stage"
rm -rf "$STAGE"
mkdir -p "$STAGE"
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"

DMG="$DIST/ding-ssh-${VERSION}-macos-${ARCH}.dmg"
rm -f "$DMG"
hdiutil create -volname "ding-ssh" -srcfolder "$STAGE" -ov -format UDZO -imagekey zlib-level=9 "$DMG"
rm -rf "$STAGE"

echo "macOS packages:"
ls -lh "$ZIP" "$DMG"
