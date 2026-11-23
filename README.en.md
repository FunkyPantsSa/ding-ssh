[简体中文](README.md) | English

<div align="center">
  <img src="frontend/icon/ding.png" width="280" alt="ding-ssh cat terminal icon">
  <h1>ding-ssh</h1>
  <p><strong>SSH terminals, SFTP, port forwarding and local shells in one desktop workspace — with a local control plane scripts and AI clients can drive.</strong></p>
</div>

[![CI](https://github.com/FunkyPantsSa/ding-ssh/actions/workflows/ci.yml/badge.svg?branch=dev)](https://github.com/FunkyPantsSa/ding-ssh/actions/workflows/ci.yml) [![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

ding-ssh is a local-first desktop SSH workspace. The terminal is the center: SFTP, tunnels, local shells and split panes share one tab system. When debug mode is enabled, the app also exposes an HTTP + SSE + MCP control plane on the loopback interface so scripts and AI clients can inspect state, operate terminals, run tasks and modify remote files.

| Item | Details |
| --- | --- |
| Platforms | Windows x64 · macOS (Apple Silicon / Intel builds) · Linux x64 |
| Stack | Go 1.26 + Wails v3.1 · Vue 3.5 + Pinia 4 + Tailwind v4 + Vite 7 + xterm.js 6 |
| Storage | Local SQLite (WAL) + system keychain; no account, cloud sync or telemetry |
| Control plane | Off by default, loopback only; the current `dev` branch exposes **124 MCP tools** (always check `tools/list` / `GET /v1/tools`) |
| Branches | `dev` for active development, `main` for integration; released version 1.2.1, unreleased changes in [CHANGELOG](CHANGELOG.md) |
| License | [MIT License](LICENSE) |
| Documentation | [MCP tasks and files](docs/MCP任务与文件接口.md) · [MCP implementation and validation](docs/MCP改造实施与验证记录.md) · [Logging](docs/logging.md) |

Choose **Settings → General → Language** for Simplified Chinese or English. Chinese is the default and the selection is saved locally. Switching updates the interface in place without reloading the page or closing terminal sessions. Pages, help text, connection and transfer messages, and native file dialog titles support both languages; user content and remote terminal output stay unchanged.

## Contents

1. [Purpose](#1-purpose)
2. [Features](#2-features)
3. [Quick start](#3-quick-start)
4. [Interface](#4-interface)
5. [Keyboard shortcuts](#5-keyboard-shortcuts)
6. [Build from source](#6-build-from-source)
7. [Code map](#7-code-map)
8. [Data and security](#8-data-and-security)
9. [Debug mode and AI control](#9-debug-mode-and-ai-control)
10. [Logs and diagnostics](#10-logs-and-diagnostics)
11. [Known limitations](#11-known-limitations)
12. [CI and releases](#12-ci-and-releases)
13. [Interface and design system](#13-interface-and-design-system)
14. [Documentation and licensing](#14-documentation-and-licensing)

---

## 1. Purpose

Routine operations often mean switching between tools to connect, configure hosts, transfer files, create tunnels and monitor load. ding-ssh puts these tasks in one terminal workspace that can also be automated, without leaving the local security boundary to the caller's good behaviour:

1. **Local first.** Servers, credentials, command history and settings live in the local configuration directory. Sensitive fields use AES-256-GCM; keys use the system keychain or your master password. No account or server component is required.
2. **Terminal centered.** SFTP and shell directories synchronize in both directions. Tunnels, local shells and split panes share the tab system.
3. **Observable and controllable, with explicit authorization.** Debug mode adds local HTTP and MCP endpoints for layout inspection, terminal buffer reads, command input, output waits, independent execution, remote file edits and diagnostic export. Reads, connections, execution, configuration, credentials and UI are authorized separately. Per-client tokens can restrict targets and paths. Configuration, file and lifecycle changes also require a **native system approval outside the WebView**; the resulting execution credential is one-time, bound to the complete arguments and expires after 120 seconds.

The application is **not** a bastion host with server-side auditing, a fleet configuration tool, or a general-purpose SSH proxy. The debug control plane is not a remote-access solution either: it has no TLS or OAuth. Use SSH port forwarding for remote access.

## 2. Features

| Module | Features | Implementation |
| --- | --- | --- |
| **Terminal** | Multiple tabs; horizontal / vertical splits; drag tabs to split; in-terminal search | xterm.js + WebGL (the CSS `zoom` bitmap issue is fixed, so every supported scale works; falls back to Canvas when unsupported or when the context is lost); 5,000 scrollback lines; `Ctrl/Cmd+F` search |
| **Connections** | Password, private key, SSH Agent or automatic authentication; connection progress; automatic reconnect; host-key trust and change detection | Five-stage progress (DNS/direct → TCP → authentication → PTY → ready) with logs; 15s keepalive; 2/5/10/20/30s reconnect backoff; reconnect preserves the screen |
| **Groups** | Folder-style nested groups (for example `Production/APAC/Hangzhou`); selecting a parent includes descendants; drag servers into groups; search and breadcrumbs | The path is the identity; renaming cascades; legacy groups are normalized idempotently at startup |
| **Jump hosts and bastions** | Up to five saved jump hosts; JumpServer bastion mode | Each hop authenticates and verifies separately; terminal, SFTP, metrics and all tunnel modes share the connection chain and never silently fall back to a direct connection |
| **SSH Config import** | Preview, skip, duplicate, overwrite and field diffs | Import creates a read-only snapshot and never rewrites the source file; common Host rules and Include are supported, but `ProxyCommand` / `LocalCommand` and other shell directives are not executed |
| **SFTP** | Browse, upload (drag / multi-select), download, rename, delete, create folders, edit the path, download folders; cancellable transfers | SWR directory cache (`sync.Map`, asynchronous revalidation, incremental events); concurrency = CPU cores × 2; token-bucket rate limiting, unlimited by default; rz/sz through Zmodem |
| **Directory sync** | Shell `cd` follows into SFTP; SFTP navigation follows into the terminal | OSC 7 preferred, prompt parsing as fallback; both directions are independently configurable |
| **Tunnels** | Local, remote, dynamic SOCKS5 and reverse SOCKS5; start, stop, restart, edit, delete | State updates through events; health is available over MCP |
| **Local terminals** | Open a local shell without SSH | PowerShell / cmd on Windows; zsh / bash on macOS; default shell on Linux, through go-pty |
| **Completion and command palette** | Suggestions while typing with local-history ranking; `Ctrl/Cmd+K` global command palette | Trie + FZF-style scoring; configurable navigation shortcut; the palette combines recent commands, servers, actions and navigation |
| **Metrics** | Remote CPU, memory, disk and network in the status bar or side panel; per-session single / real-time mode | Per-session collectors; real-time refresh is off by default and pauses when idle |
| **Appearance** | 10 preset palettes plus custom ANSI 16 colors, backgrounds, blur, fonts and system light/dark mode; 80%–150% scaling | HCT / Tonal Spot derives semantic colors from a seed; Solid / Soft Glass / Clear Glass materials; tab-bar placement and sidebar section order are configurable; panels are resizable |
| **Output colors** | Off / Semantic / Vivid terminal coloring | The frontend Colorizer injects SGR only; ANSI/OSC/CSI sequences and prompts without newlines pass through immediately |
| **Localization** | Simplified Chinese / English, switched in place | Interface, help text, system prompts and native dialog titles; user content and remote output are untouched |
| **Data and updates** | Password-encrypted `.dingpack` import/export, master password, device migration; `main` / `dev` update channels | Up to 500 servers per package; SHA-256 verification; install immediately or on exit |
| **Automation** | Local HTTP API, SSE events and MCP (including a stdio proxy) | Capabilities, client scopes, native approval and persistent audit; see [section 9](#9-debug-mode-and-ai-control) |

## 3. Quick start

### 3.1 Get a build

- GitHub Releases publish macOS `.dmg` / `.app.zip`, Windows `setup.exe` / `.zip`, and Linux `.AppImage` / `.deb` / `.tar.gz`, with `SHA256SUMS.txt`.
- CI builds `main` / `dev` on every successful push and uploads artifacts such as `ding-ssh-dev-windows-amd64-<sha>`. Branch builds also refresh `update.json` in the `main-latest` / `dev-latest` prereleases.
- macOS packages are ad-hoc signed and not notarized: on first launch, right-click in Finder and choose **Open**.

The Windows installer defaults to `C:\Program Files\FunkyPantsSa\ding-ssh`, lets you choose another directory, and registers an uninstall entry in Windows **Installed apps**. In-app updates install directly when the directory is writable; otherwise a separate helper requests administrator permission through UAC to replace the executable, and an unelevated helper restarts the app so it does not inherit administrator rights. Cancelling UAC keeps the old version. Configuration stays in `%APPDATA%\ding-ssh`. Older builds need one manual installation of a build that contains this update helper, or an update performed by running the old app as administrator.

### 3.2 First launch

A master-password prompt appears if enabled; otherwise the workspace opens directly. **Settings → General** controls:

- start at login (off by default; uses the current user's LaunchAgent, Run registry entry or XDG autostart, with no administrator permission);
- reconnect servers that were open when the app last closed;
- confirmation before closing tabs or quitting;
- smooth transitions (off by default; follows the system reduced-motion preference).

### 3.3 Add a server

Open the session area, create a server, and enter host, port, user and authentication (password, pasted key, key file, SSH Agent or automatic). Groups support folder-style nesting: expand the tree, select a parent to include all descendants, drag servers between groups, create child groups from the context menu, and rename a parent to cascade the change.

The server context menu can copy the IP or duplicate the configuration as `original.new` and open it for editing. **Import SSH Config** previews, skips, duplicates, overwrites and reports field differences; the import is saved as a snapshot and does not rewrite `~/.ssh/config`.

The first connection shows the host type and SHA256 fingerprint; verify it through a trusted channel and choose **Trust and connect**. A changed host key aborts the connection until explicitly confirmed; revoked records are always rejected. Application trust records are stored separately, and the user's `~/.ssh/known_hosts` plus any extra trust source configured by the server stay read-only.

### 3.4 Open a terminal

Double-click a server to open a session. Expand the connection progress when it stalls to see which stage is slow (DNS, handshake, authentication or PTY). After a disconnect, the app reconnects with backoff and preserves the screen; the tab context menu also offers manual reconnect.

To autofill `sudo -i` / `sudo -s`, enable **Autofill sudo password** while editing the server, save and reconnect. When a standard English or Chinese password prompt is detected, a popup appears beside the cursor and submits the saved login password once after one second; you can submit immediately, type another password, or press Esc to cancel. After a wrong password only manual input is offered, and private-key passphrases are never reused as login passwords. Nested SSH, `su`, tmux / screen / containers and multiline pastes suspend autofill until the next connection. This feature is independent of the debug API's `terminal.sudo` and enables no MCP capability.

`Cmd/Ctrl+F` searches terminal output, including retained scrollback. It supports case sensitivity and previous/next navigation, and never sends keystrokes to the remote host.

### 3.5 Files and tunnels

Switch the right panel to SFTP; paths synchronize with the terminal and dropped files upload immediately. Create local, remote, dynamic SOCKS5 or reverse SOCKS5 forwarding on the Tunnels page.

#### Dynamic SOCKS5

| Tunnel mode | Where the proxy is configured | Traffic exit |
| --- | --- | --- |
| SOCKS5 `-D` | Local applications use `127.0.0.1:1080` | The SSH server |
| Reverse SOCKS5 `-R` | Applications on the SSH server use `127.0.0.1:1080` | The machine running Ding SSH |

Choose **Reverse SOCKS5**, select a server and set the server-side proxy port (default `1080`). Ding SSH connects from this machine to the server's SSH port; the server may be reachable through an intranet, VPN or jump host. The server must allow remote TCP forwarding (`AllowTcpForwarding remote` / `yes` and the matching `PermitListen` policy). The local side connects to targets using networks reachable from this machine.

Verify the proxy on the server and let the proxy resolve destination names:

```bash
curl --proxy socks5h://127.0.0.1:1080 https://example.com
```

- **The proxy has no authentication.** Other users, processes and containers on the server may use it to reach loopback services, intranets and the public internet through this machine. Enable it only for trusted servers and stop the tunnel when done.
- **Restrict the listening scope.** The app asks the server to bind `127.0.0.1` and rejects non-loopback sources reported by SSH. Keep `GatewayPorts no` in OpenSSH and block the proxy port at the firewall.
- **Enable it per application.** SOCKS5 supports TCP CONNECT only. If you capture all traffic globally, keep a direct route for the SSH peer to avoid tunnel loops.

### 3.6 For AI clients

Open **Settings → Debug mode** and enable the main switch. On first enable only basic metadata is available; terminal, file and UI reads plus all execution capabilities stay off. Use the **Observe**, **Operate** or **Fully trusted developer** preset, or enable individual capabilities.

To authorize a single automation client, enter its name, server / session IDs, read and write paths, and expiry on the same page. Creating it opens a native confirmation window. The token is shown once; up to 32 clients, valid for at most 24 hours (the UI defaults to one hour), all invalidated on restart, and revocable at any time. See [section 9](#9-debug-mode-and-ai-control).

## 4. Interface

```
┌──────────────────────────────────────────────────────────────┐
│ Top bar: tabs (optional) · command palette · settings         │
├────────────┬─────────────────────────────────┬───────────────┤
│ Sidebar    │ Terminal area (splits)           │ Right panel   │
│ · Nav      │  ┌──────────┬──────────┐         │ · SFTP        │
│ · Sessions │  │ tab-1    │ tab-2    │         │ · SysInfo     │
│ · Tabs     │  └──────────┴──────────┘         │ · Transfers   │
├────────────┴─────────────────────────────────┴───────────────┤
│ Status bar: remote CPU / memory / disk / network (optional)   │
└──────────────────────────────────────────────────────────────┘
```

- **Sidebar** has three sections (navigation / sessions / tabs) whose order is one of six choices. Collapse state is stored in `localStorage`; the width is draggable and shared with popover positioning. When collapsed, hovering the logo for 500 ms reveals a temporary overlay without resizing the terminal.
- **Tab bar** can sit in the top bar or the sidebar. The tab context menu can copy the remote IP, reconnect or close.
- **Terminal area** supports horizontal / vertical splits and drag-to-split with draggable dividers.
- **Right panel** is resizable and switches between SFTP, system info and transfer history.
- **Quick connect** is a side drawer that opens a session in one click.
- **Command palette** (`Ctrl/Cmd+K`) is the global entry point; `Esc` closes it.

## 5. Keyboard shortcuts

| Shortcut | Action | Scope |
| --- | --- | --- |
| `Ctrl+K` / `Cmd+K` | Open / close the command palette (ignored while locked) | Global |
| `Ctrl+W` / `Cmd+W` | Close the current tab (workspace only) | Global |
| `Ctrl+1` … `Ctrl+9` | Switch to the Nth tab when it exists | Global |
| `Ctrl+B` / `Cmd+B` | Toggle the sidebar | Global |
| `Ctrl+Shift+B` / `Cmd+Shift+B` | Toggle the right toolbar | Workspace |
| `Esc` | Close the command palette / completion panel / terminal drawer | Global |
| `↑` `↓` `Enter` | Select and run in the command palette | Command palette |
| `Ctrl+V` / `Ctrl+Shift+V` / `Cmd+V` / `Shift+Insert` | Paste | Terminal |
| `Ctrl+Shift+C` / `Cmd+C` | Copy the selection (no-op without one) | Terminal |
| `Ctrl+=` / `Ctrl+-` / `Ctrl+0` | Increase / decrease / reset font size to 13 | Terminal |
| `F11` | Fullscreen | Terminal |
| `Cmd+F` / `Ctrl+F` | Search terminal output; Enter / Shift+Enter navigate, Esc closes | Terminal |
| Custom shortcut (default `Alt+↓`) | Enter completion navigation | Terminal |
| `↑` `↓` `Tab` `Enter` | Change / accept completion candidates | Completion panel |

Paste and copy are intercepted by the terminal in the capture phase to bypass WebView2 accelerator restrictions. `Ctrl+C` always sends SIGINT and is never captured for copy. The right mouse button can be configured to open the context menu or paste directly. Completion navigation accepts forms such as `Alt+ArrowDown` / `Ctrl+Shift+Space` and requires at least one modifier, or a function, arrow, `Space`, `Tab`, `Escape` or `Enter` key.

## 6. Build from source

**Requirements**

| Requirement | Version / notes |
| --- | --- |
| Go | 1.26.10+ (toolchain selected in `go.mod`) |
| Node.js | 22.12+ (`.nvmrc` pins 22.23.4; minimum for Vite 7 / Vitest 5) |
| Wails CLI | `v3.1.0` |
| Linux | `libgtk-3-dev`, `libwebkit2gtk-4.1-dev` (build with `-tags gtk3`) |
| macOS | macOS 12+ and Xcode Command Line Tools for builds |
| Windows | WebView2 runtime; NSIS for the installer |

**Commands**

```bash
# Install the Wails CLI
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0
# Linux (GTK3 / WebKitGTK 4.1)
GOFLAGS=-tags=gtk3 go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0

# Development: frontend hot reload plus Go rebuild
npm --prefix frontend ci
wails3 dev

# Production build (output in build/bin/)
wails3 build

# Windows installer: install directory selectable, UAC helper for protected folders
wails3 task package GOOS=windows ARCH=amd64

# Backend tests (add -tags gtk3 on Linux)
go test ./internal/...

# Frontend tests, type check and build
cd frontend && npm ci && npm test && npm run build

# Full UI gate: type check, build, frontend tests, Go tests and size budgets
bash scripts/verify-ui2.sh
```

Running real PTY tests locally (for example `internal/localterm`) requires permission to create pseudo-terminals; in a restricted sandbox those tests report an environment failure. The Linux CI runner is unaffected.

On Windows, two helpers work around a terminal whose PATH predates the Go/Node installation:

- `scripts/dev/dev-env.ps1` adds Go (`C:\Program Files\Go\bin`, `%LOCALAPPDATA%\Programs\Go\bin`, the project's `.tools\go\bin`), `GOPATH\bin` and Node (`C:\Program Files\nodejs`, `%APPDATA%\npm`) to the current session, optionally configures `GOPROXY` / `GOSUMDB` mirrors, returns to the repository root and prints versions. Dot-source it: `. .\scripts\dev\dev-env.ps1`.
- `scripts/dev/dev.cmd` wraps the script with `-ExecutionPolicy Bypass`. Without arguments it opens an activated project terminal; with arguments it runs one command: `.\scripts\dev\dev.cmd wails3 dev`, `.\scripts\dev\dev.cmd go test ./internal/...`.

**Packaging scripts** (called by the Release workflow)

| Script | Artifact |
| --- | --- |
| `package-macos.sh` | `ding-ssh-<ver>-macos-<arch>.dmg` / `.app.zip` (`arm64` for Apple Silicon, `amd64` for Intel) |
| `package-windows.ps1` | `ding-ssh-<ver>-windows-amd64.zip` / `-setup.exe` |
| `package-linux.sh` | `.tar.gz` / `.deb` (nfpm, depends on gtk3 + webkit2gtk-4.1) / `.AppImage` |
| `set-version.py` | Stamps macOS and Windows native package metadata |
| `update-manifest.py` | Generates `update.json` for branch update channels |
| `debug.ps1` | Debug control-plane helper (reads port and token from `debug.json`) |

## 7. Code map

```
.
├── main.go                 Wails entry: window options, embedded frontend/dist, lifecycle hooks
├── internal/
│   ├── apphost/            Wails host: app facade, debug control plane, native approvals, client policy, jobs and file APIs
│   ├── appdir/             Single configuration-directory entry point + `DING_SSH_DEV_CONFIG_DIR` isolation
│   ├── models/             Shared structures (ServerNode, Settings, DebugSettings, event payloads)
│   ├── sshx/               SSH sessions, independent exec, SFTP cache/safe writes/transfers, tunnels, OSC 7, metrics
│   ├── localterm/          Local shells (PowerShell / cmd / zsh / bash through go-pty)
│   ├── store/              SQLite (WAL) + JSON fallback, migrations, .dingpack
│   ├── cryptox/            AES-256-GCM field encryption, Argon2id master password, keychain
│   ├── debugsrv/           Control-plane core: HTTP/MCP/SSE, capabilities, client policy, confirmation, jobs, SFTP/UI
│   ├── logx/               Log rotation, redaction, tail reads
│   ├── i18n/               Chinese and English messages
│   ├── sshconfig/          OpenSSH Config parsing and import
│   ├── autostart/          Per-platform start at login
│   └── updater/            Branch channels, download verification and install
├── cmd/ding-ssh-mcp/       Standalone stdio MCP proxy for stdio-only clients
├── frontend/src/
│   ├── App.vue             App composition, top-level state and navigation
│   ├── layouts/            AppLayout / AppSidebar / AppTopBar / AppUnlock
│   ├── views/              Persistent terminal workspace and home
│   ├── components/         Domain components plus `ui/` primitives
│   ├── composables/        Theme, shortcuts, toasts, settings and terminal completion
│   ├── stores/             Pinia: servers, sessions, settings, ui, credentials, groups, transfers
│   ├── services/           Thin wrappers over Wails v3 bindings and events
│   ├── completion/         Trie + FZF scoring + shortcut parsing
│   ├── theme/              Tokens, semantic colors, engine, presets, fonts, materials
│   ├── panes/layout.ts     Split layout calculations
│   └── debug/              Frontend UI observation bridge (layout, styles, stores, synthetic events)
├── scripts/                Packaging, versioning, debug helpers, UI gate, MCP audit scripts
├── build/                  Wails platform assets (icons, Info.plist, nfpm, NSIS templates)
└── docs/                   MCP design and interfaces, logging, groups, testing and validation
```

**Frontend/backend contract**: the frontend calls `App` methods through Wails Bind (server CRUD, settings, SFTP transfers, tunnels, sessions, security, import/export). The backend pushes terminal output, session state, connection progress and tunnel state through `EventsEmit`. In debug mode the same events feed SSE subscribers and the MCP push channel — **the UI and the AI see the same data stream**, with increasing `seq` values and gap markers.

## 8. Data and security

**Where files live** (`os.UserConfigDir()/ding-ssh/`, on Windows `%AppData%\ding-ssh\`)

For development and automation, the absolute-path environment variable `DING_SSH_DEV_CONFIG_DIR` moves the database, legacy JSON, logs, UI snapshots, `debug.json`, job records and local test keys into an isolated directory. It does not read or create real keychain entries, and relative paths are ignored. Do not set it in production.

| File | Contents |
| --- | --- |
| `ding-ssh.db` | Main SQLite database (WAL): `servers` / `settings` / `credentials` / `groups` / `command_history` |
| `settings.json`, `servers.json`, `groups.json` | Legacy JSON stores; imported into SQLite on first launch and used only when SQLite is unavailable |
| `security.json`, `.master.key` | Master-password state and local key file (the key moves to the OS Keyring once a master password is enabled) |
| `logs/` | Application logs (see [section 10](#10-logs-and-diagnostics)) |
| `logs/ui-snapshots/` | UI snapshots, mode 0600; up to 64 groups / 64 MiB, pruned when older than 24 hours |
| `debug.json` | Runtime debug info: `url` / `lanUrl` / `port` / `token` / switches / PID, mode `0600`; removed when the service stops, **the token is regenerated on every launch** |
| `mcp-jobs/` | Job summaries and bounded output; directory 0700, files 0600, atomic replacement, no command text or environment |
| `mcp-audit.jsonl` | Optional persistent audit (off by default): client, tool, action, time and result metadata only; no arguments, output or error bodies |

**Encryption chain**

- Field level: AES-256-GCM with an `enc1:` prefix for detection and migration. Legacy plaintext sensitive fields are encrypted at startup.
- Key level: the system keychain (`zalando/go-keyring`) by default, or a master password. Argon2id (3 passes, 64 MiB, 4 threads) derives a 32-byte key. Operations that need the key are blocked by `ensureUnlocked()` before unlock.
- Configuration packages: `.dingpack` is a password-encrypted JSON payload (magic `DINGPACK1`) containing servers, credentials and groups, with up to 500 servers.

**Host trust**

The first connection requires explicit confirmation of the SHA256 fingerprint; a changed host key aborts the connection. Application trust records are handled separately from read-only OpenSSH sources. Revoked records are always rejected, and a legitimate rotation requires the user to verify the new fingerprint in a trusted UI. Headless calls and automatic reconnects never approve new trust, and no login password is sent before confirmation.

**Boundaries**

- No telemetry or account is required. Update checks can be disabled in the update panel; optional online fonts may contact a font source on first use. The debug port listens only while debug mode is enabled.
- Logs and audit records redact tokens, passwords and private keys before writing (`key=value`, `?token=`, JSON secret fields, `Authorization: Bearer`, and full PEM blocks). Redaction is always on and cannot be disabled.
- Diagnostic packages contain only safe projections: debug info, state snapshot and environment. Raw logs, terminal content, authentication tokens, passwords and private keys are never included.
- Capability switches **apply in place**. Only fields that affect the listening address (`enabled` / `port` / `bindLan`) restart the service, so toggling capabilities does not change the port or token or disconnect clients.
- LAN binding increases exposure: `bindLan` makes the plaintext HTTP endpoint reachable from the same network (the token still applies). Turn it off when unused, and prefer SSH forwarding for remote access.

## 9. Debug mode and AI control

### 9.1 Enabling and connecting

Debug mode is off by default and listens on no port when disabled. When enabled it binds `127.0.0.1:<port>` (default 8765; port 0 picks a free port) and can optionally bind the LAN. The URL and token appear under **Settings → Debug mode → Runtime status** and in `debug.json`.

- Authentication accepts only `Authorization: Bearer <token>`; query tokens are rejected so they cannot leak through browser history, proxy logs or screenshots.
- Execution confirmation accepts only the `X-Ding-Confirm-Token` header, never a query parameter.
- The server validates Host and Origin to reject DNS rebinding and cross-origin browser requests. Concurrent HTTP requests are capped at 64 and SSE connections at 128; excess requests get 429. `/mcp` bodies are capped at 8 MiB and headers at 64 KiB.
- MCP catalog and HTTP read endpoints still require a token; there is no unauthenticated exception.

### 9.2 Permission model: capabilities, client scope and native approval

On first enable only basic metadata is open. Terminal, remote-file and UI reads plus all execution capabilities default to off. Existing configurations keep capabilities that were explicitly saved; newly added capabilities are not enabled automatically. Three presets are available, and expanding permissions requires native system approval:

| Preset | Capabilities enabled | Notes |
| --- | --- | --- |
| **Observe** | `terminal.read`, `fs.remote.read`, `ui.inspect` | Read terminals, remote files and UI; useful for troubleshooting and demos |
| **Operate** | Observe plus `session.connect`, `session.disconnect`, `credentials.use`, `terminal.input`, `transfer.cancel`, `config.write`, `fs.remote.write` | Allows remote commands and configuration changes; file writes still require the allowlist and per-operation approval |
| **Fully trusted developer** | Existing capabilities plus `automation.trusted`, `eval`, `ui.write`, `ui.inspect`, `terminal.read` | Arbitrary DOM interaction and page JS; not a sandbox, only for trusted local automation |

| Capability | Default | Purpose |
| --- | --- | --- |
| `read` | on (cannot be disabled) | Safe basic metadata; no terminal, file, UI or secret fields |
| `terminal.read` | off | Read buffers and output of authorized sessions |
| `fs.remote.read` | off | Read remote directories and files of authorized sessions |
| `ui.inspect` | off | Read layout, DOM, console, styles and screenshots |
| `session.connect` | off | Open connections and tabs; saved credentials also need `credentials.use` |
| `session.disconnect` | off | Disconnect or reconnect sessions, possibly interrupting jobs |
| `credentials.use` | off | Use saved credentials internally; never export plaintext |
| `terminal.input` | off | Type into a terminal or run independent remote commands, with arbitrary side effects |
| `exec.local` | off | Also allow local command execution; a fully trusted capability |
| `ui.write` | off | UI experiments and navigation; arbitrary DOM interaction also needs `automation.trusted` |
| `automation.trusted` | off | Fully trusted UI interaction that can trigger persistent business changes |
| `eval` | off | Execute page-context JS; also needs **Allow JS execution** and `automation.trusted` |
| `config.write` | off | Write servers, tunnels and app settings (debug authorization stays user-managed) |
| `secrets.read` | off | Explicitly read / export plaintext credentials, separate from internal use and writing |
| `secrets.write` | off | Create, modify or delete saved credentials |
| `fs.remote.write` | off | Remote file writes; also needs a non-empty allowlist and client path scope |
| `transfer.cancel` | off | Cancel transfers owned by the caller |
| `sudo.credential` | off | Use the saved password for sudo; also needs `credentials.use` and per-operation approval |
| `lifecycle` | off | Reload, quit, clear logs and similar actions; requires approval |

**The remote-write allowlist is a hard boundary**: empty means no writes at all. Only absolute paths matching a prefix are writable (`/srv/app` allows `/srv/app` and `/srv/app/x`, not `/srv/app-evil`). The host also verifies canonical paths and refuses to write a final symlink, but ordinary SFTP cannot fully eliminate races between remote path checks and replacement. For strong isolation use a restricted remote account, chroot or a controlled proxy.

### 9.3 Client authorization

The settings page can issue an independent token for each automation client:

- Name, capability set, server IDs, session IDs, readable paths, writable paths and expiry.
- Expiry of at most 24 hours (the UI defaults to one hour); at most 32 clients; the token is shown only once.
- Grants live in memory only, are invalidated when the app restarts, and can be revoked at any time.
- Restricted clients **cannot** export plaintext credentials, execute local commands, run arbitrary JS or use fully trusted UI automation.
- Servers and sessions are authorized separately; a transfer's `sessionId` is not an identity; global capabilities are an upper bound that client tokens cannot expand.
- Client grants can only be created from the local settings UI; the MCP catalog contains no tool for creating grants.

### 9.4 Independent approval and execution confirmation

Config, file and lifecycle tools registered as confirmable first call `confirm.prepare`. The server checks the actual target and complete arguments, then shows a **native system approval window outside the WebView**. No execution credential is issued without approval.

```json
{"name":"confirm.prepare","arguments":{"action":"servers.delete","tool":"servers.delete","args":{"id":"srv-1"}}}
```

- On approval it returns `operationId`, `status: approved`, a one-time `token`, and a server-generated redacted summary. `confirm.status` queries the state later but never returns the token again.
- The user can approve once, or allow the **same caller, action and tool with the exact same arguments** for ten minutes.
- The execution credential is one-time, bound to client identity / tool / action / arguments, expires after 120 seconds, and is invalidated by revocation or permission changes.
- The tool is then called with the original arguments and token (MCP `token` or `confirm`; HTTP `X-Ding-Confirm-Token`).
- Arbitrary terminal commands are allowed continuously under the granted execution capability without per-command prompts. This is equivalent to handing command execution to the caller, so grant only the intended targets.
- MCP cannot expand its own permissions through `capability.enable`, settings updates, settings reset or `ui.storePatch`. Debug switches and client grants remain local-user actions.

`app.permissions` returns the current capability snapshot, per-tool requirements and the confirmable action list.

### 9.5 HTTP endpoints

| Method | Path | Description |
| --- | --- | --- |
| GET | `/v1/health` | Runtime status, version, port and switches |
| GET | `/v1/state` | View / tabs / sessions / tunnels / settings and debug snapshot |
| GET | `/v1/settings` · PUT | Read settings; partial update (only changed fields), effective immediately |
| GET | `/v1/servers` | Server list (redacted by default; no passwords or private keys) |
| GET | `/v1/sessions` | Backend session list |
| GET | `/v1/terminals` | Terminal diagnostics: `rows/cols`, buffer lines, `baseY/viewportY`, scrollback reachability, visible text and DOM geometry |
| GET | `/v1/terminals/{id}/buffer?from=tail&lines=200` | Read terminal buffer including scrollback |
| POST | `/v1/terminals/{id}/input` | Write to a session with `{data}` or `{base64}` |
| POST | `/v1/terminals/{id}/scroll` | `{to: top\|bottom\|line\|lines, value}` |
| POST | `/v1/terminals/{id}/resize` | `{cols, rows}`; changes the emulator only, not the PTY |
| POST | `/v1/sessions/{id}/reconnect` · `/disconnect` | Reconnect (keep the buffer) / disconnect (keep the tab) |
| POST | `/v1/tabs` · DELETE `/v1/tabs/{id}` | Open a tab (`{serverId}` / `{node}` / `{local:true}`) / close a tab |
| POST | `/v1/eval` | Execute page-context JS (needs `eval` + `automation.trusted` + **Allow JS execution**) |
| GET | `/v1/screenshot` | Window screenshot as PNG (**Windows only**; the window must be visible) |
| GET | `/v1/events?limit=&after=&topics=` | Recent events (ring buffer, default 50, max 300; `after` continues by seq) |
| GET | `/v1/stream?topics=ssh.output,ssh.status` | SSE event stream filtered by topic |
| GET | `/v1/logs` · `/v1/logs/tail` | Log directory and retention; tail reads filtered by level / keyword / file |
| POST | `/v1/logs/clear` · `/export` · `/options` | Clear logs (`lifecycle` + token) / export safe diagnostics / change log switches |
| GET | `/v1/tools` | **Tool catalog**: `{name, capability[], confirmAction?, needsToken, description, inputSchema, annotations, hasStructuredOutput, outputSchema?}`, same source as `tools/list` |
| GET/POST | `/v1/ui/*` | UI observation and experiments: reads need `ui.inspect`; writes need `ui.write`, arbitrary interaction also needs `automation.trusted` |
| * | `/mcp` | MCP over Streamable HTTP: POST requests / GET push / DELETE end |

Helper script (reads the port and token from `debug.json`):

```powershell
.\scripts\debug.ps1 health
.\scripts\debug.ps1 state
.\scripts\debug.ps1 terminals
.\scripts\debug.ps1 servers / settings
.\scripts\debug.ps1 buffer <id> [lines]
.\scripts\debug.ps1 input <id> "echo hi`r"
.\scripts\debug.ps1 scroll <id> <to> [value]
.\scripts\debug.ps1 resize <id> <cols> <rows>
.\scripts\debug.ps1 open <serverId> [-Local] / close <id>
.\scripts\debug.ps1 reconnect <id> / disconnect <id>
.\scripts\debug.ps1 eval "<js>"
.\scripts\debug.ps1 events [limit] [topics]
.\scripts\debug.ps1 stream [topics]
.\scripts\debug.ps1 screenshot [out.png]    # Windows
.\scripts\debug.ps1 mcp tools|resources|call <tool> [json]
```

### 9.6 MCP tools

The endpoint is `POST http://127.0.0.1:<port>/mcp`. There are currently **124 tools**:

| Domain | Count | Highlights |
| --- | --- | --- |
| Built-in core | 27 | State, terminal read/input/scroll/resize, tabs, server list, settings, logs, screenshot, events, permissions, audit, `confirm.*` |
| UI and workspace | 28 | Layout, DOM, styles, console, assertions, snapshots/diffs, experiments, tab activation, panels, splits |
| Application control | 31 | Groups, server CRUD / duplicate / move / import/export, connection tests, credentials, tunnels, settings, commands, quit |
| Files and interactive terminal | 17 | `sftp.list/stat/read/download/upload/patch`, atomic/conditional writes, transfer cancel, `terminal.run/expect/sudo` |
| Jobs and operations | 21 | `exec.start/plan/batch`, `jobs.*`, session connect/wait, `servers.search`, host-key challenges, metrics, processes, tunnel health, systemd services |

Parameters and guarantees are documented in [MCP tasks and files](docs/MCP任务与文件接口.md). `terminal.run` uses a random exit marker per call and returns `completionKnown`; it never claims completion without an exit code from the current call.

**Protocol**: negotiates `2024-11-05`, `2025-03-26` and `2025-06-18`; strictly validates the JSON-RPC envelope, `Content-Type`, argument types and schemas. Only 2025-06-18 receives `structuredContent/outputSchema`. Tools carry `readOnlyHint` / `destructiveHint` / `idempotentHint` annotations as hints; they never replace server-side authorization.

**Sessions and events**: each MCP session allows one active SSE connection (a second returns 409); at most 128 sessions with a 30-minute idle TTL. Events carry increasing `seq`, an `after` cursor and a `gap` flag; after a gap, re-read state or job output instead of assuming event completeness.

**stdio proxy**: handles up to 32 concurrent requests plus cancellation; a request times out after 11 minutes; it retries only when the request is known not to have been accepted, never replaying a sent side-effecting request.

### 9.7 Jobs, files and operations

- **Independent execution**: `exec.start` opens a separate non-PTY exec channel on an existing SSH connection. It does not type into the user's terminal or change its directory or environment. It supports `raw` / `posix` modes, `cwd`/`env`, timeouts, output budgets and idempotency keys. Local execution requires explicit `local=true` and `exec.local`. The process keeps at most 8 unfinished jobs and 64 retained jobs; completed records live for one hour; each job stores at most 1 MiB / 4,096 output chunks. Without exit confirmation the result is `unknown`; after an app restart unfinished jobs become `unknown` and are never replayed.
- **Job queries**: `jobs.get/wait/output/cancel/list`. `jobs.output` pages stdout / stderr by `cursor`; use `base64` to preserve bytes across UTF-8 boundaries. `hasMore=false` means the current page is exhausted, not that a running job will produce no more output.
- **Multi-host plans**: `exec.plan` freezes 1–8 connected SSH sessions and their full arguments, returning a `planId`, `dryRun` and a 120-second lifetime. `exec.batch` submits with concurrency 1–4, returns an independent `job` per target, and accepts each plan once. There is no transaction rollback across hosts.
- **Operations**: `hosts.metrics`, `processes.list`, `tunnels.health`; on Linux `services.status/logs` use a fixed `systemctl show` and journald reads, while `services.restart` is a write operation requiring native approval for the exact `sessionId/unit/idempotencyKey` and returning a job asynchronously. No automatic sudo; missing procps / systemd / permissions are reported through `available/error`.
- **File reads**: `sftp.list` pages directories (up to 500 per page); `sftp.read` / `sftp.download` read in chunks (default 256 KiB, max 1 MiB, returning `sha256/nextOffset/truncated`) and never touch arbitrary local paths.
- **File edits**: `sftp.patch.preview` previews an exact text replacement without writing; `sftp.patch` requires the preview's `expectedHash` plus an approval token, re-reads and verifies the whole file, then replaces it through a private same-directory temporary file and POSIX atomic rename. `sftp.upload` accepts base64 (at most 1 MiB decoded); `sftp.write` accepts text or base64, `append` and `expectedHash` (including `missing`). Overwrites are staged and atomically replaced; if the target exists and the server cannot replace atomically, the call is refused. Basic mode / owner metadata is preserved where possible; ACLs and xattrs are not guaranteed.
- **Interactive terminal**: `terminal.run/expect/sudo` share the user's terminal. Only one operation per session is allowed at a time; the second is rejected. `terminal.run` uses a random exit marker per call, and `completionKnown:false` must not be treated as completion. `terminal.sudo` reads the password only from the credential store and feeds it to `sudo -S` through a random temporary file (`/tmp/.ding-sudo-<12hex>`, `chmod 600`); the password never enters arguments, return values, logs or audit. Cleanup uses both the original shell and an independent SFTP channel, reporting `cleanedUp` / `cleanupVerified`.
- **Panel and transfers**: `sftp.syncCwd` only changes the file panel (`ui.write` required) and never types `cd`; `sftp.cancel` only needs `transfer.cancel`. Cancellation is a request applied at the next data-chunk checkpoint and does not guarantee immediate deletion of a remote temporary file.

> **A safer privilege path**: prefer passwordless sudo for AI (for example, `/etc/sudoers.d/ding-ai` with `deploy ALL=(root) NOPASSWD: /usr/bin/systemctl restart nginx`). `terminal.run` can then do the same work without enabling `sudo.credential`, which is only for machines with no passwordless sudo and an immediate need to elevate.

### 9.8 MCP resources and push

| URI | Type | Content |
| --- | --- | --- |
| `app://state` · `app://debug/info` · `app://servers` · `app://logs` | JSON | State, debug info, servers (redacted), log settings |
| `app://logs/tail` · `terminal://{id}/buffer` · `ui://console` | text | Log tail, terminal buffer, safe frontend console metadata |
| `ui://layout` · `ui://view` | JSON | Layout skeleton, current view and navigation state |
| `sftp://{sessionId}/cwd` · `transfers://current` | JSON | Session working-directory summary (first 20 entries), recent transfers |

Resource templates are `terminal://{id}/buffer` and `sftp://{sessionId}/cwd`; `resources/subscribe` / `unsubscribe` and `resources/updated` are supported. Server pushes use `notifications/message` with `logger` set to `ding-ssh/<topic>` (for example `ssh.output`, `ssh.status`, `tunnel.status`), plus a `: ping` every 20 seconds. `?topics=` can restrict subscriptions during `initialize`.

```bash
# 1) initialize: use -D to print headers and capture Mcp-Session-Id
curl -s -D - -X POST http://127.0.0.1:8765/mcp \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'

# 2) attach the push channel (-N disables buffering; Ctrl+C exits)
curl -N -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SESSION" http://127.0.0.1:8765/mcp

# 3) end the session
curl -X DELETE -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SESSION" http://127.0.0.1:8765/mcp
```

**HTTP client configuration**:

```json
{
  "mcpServers": {
    "ding-ssh": {
      "type": "http",
      "url": "http://127.0.0.1:8765/mcp",
      "headers": { "Authorization": "Bearer <token>" }
    }
  }
}
```

**stdio clients** can use the bundled proxy, which forwards JSON-RPC from stdin to `POST <url>/mcp`, writes responses and pushes to stdout, and keeps all logs on stderr with tokens masked:

```bash
go build -o ding-ssh-mcp ./cmd/ding-ssh-mcp
./ding-ssh-mcp -v
```

| Flag / environment variable | Description |
| --- | --- |
| `-url` · `DING_SSH_DEBUG_URL` | Debug service URL such as `http://127.0.0.1:8765` (`/mcp` suffix optional) |
| `-token` · `DING_SSH_DEBUG_TOKEN` | Debug token (also accepts a per-client `dsc_` token) |
| `-debug-json` | Path to `debug.json`; defaults to the configuration directory |
| `-v` / `-quiet` / `-once` / `-h` | Verbose forwarding / errors only / availability probe / Chinese usage |

Resolution order: `-url`/`-token` → environment → `debug.json`. After receiving a session ID the proxy attaches to `GET /mcp` for pushes and reconnects with 1s / 2s / 5s backoff. On exit it closes SSE and attempts `DELETE /mcp`. If the app restarts with a different port or token, the proxy re-reads `debug.json` on request failure (explicit flags are never overridden).

### 9.9 Audit and UI observation

- **AI records** (settings tab): every call leaves a structured record (time, source `mcp|http|ui`, tool, capability, duration, result, redacted arguments, reversibility), whether it succeeded, was denied or failed; the ring buffer keeps the latest 500. Settings changes (`update_settings` / `set_log_options`) offer an **Undo** button (the last 20 pre-change snapshots are kept). AI clients can inspect records through `app.recent_calls`.
- **Persistent audit** (optional): fixed metadata only — client, tool, action, time and result — mode 0600, two rotating 2 MiB files; never arguments, commands, output or error bodies.
- **UI observation**: `ui.layout` (layout skeleton plus unexpected overlaps), `ui.query` / `ui.styles` / `ui.tokens` / `ui.hit` / `ui.tree`, `ui.console`, `ui.revision` (detects stale DOM knowledge), `ui.screenshot` / `ui.elementShot`, `ui.snapshot` / `ui.diff` / `ui.assert`.
- **Implicit data protection**: `ui.tree` reports only prop names, types and sizes; DOM text excludes hidden and sensitive controls. `ui.console` keeps only time, level, source and argument types/sizes — no message text, exception messages or stack traces. Native console output is unchanged.
- **UI experiments** (need `ui.write`): `ui.setToken`, `ui.injectCSS`, `ui.storePatch`, `ui.navigate`, `ui.window`, `ui.reload`, `ui.click` / `ui.type` / `ui.key` / `ui.hover` / `ui.scroll`. Changes affect only the running instance (memory), not settings or the database, and revert on reload. Synthetic DOM events can trigger real business actions, so arbitrary interaction also requires `automation.trusted`.
- **Stable selectors**: automation should prefer semantic IDs such as `[data-ui="topbar"]`, `sidebar`, `terminal`, `context-panel`, `statusbar`, `command-palette` and `settings`; dynamic tabs use `data-tab-id`, servers and commands use business IDs in `data-testid`. Do not depend on Tailwind classes or array indices.
- **Two diff modes**: pixel diff compares 16×16 blocks (`threshold` controls channel difference), merges adjacent changes into `regions[]` (max 20) and returns `changedRatio` / `changedPixels` plus a red-overlay image; fingerprint diff reports only changed fields (`changed[].fields[]`), with `styles` resolved to individual CSS properties. Snapshots live in `logs/ui-snapshots/` with validated file names.

## 10. Logs and diagnostics

Logs live in `<os.UserConfigDir()>/ding-ssh/logs` (Windows `%AppData%\ding-ssh\logs`, macOS `~/Library/Application Support/ding-ssh/logs`, Linux usually `~/.config/ding-ssh/logs`):

- Current file `app-YYYYMMDD.log`; rotated to `app-YYYYMMDD-HHMMSS.log` when it exceeds **8 MB** or crosses a day.
- At most **20** files are retained; the oldest are deleted first and the active file is never removed.
- Tail reads default to 200 lines and at most 4 MB per read.

| Setting | Description |
| --- | --- |
| Runtime log (console) / write log file | Independent switches |
| Log level | `debug` / `info` / `warn` / `error`, default `info` |
| Log MCP / debug API calls | Separate switch (off by default). Each call adds one `[api]` line with method, path, status, duration and response bytes; only the path is logged, never the query or token |
| Open log folder / clear logs | Clearing only deletes `app-*.log`, closes handles first, then recreates files according to the switches |
| Export diagnostics | Creates `diagnostics-YYYYMMDD-HHMMSS.zip` containing safe projections of `debug.json`, `state.json` and `env.json`. Raw logs and terminal content are excluded by default; one failed entry does not block the rest |

The terminal context menu also offers **Start / stop tracing this session**: output is written to the log with a `[trace:<id>]` prefix (ANSI removed, each entry truncated at 2,000 characters), which helps reproduce connection issues. Fine-grained tracing requires log level `debug`.

See [`docs/logging.md`](docs/logging.md) for level semantics, format, redaction and sampling.

## 11. Known limitations

**Window screenshots are Windows-only.** `/v1/screenshot`, `screenshot`, `ui.screenshot`, `ui.elementShot` and `ui.snapshot` capture pixels through Windows GDI. macOS and Linux return an explicit unsupported reason, and the tool catalog marks `platformSupported=false`. No cross-platform pixel validation is claimed.

**WebGL and UI scaling (fixed).** Scaling uses `.app-shell { zoom }`, while `@xterm/addon-webgl` sizes the canvas from a zoom-inclusive `devicePixelContentBoxSize`, so the bitmap and render grid disagreed (top lines invisible, buffer already at the top). The fix is in `frontend/src/utils/xterm-webgl-zoom.ts`: when zoom ≠ 1 it ignores the zoom-inclusive size and, after `fit()` or a scale change, verifies that the bitmap matches the grid and reruns `handleResize()` to restore viewport, uniforms and vertices. WebGL is now used at every supported scale; if the private-API patch cannot apply and the scale is not 100%, the renderer falls back to Canvas. Check the `[webgl] enabled …` log line when debugging.


**Other**

- **CDP is unavailable**: The app provides page debugging through `/v1/eval` and uses native window capture for Windows screenshots.
- **`/v1/terminals/{id}/resize` changes only the emulator size**, not the remote PTY; it exists to reproduce layout and rendering issues.
- **Event channels can drop events**: `/v1/stream`, `GET /mcp` (SSE) and terminal automation share one Hub; a slow subscriber loses events (the Hub neither queues nor blocks publishers). Events carry `seq` and `gap`; after a gap, re-read state. `terminal.run` / `terminal.expect` can also miss output; use `read_terminal` for a complete buffer.
- **`ui.reload` remounts the entire frontend**: terminals reconnect, DOM state and snapshot fingerprints are invalidated (`ui.revision` increments), and in-flight `ui.*` sessions must re-locate elements. Take `ui.snapshot` first if the state matters.
- **`ui.*` requires a built frontend**: outside `wails3 dev`, run `npm run build` (or `wails3 build`) first, otherwise the page has no debug bridge and `ui.*` returns a Chinese "unknown operation" error (the backend still works).
- **`wrapExitMarker` / `terminal.run` exit codes are POSIX semantics**: Windows local terminals (cmd / PowerShell) cannot provide an exit code; use `read_terminal` or check `%ERRORLEVEL%` / `$LASTEXITCODE` yourself.
- **`confirm.prepare` waits synchronously**: there is no asynchronous approval inbox, and `confirm.status` only reports the execution credential state, not business success. Time-limited approval is bound to exact arguments; there is no wildcard command approval.
- **Remote execution and cancellation are bounded**: closing an SSH channel does not prove that the entire remote process tree stopped; without exit confirmation the result is `unknown`. App restarts do not keep jobs running and never replay commands. Each SSH transport allows at most 8 not-yet-finished independent channel operations; an unresponsive remote holds a slot until the operation ends or the connection closes.
- **File writes are not a filesystem sandbox**: ordinary SFTP cannot eliminate races caused by other remote processes; appends are non-atomic and ACLs / xattrs are not guaranteed. Arbitrary shell execution can bypass the SFTP allowlist; strong isolation requires a restricted remote account, chroot or a controlled proxy.
- **Fully trusted capabilities are not sandboxes**: `terminal.input`, `exec.local`, `ui.write` + `automation.trusted`, and `eval` can all produce persistent or external side effects. The Operate preset allows arbitrary remote commands. Use these only for trusted local automation.
- **Remote access remains local-first**: there is no TLS or OAuth, and `bindLan` only expands plaintext HTTP exposure. Use SSH port forwarding for remote access.
- **macOS packages are ad-hoc signed and not notarized**; right-click and choose **Open** on first launch.
- **LAN binding expands exposure**: with `bindLan` the endpoint is reachable from the same network (the token still applies). Turn it off when unused.

## 12. CI and releases

| Workflow | Trigger | What it does |
| --- | --- | --- |
| `ci.yml` | Push to `main` / `master` / `dev`, any PR | Go `internal` tests + frontend `npm run build` (type check and build). Pushes to `main` / `dev` also build all three platforms in parallel, upload artifacts and publish `update.json` to the `main-latest` / `dev-latest` prereleases (no formal Release) |
| `release.yml` | Push a `v*` tag or manual dispatch | Runs tests, builds and packages all three platforms, writes `SHA256SUMS.txt` and creates a Release (manual dispatch uploads artifacts only) |

- Branch builds use versions such as `0.0.0-dev.<sha>` and inject `BuildChannel` / `BuildCommit` through `-ldflags`; unreleased builds are never mistaken for formal releases.
- Branch update channels identify builds by commit SHA. Historical downloads use unique file names so a running download is not overwritten.
- Downloads prefer `HTTPS_PROXY` / `HTTP_PROXY` (honouring `NO_PROXY`), then the macOS / Windows manual system proxy or the Linux GNOME manual proxy. PAC is not supported.

```bash
git tag v1.2.1
git push origin v1.2.1
```

## 13. Interface and design system

- **Token layers**: `theme/tokens.css` → `semantic.css` → `components.css`; Tailwind v4 maps semantic tokens only, the theme engine writes semantic values, and terminal palettes live independently in `terminal.css`.
- **Dynamic color**: Material Color Utilities HCT / Tonal Spot derives navigation, workspace, cards, interactions, text and state colors from a user seed. There are 10 presets, custom ANSI 16 colors, and Solid / Soft Glass / Clear Glass materials.
- **Accessibility**: core dialogs, menus and the command palette have focus traps, arrow / Home / End / Esc handling, focus restoration and ARIA semantics, and respect `prefers-reduced-motion`.
- **Automation contract**: key regions expose `data-ui`; dynamic items use business IDs in `data-tab-id` / `data-testid`, so `ui.layout` depends only on stable semantic selectors.
- **Asset governance**: Geist Sans, Geist Mono + Noto Sans SC ship as offline WOFF2 fonts; JetBrains Mono, Fira Code, Maple and Sarasa download and cache on selection, retry on failure and fall back to bundled fonts. `scripts/verify-ui2.sh` checks types, build, tests and CSS / JS / dist size budgets; font licenses ship with the app.

## 14. Documentation and licensing

- [`CHANGELOG.md`](CHANGELOG.md): changes in reverse development order (symptom → cause → fix).
- [`docs/MCP任务与文件接口.md`](docs/MCP任务与文件接口.md): parameters, limits and examples for job, file and operations APIs.
- [`docs/MCP改造实施与验证记录.md`](docs/MCP改造实施与验证记录.md): scope, validation results and explicit boundaries of the MCP overhaul.
- [`docs/MCP能力设计评审与演进方案.md`](docs/MCP能力设计评审与演进方案.md): the pre-implementation design review and evolution plan (historical).
- [`docs/logging.md`](docs/logging.md): log levels, format, redaction and sampling.
- [`docs/分组嵌套设计方案.md`](docs/分组嵌套设计方案.md): path model and cascade rules for folder-style groups.
- [`docs/testing/`](docs/testing/) · [`docs/validation/mcp-2026-11-22.json`](docs/validation/mcp-2026-11-22.json): test records and redacted integration counts.
- [`scripts/audit/`](scripts/audit/): `mcp_security_smoke.py`, `live_smoke.py` and `mcp_sdk_compat.mjs` for rerunning security and MCP compatibility checks against an isolated configuration.

This project is licensed under the [MIT License](LICENSE). Third-party dependencies and bundled fonts retain their original licenses; an `OFL.txt` ships with the fonts, with additional font licenses in `frontend/public/licenses/fonts/`.
