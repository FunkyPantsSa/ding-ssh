// 与 Go 端 models 包对应的类型定义。

export interface ServerNode {
  id: string
  name: string
  group: string // 分组名，空为未分组
  host: string
  port: number
  user: string
  authType: string // 'password' | 'privateKey'
  password?: string
  keyPath?: string
  keyContent?: string // 直接粘贴的私钥内容，优先于 keyPath
  bgImage: string
  blurAmount: number
  envVars: Record<string, string>
}

export interface ConnectResult {
  sessionId: string
  server: string
}

// 服务器在线状态测试结果（对应 Go 端 models.ServerTestResult）。
export interface ServerTestResult {
  nodeId: string
  latencyMs: number // -1 表示连接失败
  reachable: boolean // SSH 端口是否畅通
  error?: string
}

export type SessionStatus = 'connecting' | 'connected' | 'closed' | 'error' | 'disconnected'

export interface SessionInfo {
  sessionId: string
  serverName: string
  host: string
  user: string
  status: string
  createdAt: number
}

export interface OutputEvent {
  sessionId: string
  data: string // base64
}

export interface StatusEvent {
  sessionId: string
  status: SessionStatus
  message?: string
}

// 前端会话标签（连接建立前尚无后端 sessionId）。
export interface SessionTab {
  clientId: string
  sessionId: string
  serverName: string
  host: string
  user: string
  node: ServerNode
  status: SessionStatus
  message?: string
  createdAt: number
  sftpPath: string
  /** ssh=远程服务器；local=本机终端 */
  kind?: 'ssh' | 'local'
}

// 终端主题设置（对应 Go 端 models.Theme）。
export interface Theme {
  background: string
  foreground: string
  cursor: string
  selection: string
  bgImage: string
  blurAmount: number
  textShadow: boolean
  shadowBlur: number
}

// 应用设置（对应 Go 端 models.Settings）。
export interface Settings {
  logEnabled: boolean
  copyOnSelect: boolean // 终端选中内容自动复制到剪贴板
  webGLEnabled: boolean // 优先 WebGL 渲染
  completionEnabled: boolean // 智能命令补全
  completionNavHotkey: string // 补全导航开/关热键，如 Alt+ArrowDown
  completionPanelLimit: number // 补全面板最多条数，默认 8
  sftpToTerminalSync: boolean // SFTP 目录变化是否同步到终端（发 cd 命令），默认开启
  terminalToSftpSync: boolean // 终端目录变化是否同步到 SFTP 面板，默认开启
  uiScale: number // 界面缩放百分比，默认 100（80–150）
  theme: Theme
  autoReconnect: boolean // 断开后自动重连，默认开启
  keepAliveEnabled: boolean // 心跳包防终端超时，默认开启
  localShell: string // 本机终端：darwin zsh|bash；windows powershell|cmd；linux default
}

// 本机 Shell 选项（对应 Go 端 localterm.ShellOption）。
export interface LocalShellOption {
  value: string
  label: string
}

// 命令补全候选（对应 Go 端 models.CommandSuggestion）。
export interface CommandSuggestion {
  command: string
  count: number
  source: string // history | dict | screen
}

// 常用凭证（对应 Go 端 models.Credential）。
export interface Credential {
  id: string
  name: string
  user: string
  password?: string
  authType: string // 'password' | 'privateKey'
  keyPath?: string
  keyContent?: string
}

// SFTP 远程目录条目（对应 Go 端 models.SFTPEntry）。
export interface SFTPEntry {
  name: string
  path: string
  isDir: boolean
  size: number
  modTime: number
}

// SFTP 传输进度事件（对应 Go 端 models.SFTPTransferEvent）。
export interface SFTPTransferEvent {
  sessionId: string
  direction: 'upload' | 'download'
  name: string
  transferred: number
  total: number
  done: boolean
  error?: string
}

// SSH 连接过程进度事件：每个事件代表一次步骤状态变更或一条步骤内详细日志，
// 前端按 step 聚合日志，支持展开排查卡住原因。
export interface ProgressEvent {
  sessionId: string
  step: string // dns | tcp | auth | pty | ready
  status: string // running | done | error
  log?: string // 追加到该步骤的一条详细日志
  message?: string // 步骤结束/失败时的摘要（错误时用于错误提示）
}

// SSH 隧道信息（对应 Go 端 models.TunnelInfo）。
export interface TunnelInfo {
  id: string
  name: string
  serverId: string
  serverName: string
  mode: string // local | remote | dynamic
  localPort: number
  remoteHost: string
  remotePort: number
  status: string // running | stopped | error
  message?: string
  startedAt: number
}

// SSH 隧道状态事件（对应 Go 端 models.TunnelStatusEvent）。
export interface TunnelStatusEvent {
  id: string
  status: string // running | stopped | error
  message?: string
}

// 系统监控快照。
export interface DiskInfo {
  mountPoint: string
  totalGb: number
  usedGb: number
  usagePct: number
}

export interface NetIface {
  name: string
  ip?: string
  rxMbps: number
  txMbps: number
}

export interface SysInfoSnapshot {
  sessionId: string
  cpuUsage: number
  memUsedMb: number
  memTotalMb: number
  diskUsage: DiskInfo[]
  netIfaces: NetIface[]
  uptime: string
  collectedAt: number
  error?: string
}

// 安全状态。
export interface SecurityStatus {
  unlocked: boolean
  masterPasswordEnabled: boolean
  keyringAvailable: boolean
  needsUnlock: boolean
}

export interface ImportConfigResult {
  servers: number
  credentials: number
  groups: number
}

// ---- Phase 2 增量类型 ----

// 目录同步事件（Shell <-> SFTP 双向联动）
export interface DirSyncEvent {
  sessionId: string
  currentPath: string
  source: string // "terminal" or "sftp"
}

// 缓存目录更新事件（SWR 增量推送）
export interface DirCacheUpdateEvent {
  sessionId: string
  path: string
  entries: SFTPEntry[]
}

// 限速配置
export interface RateLimitConfig {
  enabled: boolean
  bytesPerSec: number // 字节/秒，默认 10MB = 10 * 1024 * 1024
}
