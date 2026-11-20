[简体中文](README.md) | English

<div align="center">
  <img src="frontend/icon/ding.png" width="280" alt="ding-ssh cat terminal icon">
  <h1>ding-ssh</h1>
  <p><strong>SSH terminals, SFTP, port forwarding and local shells in one desktop workspace.</strong></p>
</div>

[![CI](https://github.com/FunkyPantsSa/ding-ssh/actions/workflows/ci.yml/badge.svg?branch=dev)](https://github.com/FunkyPantsSa/ding-ssh/actions/workflows/ci.yml)

A desktop workspace combining **SSH / SFTP / port forwarding / local shells**, with a control interface (**HTTP + SSE + MCP**) that listens locally by default. Scripts and AI clients can inspect application state and operate terminals.

| Item | Details |
| --- | --- |
| Platforms | Windows x64 · macOS (Apple Silicon / Intel builds) · Linux x64 |
| Stack | Go 1.25 + Wails v2.13 · Vue 3.5 + Pinia + Tailwind v4 + Vite 7 · xterm.js 6 |
| Storage | Local SQLite (WAL) + system keychain; no cloud synchronization or telemetry |
| Branches | `dev` for development; `main` for integration |
| Change history | [CHANGELOG](CHANGELOG.md) · [UI design](docs/2.0设计规范.md) · [Architecture](docs/refactor-2.0/) |

Choose **Settings → General → Language** for Simplified Chinese or English. Chinese is the default and the selection is saved locally. Switching updates the interface in place without reloading the page or closing terminal sessions. Pages, help text, connection and transfer messages, and native file dialog titles support both languages. User content and remote terminal output retain their original language.

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
13. [UI 2.0 design system](#13-ui-20-design-system)
14. [Documentation and licensing](#14-documentation-and-licensing)
15. [1.1.1 updates](#111-updates)
16. [1.2.1 updates](#121-updates)

## 1. Purpose

Routine operations often mean switching between tools to connect, configure hosts, transfer files, create tunnels and monitor load. ding-ssh puts these tasks in one terminal workspace that can also be automated.

1. **Local first.** Servers, credentials, command history and settings live in the local configuration directory. Sensitive fields use AES-256-GCM; keys use the system keychain or a master password. No account or server component is required.
2. **Terminal centered.** SFTP and shell directories synchronize in both directions. Tunnels, local shells and split panes share the tab system.
3. **Observable and controllable.** Debug mode exposes local HTTP and MCP endpoints for layout inspection, terminal buffer reads, command input, output waits, screenshot comparisons and diagnostic export. Capabilities control access; destructive actions require two-step confirmation.

The application does not provide server-side bastion auditing, fleet configuration deployment or a general-purpose SSH proxy service.

## 2. Features

| Module | Features | Implementation |
| --- | --- | --- |
| Terminal | Multiple tabs; horizontal / vertical splits; drag tabs to split; draggable dividers | xterm.js with WebGL at all supported interface scales; DOM fallback when unsupported or context is lost; 5,000 scrollback lines; font-size shortcuts |
| Connections | Password, private key, Agent and automatic authentication; groups, search, availability tests; JumpServer bastion mode | DNS/direct → TCP → authentication → PTY → ready, with progress logs; 15s keepalive; 2/5/10/20/30s reconnect backoff; preserves screen on reconnect |
| SFTP | Browse, multi-file / drag uploads, downloads, rename, delete, folders, editable path, cancelable transfers | SWR directory cache (`sync.Map`, asynchronous revalidation, incremental events); concurrency = CPU cores × 2; token-bucket rate limiting, unlimited by default; rz/sz through Zmodem |
| Directory sync | Shell cd follows into SFTP; SFTP navigation follows into terminal | OSC 7 preferred, prompt parsing as fallback; independently configurable directions |
| Tunnels | Local, remote, dynamic SOCKS5 and reverse SOCKS5; start, stop, restart, edit, delete | State updates through events |
| Local terminal | Local shell without SSH | PowerShell / cmd on Windows; zsh / bash on macOS; default shell on Linux, through go-pty |
| Completion | Suggestions while typing and local history ranking | Trie + FZF-style scoring; configurable navigation shortcut |
| Command palette | Navigate, connect, open local terminals and settings with Ctrl/Cmd+K | Recent commands, servers, actions and navigation; commands reflect current state |
| Metrics | Remote CPU, memory, disk and network in status bar / side panel | Per-session collection |
| Appearance | Presets, custom ANSI colors, backgrounds, blur, fonts and system light/dark mode | 80%–150% scaling; tabs in top bar or sidebar; configurable section order; resizable panels |
| Output colors | Off / Semantic / Vivid modes | Frontend Colorizer adds SGR only; terminal control sequences and prompts without newlines pass through immediately |
| Data | Password-encrypted .dingpack import/export; master password; device migration | Up to 500 servers; optional overwrite by matching ID |
| Automation | HTTP API, SSE and MCP, including stdio proxy | Capabilities, two-step confirmation and audit records; see section 9 |

## 3. Quick start

### Install and launch

[GitHub Releases](https://github.com/FunkyPantsSa/ding-ssh/releases) provide macOS `.dmg` / `.app.zip`, Windows `setup.exe` / `.zip`, and Linux `.AppImage` / `.deb` / `.tar.gz`, with `SHA256SUMS.txt`. Successful `main` / `dev` CI builds also publish artifacts such as `ding-ssh-dev-windows-amd64-<sha>`. Unsigned macOS packages may require Finder → right-click → Open on first launch.

Windows installers default to `C:\Program Files\FunkyPantsSa\ding-ssh` and let you choose another directory. Windows Installed Apps provides an uninstall entry. In-app updates install directly when the directory is writable; otherwise a separate worker requests administrator permission through UAC to replace the executable. The original unelevated helper restarts the app so it does not inherit administrator privileges. Cancelling UAC keeps the old version. Configuration is stored separately in `%APPDATA%\ding-ssh`. Older builds need one manual installation of a build containing this update worker, or an update performed by running the old app as administrator.

A master-password prompt appears if enabled; otherwise the workspace opens directly. Enable reconnecting previous servers in Settings to restore sessions that were connected when the app closed.

### Add and connect a server

Open server management and create a server with host, port, username and authentication. Paste a private key or choose its file, or use an Agent. Groups organize servers. Availability tests report TCP latency and SSH port reachability. The server context menu copies the IP or duplicates the configuration as `original.new` and opens it for editing.

Open a server to start a session. Inspect the connection progress if DNS, handshake, authentication or PTY allocation stalls. Verify a new host's fingerprint through a trusted channel before accepting it.

### Files and tunnels

Choose SFTP in the right panel. Directories synchronize with the terminal; drop files into the panel to upload. Create local, remote, dynamic SOCKS5 or reverse SOCKS5 forwarding on the Tunnels page.

| Mode | Proxy configuration | Network exit |
| --- | --- | --- |
| SOCKS5 -D | Local application uses `127.0.0.1:1080` | SSH server |
| Reverse SOCKS5 -R | Application on the SSH server uses `127.0.0.1:1080` | Computer running ding-ssh |

For reverse SOCKS5, choose the server and its proxy port (default `1080`). ding-ssh connects to the server's SSH port, including through an internal network, VPN or jump hosts. The server must permit remote TCP forwarding (`AllowTcpForwarding remote` or `yes`, with appropriate `PermitListen`). Destinations are reached from the local computer. If only the server can initiate a connection to your computer, establish a reachable path first or use a server-side SSH client to connect to your computer's SSH service.

Test on the server, using proxy-side DNS resolution:

```bash
curl --proxy socks5h://127.0.0.1:1080 https://example.com
```

- **No proxy authentication.** Other server users, processes and containers may use it to access local loopback services, private networks and the internet through your computer. Use only with trusted servers and stop it when finished.
- **Limit the listening scope.** The application requests `127.0.0.1` and rejects non-loopback sources reported by SSH. Keep `GatewayPorts no` and block external proxy access with a firewall. `GatewayPorts yes` can widen the listener. Server configuration and reported sources must be trustworthy. See the [OpenSSH manual](https://man.openbsd.org/sshd_config).
- **Configure applications explicitly.** SOCKS5 supports TCP CONNECT. For global routing, keep a direct route to the SSH peer to avoid routing loops.

For AI clients, enable Settings → Debug mode and the capabilities needed. The port and token are written to `debug.json`.

## 4. Interface

```text
┌────────────────────────────────────────────────────────────────┐
│ Top bar: optional tabs · command palette · settings             │
├──────────────┬───────────────────────────────┬─────────────────┤
│ Navigation   │ Terminal: horizontal/vertical │ Tools           │
│ · Navigation │ ┌─────────────┬─────────────┐ │ · SFTP          │
│ · Sessions   │ │ tab-1       │ tab-2       │ │ · System status │
│ · Tabs       │ └─────────────┴─────────────┘ │ · Transfers     │
├──────────────┴───────────────────────────────┴─────────────────┤
│ Status bar: remote CPU / memory / disk / network (optional)     │
└────────────────────────────────────────────────────────────────┘
```

- Sidebar sections have six possible orders in Settings. Collapse state is saved in localStorage; width is draggable and used for overlay positioning.
- Tabs can appear above the terminal or in the sidebar. Their context menu copies the remote IP.
- Terminals support drag-to-split and resizable dividers.
- The right panel is resizable and switches between tools.
- Quick connect opens a session when you choose a server.
- The command palette is a global entry point; Esc closes it.

### Startup, close confirmations and motion

Settings → General includes Launch at login (off by default), Confirm before closing tabs (on), Confirm before quitting (on), and Smooth transitions (off, respects reduced motion). Batch tab closure prompts once; canceling preserves the sessions.

Login startup uses a per-user macOS LaunchAgent, Windows Run registry entry or Linux XDG autostart entry without administrator privileges. Enable it from a stable installation path; toggle it off/on after moving the application to update that path.

Press ⌘F on macOS or Ctrl+F on Windows/Linux to search the current terminal buffer, including retained scrollback. Match case, previous and next are supported. In the search input, Enter / Shift+Enter navigate; Esc closes and restores terminal focus. Search keystrokes are not sent remotely. Output already evicted by the scrollback limit cannot be searched.

## 5. Keyboard shortcuts

| Shortcut | Action | Scope |
| --- | --- | --- |
| Ctrl/Cmd+K | Toggle command palette; inactive while locked | Global |
| Ctrl/Cmd+W | Close current tab | Workspace |
| Ctrl+1 … Ctrl+9 | Select numbered tab when present | Global |
| Ctrl/Cmd+B | Toggle left navigation | Global |
| Ctrl/Cmd+Shift+B | Toggle right tools | Workspace |
| Esc | Close palette, completion panel or terminal sidebar | Global |
| ↑ / ↓ / Enter | Select / run command | Command palette |
| Ctrl+V / Ctrl+Shift+V / Cmd+V / Shift+Insert | Paste | Terminal |
| Ctrl+Shift+C / Cmd+C | Copy selection; no action without selection | Terminal |
| Ctrl+= / Ctrl+- / Ctrl+0 | Increase / decrease / reset font size to 13 | Terminal |
| F11 | Fullscreen | Terminal |
| Cmd+F / Ctrl+F | Search terminal output; Enter / Shift+Enter navigate, Esc closes | Terminal |
| Alt+↓, configurable | Enter completion navigation | Terminal |
| ↑ / ↓ / Tab / Enter | Select / insert suggestion | Completion panel |

Terminal copy/paste is intercepted in the capture phase to work around WebView2 accelerator restrictions. Ctrl+C remains SIGINT. Right-click can open a menu or paste directly. Completion shortcuts accept forms such as `Alt+ArrowDown` or `Ctrl+Shift+Space`; use a modifier combination, function / arrow key, Space, Tab, Escape or Enter.

## 6. Build from source

| Dependency | Requirement |
| --- | --- |
| Go | 1.25+; `go.mod` declares 1.25.0 |
| Node.js | 22.12+; `.nvmrc` pins 22.23.4 |
| Wails CLI | v2.13.0 |
| Linux | `libgtk-3-dev`, `libwebkit2gtk-4.1-dev`; build with `-tags webkit2_41` |
| Windows | WebView2 runtime; NSIS for installers |

```bash
# Install Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@v2.13.0

# Development with frontend hot reload and Go recompilation
wails dev

# Production output in build/bin/
wails build

# Windows installer: directory picker, with a UAC worker for protected-directory updates
wails build -platform windows/amd64 -nsis -installscope machine -webview2 embed

# Backend tests
go test ./internal/...

# Frontend dependencies, type checking, tests and build
cd frontend
npm ci
node node_modules/vue-tsc/bin/vue-tsc.js --noEmit
npm test
npm run build
```

Windows helper scripts refresh PATH in shells opened before Go / Node installation:

- Dot-source `. .\scripts\dev\dev-env.ps1` to add Go installations, project `.tools\go\bin`, `GOPATH\bin`, Node and npm to PATH. It can configure `GOPROXY` / `GOSUMDB`, returns to the repository root and prints tool versions.
- `scripts/dev/dev.cmd` wraps PowerShell with `-ExecutionPolicy Bypass`. Without arguments it opens an activated project shell; with arguments it runs a command, such as `.\scripts\dev\dev.cmd wails dev` or `.\scripts\dev\dev.cmd go test ./internal/...`.

| Packaging script | Output |
| --- | --- |
| `scripts/package-macos.sh` | `ding-ssh-<ver>-macos-<arch>.dmg` / `.app.zip`; arm64 for Apple Silicon, amd64 for Intel |
| `scripts/package-windows.ps1` | Windows amd64 `.zip` / `-setup.exe` |
| `scripts/package-linux.sh` | `.tar.gz`, `.deb` through nfpm with GTK/WebKit dependencies, `.AppImage` |
| `scripts/set-version.py` | Updates `info.productVersion` in `wails.json` |
| `scripts/debug.ps1` | Debug API helper; reads port/token from `debug.json` |

## 7. Code map

### 1.1.0 architecture

The frontend layering preserves business features and the workspace / servers / tunnels / settings navigation.

- **Design system:** `tokens.css → semantic.css → components.css`; Tailwind v4 maps semantic tokens, the theme engine writes semantic values, and terminal palettes live independently in `terminal.css`. Material Color Utilities HCT / Tonal Spot derives colors from the seed. `uiSchema=4` handles idempotent migration. Solid / Soft Glass / Clear Glass materials are documented in [theme-system.md](docs/theme-system.md).
- **Shell and domains:** App.vue assembles global state; layouts hold the sidebar, top bar and unlock screen; views hold the terminal workspace. Commands, servers, settings, terminals and tunnels use domain components. Transfer history remains a workspace side panel.
- **Shared interaction:** forms, dialogs, status and empty states live in `components/ui/`; composables handle themes, shortcuts, toasts, settings and completion. Dialogs and menus implement focus traps/restoration, arrow/Home/End/Esc handling and ARIA semantics.
- **Shared command contract:** the palette, `app.commands` and shortcuts use one registry. Stable `data-ui`, `data-tab-id` and business-ID `data-testid` selectors support automation without depending on CSS classes or child indexes.
- **Byte-safe terminal output:** completion is separated from TerminalView. ANSI/OSC/CSI control sequences pass through unchanged, including prompts without newlines such as oh-my-zsh / powerlevel10k.
- **Verification and isolation:** regression tests cover flows, migrations, accessibility, persistent terminals and colorization. `scripts/verify-ui2.sh` checks types, build, Go tests, migration guards and budgets. `DING_SSH_DEV_CONFIG_DIR` isolates data and test keys from the real keychain.

```text
main.go                    Wails entry, window options and embedded frontend/dist
internal/apphost/          Business facade, debug host and platform path adapters
internal/appdir/           Configuration directory and development isolation
internal/models/           Shared server/settings/event structures
internal/sshx/             SSH, SFTP cache/transfers, tunnels, OSC 7, metrics
internal/localterm/        Local shells through go-pty
internal/store/            SQLite WAL, JSON fallback, migrations, .dingpack
internal/cryptox/          AES-256-GCM, Argon2id and keychain
internal/debugsrv/         Routes, capabilities, confirmations, audit, MCP, automation
internal/i18n/             Native dialog and backend message translations
internal/logx/             Logging, rotation, redaction and tail reads
internal/logfilter/        Wails runtime log filtering
cmd/ding-ssh-mcp/          Standalone stdio MCP proxy
frontend/src/App.vue       Assembly, top-level state and navigation
frontend/src/layouts/      AppLayout / AppSidebar / AppTopBar / AppUnlock
frontend/src/views/        Workspace with persistent terminal instances
frontend/src/components/   Domain components and ui primitives
frontend/src/composables/  Themes, shortcuts, toasts, settings, completion
frontend/src/stores/       Pinia servers/sessions/settings/ui/credentials/groups/transfers
frontend/src/services/     Wails binding and event wrappers
frontend/src/completion/   Trie, FZF scoring and shortcuts
frontend/src/theme/        Tokens, migrations, presets and theme engine
frontend/src/locales/      Application translation catalogs
frontend/src/i18n.ts       Language preference and parameterized translations
frontend/src/panes/        Split layout calculations
frontend/src/debug/        Frontend observation and UI experiment bridge
scripts/                   Packaging, versioning, debug helpers and UI gates
build/                     Platform icons, Info.plist, nfpm and NSIS resources
docs/                      Design specification, plans, ADRs and acceptance records
```

The frontend calls App methods through Wails bindings for CRUD, settings, transfers, tunnels, sessions, security and migration. Backend EventsEmit pushes terminal output, session/connection/tunnel status. Debug subscribers and tracing receive the same stream through SSE and logs.

## 8. Data and security

Configuration lives in `os.UserConfigDir()/ding-ssh/`, such as `%AppData%\ding-ssh\` on Windows. An absolute `DING_SSH_DEV_CONFIG_DIR` isolates databases, legacy JSON, logs, snapshots, debug.json and local test keys; this mode does not read or create real system keychain entries. Relative paths are ignored. Do not set it for production use.

| File | Content |
| --- | --- |
| `ding-ssh.db` | WAL database: servers, settings, credentials, groups, command_history |
| `settings.json`, `servers.json`, `groups.json` | Legacy stores imported once into SQLite, then used only as fallback |
| `security.json`, `.master.key` | Master password metadata and local key fallback |
| `logs/` | Logs and ui-snapshots |
| `debug.json` | url, lanUrl, port, token, CDP port, options and PID; mode 0600; deleted when stopped; new token on each start |

Sensitive fields use AES-256-GCM with `enc1:` prefixes. Legacy plaintext fields migrate automatically at startup. Keys normally use `zalando/go-keyring`; a master password derives a 32-byte key with Argon2id (3 rounds, 64 MiB, 4 threads). `ensureUnlocked()` blocks operations that need a locked key. `.dingpack` contains password-encrypted JSON with magic `DINGPACK1`, including servers, credentials and groups, with a 500-server limit.

The app has no telemetry or automatic update checks. Fonts configured for online download may access their font sources. Debug ports are closed unless enabled, and bind to 127.0.0.1 by default. SSH verifies host trust; first-use fingerprints require confirmation, changed keys stop connections, and revoked keys are rejected. See the host trust section below.

Logs/audits always redact tokens, passwords and private keys, including key=value, token query strings, sensitive JSON, Authorization headers and PEM blocks. Diagnostics are also redacted. Capability changes apply immediately without replacing the port/token or disconnecting clients; only enabled/port/bindLan changes restart the listener.

## 9. Debug mode and AI control

Disabled by default. When enabled, the control interface listens on `127.0.0.1:<port>` (default 8765, optionally LAN). Find the address/token in Settings → Debug mode or debug.json. Requests require `Authorization: Bearer <token>`; `?token=` is also accepted.

### Capabilities and two-step confirmation

Capabilities authorize categories of work. Two-step confirmation authorizes a specific destructive action. Reads are always allowed; reversible/frequent terminal input, UI writes and eval require their capabilities but no confirmation token.

| Capability | Default | Purpose |
| --- | --- | --- |
| `read` | On, fixed | State, terminal buffers, logs, audit |
| `terminal.input` | On | Write terminal input |
| `secrets.read` | Off | Read sensitive fields |
| `eval` | Off | Execute page JavaScript |
| `ui.write` | Off | Change CSS, tokens, stores and synthetic events |
| `config.write` | Off | Servers, tunnels and settings |
| `secrets.write` | Off | Credential CRUD; also requires secrets.read |
| `fs.remote.write` | Off | Remote writes within a nonempty allowlist |
| `sudo.credential` | Off | sudo -i with a saved password; also requires secrets.read |
| `lifecycle` | Off | Reload, quit, clear logs |

Confirmation applies to `servers.create|update|delete`, `credentials.create|update|delete`, `tunnels.create|update|delete|start|stop`, `sftp.write|mkdir|rename|remove|upload`, `terminal.sudo`, `settings.update|reset`, `logs.clear`, `app.quit|reload`, configuration-changing `app.command`, and `capability.enable`. Consult the registry's `ConfirmableActions()` for the current set.

Tokens are single use, bound to action and arguments, and expire after 120 seconds by default:

```text
tools/call confirm.prepare  {"action":"servers.delete","args":{"id":"srv-1"}}
→ {"token":"cfm_...","expiresInMs":120000,"summary":"..."}
tools/call servers.delete   {"id":"srv-1","token":"cfm_..."}
```

HTTP callers use `?confirmToken=...`. Both interfaces share the confirmation manager and tokens. `app.permissions` returns the current capabilities and action descriptions. Sensitive capability grants themselves require confirmation.

### HTTP endpoints

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/v1/health` | Runtime status, port, switches |
| GET | `/v1/state` | Views, tabs, sessions, tunnels, settings and debug snapshot |
| GET / PUT | `/v1/settings` | Read / partially update settings immediately |
| GET | `/v1/servers` | Servers, redacted by default |
| GET | `/v1/sessions` | Backend sessions |
| GET | `/v1/terminals` | rows/cols, buffer lines, baseY/viewportY, scrollback reachability, visible text and DOM geometry |
| GET | `/v1/terminals/{id}/buffer?from=tail&lines=200` | Terminal buffer including scrollback |
| POST | `/v1/terminals/{id}/input` | `{data}` or `{base64}` |
| POST | `/v1/terminals/{id}/scroll` | `{to: top\|bottom\|line\|lines, value}` |
| POST | `/v1/terminals/{id}/resize` | `{cols, rows}`; emulator only, not remote PTY |
| POST | `/v1/sessions/{id}/reconnect` / `/disconnect` | Reconnect preserving buffer / disconnect preserving tab |
| POST / DELETE | `/v1/tabs` / `/v1/tabs/{id}` | Open `{serverId}`, `{node}` or `{local:true}` / close |
| POST | `/v1/eval` | Execute page JavaScript; requires eval |
| GET | `/v1/screenshot` | Visible window PNG |
| GET | `/v1/events?limit=&topics=` | Recent events, default 50, maximum 300 |
| GET | `/v1/stream?topics=ssh.output,ssh.status` | SSE topic subscriptions |
| GET | `/v1/logs` / `/v1/logs/tail` | Files/retention; tail with level, keyword and file filters |
| POST | `/v1/logs/clear` / `/export` / `/options` | Clear (lifecycle + token), diagnostics, options |
| GET | `/v1/tools` | Tool catalog shared with tools/list |
| GET / POST | `/v1/ui/*` | UI reads / parameterized reads and writes; writes require ui.write |
| GET / POST / DELETE | `/mcp` | Streamable HTTP MCP |

The tool catalog is authenticated like other read endpoints. It returns `name`, `capability[]`, `confirmAction?`, `needsToken`, `description`, `inputSchema`, `hasStructuredOutput` and `outputSchema?`.

The PowerShell helper reads debug.json automatically:

```powershell
.\scripts\debug.ps1 health
.\scripts\debug.ps1 state
.\scripts\debug.ps1 terminals
.\scripts\debug.ps1 servers
.\scripts\debug.ps1 settings
.\scripts\debug.ps1 buffer <id> [lines]
.\scripts\debug.ps1 input <id> "echo hi`r"
.\scripts\debug.ps1 scroll <id> <to> [val]
.\scripts\debug.ps1 resize <id> <cols> <rows>
.\scripts\debug.ps1 open <serverId> [-Local]
.\scripts\debug.ps1 close <id>
.\scripts\debug.ps1 reconnect <id>
.\scripts\debug.ps1 disconnect <id>
.\scripts\debug.ps1 eval "<js>"
.\scripts\debug.ps1 events [limit] [topics]
.\scripts\debug.ps1 stream [topics]
.\scripts\debug.ps1 screenshot [out.png]
.\scripts\debug.ps1 mcp tools|resources|call <tool> [json]
```

### MCP tools and resources

POST to `http://127.0.0.1:<port>/mcp`. The documented inventory is 89 tools: 63 domain-registered and 26 built in. Do not hard-code counts: query tools/list or GET /v1/tools.

| Domain | Documented count | Representative tools | Capabilities |
| --- | --- | --- | --- |
| Built-in state, terminals, settings, logs, permissions | 26 | app_state, list_terminals, read_terminal, send_input, update_settings, list_logs, screenshot, app.permissions, app.recent_calls, confirm.prepare/commit | read, terminal.input, config.write, lifecycle, eval |
| UI observation / experiments | 25 | ui.layout/query/styles/tree/console/screenshot/diff/assert/setToken/injectCSS/click/type | read; ui.write without token for reversible writes |
| Application control | 25 | servers.*, credentials.*, tunnels.*, settings.schema/reset, app.commands/command/quit | config.write, secrets.write, lifecycle; most writes require confirmation |
| SFTP / terminal automation | 13 | sftp.list/stat/read/transfers/write/mkdir/rename/remove/syncCwd/cancel, terminal.run/expect/sudo | read, fs.remote.write, terminal.input; sudo also needs sudo.credential |

Tools declaring outputSchema return `structuredContent` alongside text (MCP 2025-06-18). Existing content[0] text remains compatible with older clients.

| Resource URI | Type | Content |
| --- | --- | --- |
| `app://state`, `app://debug/info`, `app://servers`, `app://logs` | JSON | State, debug information, redacted servers, logging options |
| `app://logs/tail`, `terminal://{id}/buffer`, `ui://console` | text | Log tail, terminal buffer, frontend console |
| `ui://layout`, `ui://view` | JSON | Layout, view and navigation |
| `sftp://{sessionId}/cwd`, `transfers://current` | JSON | Current directory summary (first 20 entries), recent transfers |

Initialize returns `Mcp-Session-Id`. Supply it to GET /mcp for SSE and DELETE /mcp to close the session. Notifications use `notifications/message`, logger `ding-ssh/<topic>` (such as ssh.output, ssh.status, tunnel.status) and data.uri for cache invalidation. A `: ping` is sent every 20s. Specify `?topics=` on initialization to restrict subscriptions.

```bash
# Initialize and inspect response headers for Mcp-Session-Id
curl -s -D - -X POST http://127.0.0.1:8765/mcp \
  -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"curl","version":"0"}}}'

# Subscribe without buffering; Ctrl+C exits
curl -N -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SESSION" http://127.0.0.1:8765/mcp

# End the session
curl -X DELETE -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SESSION" http://127.0.0.1:8765/mcp
```

HTTP client configuration:

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

For stdio-only clients, build the proxy. It forwards stdin JSON-RPC to POST /mcp, writes responses/pushes to stdout and redacted logs to stderr:

```bash
go build -o ding-ssh-mcp ./cmd/ding-ssh-mcp
./ding-ssh-mcp -v
```

| Flag / variable | Meaning |
| --- | --- |
| `-url` / `DING_SSH_DEBUG_URL` | Base debug URL; a trailing /mcp is accepted |
| `-token` / `DING_SSH_DEBUG_TOKEN` | Token |
| `-debug-json` | Override debug.json path |
| `-v`, `-quiet`, `-once`, `-h` | Verbose forwarding, errors only, availability probe, usage |

Resolution order: explicit flags → environment → debug.json. The proxy subscribes to GET /mcp after receiving a session ID, reconnects with 1/2/5s backoff, and closes SSE / attempts DELETE on exit. After an application restart changes the token or port, it rereads debug.json on request failure unless explicit values were supplied.

### Terminal and SFTP automation

- `sftp.*` reuses the session's existing SFTP connection. Reads: list, stat (Lstat with symlink target), read (text/base64, default 256 KB limit, sha256), transfers. Writes: write (append=true supported), mkdir, rename, remove, syncCwd. Cancel with sftp.cancel.
- Remote write/delete/rename/mkdir paths must be absolute, contain no `..`, and match an allowlisted path-segment prefix: `/srv/app` permits itself and descendants, not `/srv/app-evil`. Empty allowlists reject all writes. Destructive writes require fs.remote.write and confirmation; remote changes cannot be undone through audit. Writes invalidate the SFTP cache.
- `terminal.run` sends a command with a trailing carriage return. With expect, waits for the pattern; otherwise waits for output silence (idleMs defaults to 400ms, with 1500ms grace before initial real output). Matching skips command echo, including wrapped long commands. `wrapExitMarker=true` uses POSIX `command; printf '\n__DING_EXIT__%s\n' "$?"` to capture the exit code.
- `terminal.expect` only waits and sends no input. sinceMarker skips previously seen content.
- `terminal.sudo` uses the saved password with sudo -i. The password reaches sudo -S through a random `/tmp/.ding-sudo-<12hex>` file with mode 600; it never appears in parameters/results/logs/audit. Cleanup uses shell rm+ls and independent SFTP Remove+Stat, including after command hangs/timeouts. Failure hints include the exact residual path and recovery advice. cleanedUp, cleanupAttempted, cleanupVerified and scrubbed report actual results. cleanupOnly accepts only this tool's generated path shape and performs idempotent cleanup without touching the terminal.
- Only one terminal.run/expect/sudo may operate per session at a time. Concurrent calls are rejected immediately rather than queued.

Prefer narrowly scoped passwordless sudo where possible, such as `deploy ALL=(root) NOPASSWD: /usr/bin/systemctl restart nginx` in `/etc/sudoers.d/ding-ai`. Then terminal.run can perform the task without sudo.credential. Stored-password elevation is for cases where this is unavailable and elevation is needed immediately.

### Audit and UI experiments

AI activity records every call, including rejected/failed calls: time, source mcp/http/ui, tool, capability, duration, result, redacted arguments and undo support. The ring buffer keeps 500 entries. Setting changes through update_settings/set_log_options can restore the previous value using the latest 20 snapshots. AI clients can inspect app.recent_calls.

UI reads include ui.layout (layout and unexpected overlaps), query, styles, tokens, hit, tree, console, revision, screenshot, elementShot, snapshot, diff and assert. Experiments require ui.write: setToken, injectCSS, storePatch, navigate, window, reload, click, type, key, hover and scroll. Experiments affect the current in-memory instance and revert on reload; they do not save settings.

Use semantic `[data-ui="topbar"]`, sidebar, terminal, context-panel, statusbar, command-palette and settings markers. Tabs use data-tab-id; server/command items use business-ID data-testid. Avoid CSS utility classes or child indexes.

ui.diff compares 16×16 pixel blocks using threshold, merges adjacent changes into up to 20 regions, and returns changedRatio/changedPixels with a red overlay. Fingerprint changes report only changed fields, including individual CSS properties. Snapshots live in logs/ui-snapshots with validated filenames.

## 10. Logs and diagnostics

Logs live in `<os.UserConfigDir()>/ding-ssh/logs`: `%AppData%\ding-ssh\logs` on Windows, `~/Library/Application Support/ding-ssh/logs` on macOS, usually `~/.config/ding-ssh/logs` on Linux.

- Current file: app-YYYYMMDD.log; rotates at 8 MB or the next day to app-YYYYMMDD-HHMMSS.log.
- Keeps at most 20 files, removing the oldest without deleting the active file.
- Tail reads default to 200 lines with a 4 MB maximum per read.

| Option | Behavior |
| --- | --- |
| Console / file logging | Independent switches |
| Log level | debug / info / warn / error; default info |
| MCP / debug API logging | Off by default; one [api] line per call with method, path, status, duration and response bytes; no query token |
| Open / clear logs | Clear only app-*.log, close handles first, then recreate according to switches |
| Export diagnostics | diagnostics-YYYYMMDD-HHMMSS.zip with the latest 3 logs, debug.json, state.json and env.json; individual entry failures do not prevent others |

The terminal context menu starts/stops session tracing. Output is copied to `[trace:<id>]` logs after ANSI removal, with entries truncated beyond 2,000 characters. This detailed tracing requires debug log level.

## 11. Known limitations

**WebGL scaling, fixed:** `.app-shell { zoom }` changes Chromium's ResizeObserver devicePixelContentBoxSize. At zoom=1.25, a 792×442 grid could produce a 990×553 bitmap while the addon only changed bitmap dimensions, leaving GL viewport/uniform/vertex normalization stale. This hid top lines even though buffer and DOM geometry were correct; refresh/clearTextureAtlas did not fix it.

`frontend/src/utils/xterm-webgl-zoom.ts` ignores zoomed observer dimensions when zoom differs from 1 and keeps handleResize's unscaled grid dimensions. It checks bitmap/grid equality after fit/scale changes and reruns handleResize when needed. WebGL now works at all scales. If addon private APIs change and correction is unavailable at non-100% scale, or if WebGL is unavailable/context is lost, rendering falls back to DOM with a message. `[webgl] enabled …` logs include uiScale/dpr/grid/bitmap/css/rect/cols×rows/viewport/baseY.

**Development runtime noise:** opening the wails dev URL in a browser can produce two `Unknown message from front end: runtime:ready` messages because Wails v2.13.0's dev server does not intercept the internal message. It does not affect production or the desktop window; internal/logfilter filters these messages precisely.

Other limits:

- SSH fingerprints require first-use confirmation; changed host keys interrupt connections. See host trust details below.
- Wails v2.13 overwrites WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS, preventing application-side CDP port injection. Page control uses /v1/eval; native screenshots depend on window visibility/occlusion.
- /v1/terminals/{id}/resize changes only emulator dimensions, not the remote PTY.
- Slow Hub subscribers can lose events; the Hub neither queues nor blocks publishers. This affects SSE and terminal automation. Inspect outputBytes/droppedOutput, or use read_terminal for complete buffered history.
- ui.reload remounts the frontend: terminals reconnect, DOM state/fingerprints become invalid and ui.revision increments. Reidentify elements; take ui.snapshot first to preserve evidence.
- Outside wails dev, rebuild the frontend with npm run build or wails build to include current ui.* bridge operations. A stale frontend may report unknown operations despite a healthy backend.
- wrapExitMarker uses POSIX semantics. For cmd/PowerShell, read the buffer or explicitly use `echo %ERRORLEVEL%` / `$LASTEXITCODE`.
- macOS packages are unsigned/unnotarized and may need right-click → Open.
- bindLan exposes the authenticated listener to the local network. Disable when unused.

## 12. CI and releases

| Workflow | Trigger | Actions |
| --- | --- | --- |
| ci.yml | Push to main/dev/master; all PRs | Go internal tests and frontend type checks; main/dev pushes additionally build three platforms and upload artifacts without a Release |
| release.yml | v* tags or manual | Tests, three-platform build/package, SHA256SUMS.txt and Release; manual runs upload artifacts only |

Unreleased builds use `0.0.0-dev.<sha>`, injected into About by CI.

```bash
git tag v1.0.0
git push origin v1.0.0
```

## 13. UI 2.0 design system

P0–P4 in docs/refactor-2.0 cover tokens, primitives, shell, domain pages, commands/shortcuts, stable DOM markers and performance gates. Cross-platform manual smoke tests and release signing follow the release checklist.

MD3's mobile-first tonal elevations and low density do not match this desktop workspace's server lists, SFTP tables and status bars. It would not change the rendering pipeline or improve rendering performance.

| Reference | Role | Decision |
| --- | --- | --- |
| W3C DTCG | Token format and source of truth | Adopted foundation |
| IBM Carbon | Enterprise density and mature dark themes | Primary architectural reference |
| WCAG 2.2 AA / WAI-ARIA APG | Accessibility and keyboard interaction | Required baseline |
| Microsoft Fluent 2 | Desktop density | Secondary reference |
| Material Design 3 | Mobile-first | Not adopted |
| Apple HIG | macOS shell | Platform-specific, not a universal standard |

The Grid Desk identity is expressed as explicit principles. The original 117 variables are organized into primitive → semantic → component layers; seven presets derive from semantic tokens. Compact density serves tables/lists/status bars; comfortable density serves settings/palette. Keyboard-first accessibility, reduced motion and consistent duration/easing tokens form the baseline.

Current implementation uses theme/tokens.css → semantic.css → components.css, with the theme engine writing semantic values. See [theme-system.md](docs/theme-system.md) for architecture, migration and validation. The palette implements dialog/listbox roles, looping navigation, focus protection and IME protection. Automation uses stable semantic IDs. Configurable fonts include their licenses, and scripts/verify-ui2.sh checks CSS/JS/font/dist budgets.

## 14. Documentation and licensing

- [CHANGELOG.md](CHANGELOG.md): completed changes, newest first, including symptoms, causes and fixes.
- [UI 2.0 specification](docs/2.0设计规范.md): visual and interaction principles and component rules.
- [Architecture documents](docs/refactor-2.0/): reviews, phased plans, ADRs, selector contracts and acceptance records.

The repository has no project-level license file. Linux packaging metadata lists MIT but is not a substitute for an explicit project license. Dependencies and bundled fonts retain their original licenses; font directories include OFL.txt.

## 1.1.1 updates

- Sidebar pinning and hover preview: after collapse, the icon rail remains. Hovering the logo for 500ms opens a preview; leaving the whole sidebar closes it after 250ms. Pinning restores permanent expansion. Preview overlays content without resizing terminals; menus, drag operations and keyboard interaction defer closing.
- Home shares the sidebar's glass base; server rows/groups use lighter blur, with corresponding settings/tunnel/dialog surfaces. Disabling glass restores solid backgrounds.
- Narrow layouts allow input shrinking and wrapping toolbars/action rows, preventing vertically stacked Chinese buttons.
- SSH host trust verification, up to five jump hosts, OpenSSH snapshot import and local Agent authentication are included below.

The original 1.1.1 validation record covered frontend types/build/40 tests, Go tests/race/static checks, macOS packaging and Windows/Linux SSH cross-compilation. Full cross-platform desktop and real remote end-to-end tests were not performed. The full UI2 gate still had a font resource budget overrun, so that record was not a complete UI2 pass.

### Host trust, jump hosts and OpenSSH import

First manual connections display key type and SHA256 fingerprint. Verify it before choosing Trust and connect. A changed key aborts the connection and opens an application dialog; no replacement occurs without explicit approval, and revoked keys are always rejected. Canceling saves no trust.

Settings → Security → SSH host trust lists application records with fingerprint copy/delete. For legitimate key rotations, verify the new fingerprint in the change dialog, select the verification checkbox and choose Replace current key and connect. Revoked records cannot be replaced. Read-only source conflicts can be superseded by an explicitly approved independent application record; source files stay unchanged. User ~/.ssh/known_hosts and server-specific trust sources are read only.

Server editing supports up to five saved jump hosts, with ordering. Each hop authenticates and verifies independently; referenced servers cannot be deleted directly. Terminal, SFTP, metrics and tunnel connections share the same chain without falling back to a direct connection.

Agent and automatic authentication use SSH_AUTH_SOCK on Linux/macOS and the native OpenSSH Agent pipe on Windows, with optional IdentityAgent. Public key fingerprints can be inspected. Agent forwarding is not enabled.

Import SSH Config provides preview, skip/copy/overwrite and field differences. It creates snapshots without rewriting source files; select jump dependencies together. SQLite import is transactional; JSON fallback uses atomic replacement. Automatic authentication prefers Agent signatures, supports multiple IdentityFile values and respects IdentitiesOnly. Encrypted identities request a temporary passphrase that is never saved or exported.

Databases automatically gain ssh_options while retaining IDs, groups, passwords and private keys. JSON/.dingpack preserve added SSH options. Application host trust is separate and excluded from packages.

Import supports Host aliases, default/wildcard/negated rules, first-value precedence, HostName, User, Port, IdentityFile, IdentityAgent, IdentitiesOnly, ProxyJump, HostKeyAlias, UserKnownHostsFile and depth/cycle-limited Include. ProxyJump currently requires explicit Host aliases in the same configuration, not inline user@host:port. ProxyCommand, LocalCommand and shell commands are never executed. Unsupported keywords require manual review; Match is rejected. Advanced CanonicalizeHostname behavior is not claimed. Relative Include paths resolve under ~/.ssh.

Unknown-host confirmation aborts the first handshake, asks for explicit approval, then reconnects; no login password is sent before approval. Headless calls and automatic reconnect never approve new trust. Windows/Linux adapters exist, while full desktop and remote environment validation still requires the respective platforms.

### Host trust and Windows title bar fixes

Trust/change dialogs display and copy full fingerprints. Backend requests expire in five minutes and are single use; confirmation rechecks revocation and concurrent record changes. Ordinary application records can be explicitly replaced; read-only sources use independent overrides without rewriting OpenSSH files. Trust search supports IP, hostname and fingerprint, with ten records per page and bounded height.

Windows uses an application title bar with dragging, double-click maximize, minimize, restore and close. Its material matches the macOS top bar and becomes solid when glass is disabled, avoiding a separate native Acrylic title surface. Platform window-control conventions remain. Windows runtime behavior still needs hardware verification.

Terminal text is checked against its actual background with at least 4.5:1 contrast protection, preventing unreadable light text in light themes. This affects display only, preserving remote output, copied text and server configuration.

The Windows drag region is separate from window buttons. The system handles the window outline without frontend rounded clipping. Light terminals disable text shadow; without backgrounds/glass they render opaquely, and Windows uses heavier text for readability. Light interfaces use bright solid surfaces when glass is off.

Quick connect uses an independent light surface to avoid backdrop darkening. Home fills an empty terminal page and supports narrow windows, scaling and vertical scrolling.

## 1.2.1 updates

- **Simplified Chinese and English**: Settings → General → Language switches in place without reloading the page or rebuilding terminal sessions. Pages, help text, connection and transfer messages, and native file dialog titles are translated; user content and remote terminal output are untouched.
- **JumpServer bastion mode**: a new toggle under server advanced settings. Terminal and SFTP directories stay independent, so you enter the target asset and account directory in SFTP before uploading, renaming or deleting; the panel no longer writes its path back into the terminal.
- **Automatic update channels**: Settings → About → Check for updates supports `main` and `dev`, each reading `update.json` from its own prerelease. Downloads are verified with SHA-256 and can be installed immediately or on exit. Branch builds are tagged with their channel so development builds are no longer mistaken for releases.
- **Tunnels**: reverse SOCKS5 (`-R`) forwarding; a client address can be selected when creating a tunnel, and listening/running state is shown live in the list.
- **Appearance and terminal**: dynamic theming moved to HCT tone mapping with Solid / Soft Glass / Clear Glass materials; the dark skeleton was lightened and re-layered, keeping the terminal Ink in the same hue family as navigation, toolbar and SFTP; terminal font presets can be downloaded and cached on demand.
- **Workflow**: startup and close preferences, terminal buffer search (`Cmd/Ctrl+F`), server configuration summary with collection switches, SFTP context menus, unified UI tokens and management boundaries, and focus traps with keyboard navigation in dialogs and menus.
- **Fixes**: garbled glyphs after switching tabs caused by the shared WebGL atlas; per-keystroke input not echoing in local terminals with zsh `%` prompts; the status bar freezing after closing the system board; drag-highlight colliding with the "ungrouped" sentinel; misaligned server rows and dialog focus in narrow windows; hardened MCP controls and transfer state.
- **Packaging**: macOS now ships separate Apple Silicon and Intel builds (`macos-arm64` / `macos-amd64`) instead of a Universal package.

Validation: Go `internal` tests pass; frontend `vue-tsc --noEmit` and `npm run build` pass with 51 test files and 228 tests. Installers for all three platforms are produced by the release workflow; real-remote and cross-platform desktop smoke tests were not run for this release.
