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

export type SessionStatus = 'connecting' | 'connected' | 'closed' | 'error'

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
  theme: Theme
}

// 常用凭证（对应 Go 端 models.Credential）。
export interface Credential {
  id: string
  name: string
  user: string
  password: string
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

// SSH 连接过程进度事件。
export interface ProgressEvent {
  sessionId: string
  step: string
}

// SSH 隧道信息（对应 Go 端 models.TunnelInfo）。
export interface TunnelInfo {
  id: string
  name: string
  serverId: string
  serverName: string
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
