#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION="${APP_VERSION:?APP_VERSION is required}"
NFPM_VERSION="${NFPM_VERSION:-$VERSION}"
BIN="$ROOT/build/bin/ding-ssh"
DIST="$ROOT/dist"

if [[ ! -f "$BIN" ]]; then
  echo "missing $BIN" >&2
  exit 1
fi

mkdir -p "$DIST"
chmod +x "$BIN"

TAR="$DIST/ding-ssh-${VERSION}-linux-amd64.tar.gz"
STAGE="$ROOT/build/linux-stage"
rm -rf "$STAGE"
mkdir -p "$STAGE"
cp "$BIN" "$STAGE/ding-ssh"
tar -C "$STAGE" -czf "$TAR" ding-ssh
rm -rf "$STAGE"

if command -v nfpm >/dev/null 2>&1; then
  export NFPM_VERSION
  nfpm package --config "$ROOT/build/linux/nfpm.yaml" --packager deb --target "$DIST/"
  # nfpm names the file from name_version_arch.deb; rename to include our artifact version if needed
  shopt -s nullglob
  for deb in "$DIST"/ding-ssh_*.deb; do
    dest="$DIST/ding-ssh_${VERSION}_amd64.deb"
    if [[ "$deb" != "$dest" ]]; then
      mv "$deb" "$dest"
    fi
  done
else
  echo "nfpm not found; skipping .deb" >&2
fi

APPDIR="$ROOT/build/AppDir"
rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin" "$APPDIR/usr/share/applications" "$APPDIR/usr/share/icons/hicolor/256x256/apps"
cp "$BIN" "$APPDIR/usr/bin/ding-ssh"
cp "$ROOT/build/linux/ding-ssh.desktop" "$APPDIR/ding-ssh.desktop"
cp "$ROOT/build/linux/ding-ssh.desktop" "$APPDIR/usr/share/applications/ding-ssh.desktop"
cp "$ROOT/build/appicon.png" "$APPDIR/ding-ssh.png"
cp "$ROOT/build/appicon.png" "$APPDIR/.DirIcon"
cp "$ROOT/build/appicon.png" "$APPDIR/usr/share/icons/hicolor/256x256/apps/ding-ssh.png"
cat > "$APPDIR/AppRun" << 'EOF'
#!/bin/bash
HERE="$(dirname "$(readlink -f "$0")")"
exec "$HERE/usr/bin/ding-ssh" "$@"
EOF
chmod +x "$APPDIR/AppRun" "$APPDIR/usr/bin/ding-ssh"

APPIMAGE_TOOL="${APPIMAGE_TOOL:-}"
if [[ -z "$APPIMAGE_TOOL" ]]; then
  if command -v appimagetool >/dev/null 2>&1; then
    APPIMAGE_TOOL="$(command -v appimagetool)"
  fi
fi

if [[ -n "$APPIMAGE_TOOL" ]]; then
  export APPIMAGE_EXTRACT_AND_RUN=1
  ARCH=x86_64 "$APPIMAGE_TOOL" "$APPDIR" "$DIST/ding-ssh-${VERSION}-linux-amd64.AppImage"
else
  echo "appimagetool not found; skipping AppImage" >&2
fi

echo "Linux packages:"
ls -lh "$DIST"
