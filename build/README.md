# Desktop build

The root Taskfile drives Wails v3.1.0 and writes native artifacts to `build/bin`.

- Run `wails3 build` for the current platform. Use `wails3 task build GOOS=darwin ARCH=amd64` on macOS to build for Intel.
- Run `npm --prefix frontend ci` once before `wails3 dev`. The dev watcher uses `build/config.yml`, with Vite on loopback port 9245.
- On Windows, run `wails3 task package GOOS=windows ARCH=amd64` with NSIS installed. The installer embeds the WebView2 bootstrapper and downloads the runtime when needed.
- Linux builds set `gtk3` to retain the GTK3 / WebKitGTK 4.1 runtime contract for existing installations and the branch updater.
- The macOS bundle keeps `com.funkypantssa.ding-ssh` and targets macOS 12. `build/darwin/Info.plist` and `build/windows/info.json` carry native version metadata. CI stamps these with `scripts/set-version.py`.
- The packaging scripts under `scripts` consume the native artifacts to produce the existing DMG, ZIP, NSIS, DEB, tarball and AppImage distributions. Branch builds also use `scripts/update-manifest.py` to hash the updater assets.
