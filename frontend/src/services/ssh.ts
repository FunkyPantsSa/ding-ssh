import {errorText} from '../i18n'
import {t} from '../i18n'
import {requestHostTrust} from './hostTrust'
import {requestPassphrase} from './passphrase'

// 封装 Wails 自动生成的绑定与事件，统一供各组件使用。
import {
  ConnectAuthenticated,
  TestServerLoginAuthenticated,
  SyncSftpToTerminal,
  RevealLocalPath,
  Reconnect,
  ResolveHostTrust,
  Connect,
  DeleteServer,
  Disconnect,
  GetServers,
  ListTunnels,
  RemoveTunnel,
  Resize,
  RestartTunnel,
  SaveServer,
  SelectKeyFile,
  SelectImageFile,
  SelectLocalFile,
  SelectLocalFiles,
  SelectLocalFolder,
  SelectSaveFolder,
  SelectSavePath,
  LocalPathIsDir,
  SftpCancelTransfer,
  SftpDownload,
  SftpDownloadFolder,
  SftpList,
  SftpMkdir,
  SftpRemove,
  SftpRename,
  SftpUpload,
  SftpUploadFolder,
  StartTunnel,
  StopTunnel,
  TestServer,
  TestServerLogin,
  TestServers,
  UpdateTunnelWithListenHost,
  StartTunnelWithListenHost,
  ListLocalIPv4,
  Write,
  WriteInteractive,
  SubmitSudoPassword,
  CancelSudoPrompt,
  ConnectLocal,
  GetPlatform,
  GetLocalShellOptions,
} from '../../bindings/ding-ssh/internal/apphost/app'
import {EventsOn} from './desktop'
import type {
  ConnectResult,
  LocalShellOption,
  ProgressEvent,
  ServerNode,
  ServerTestResult,
  SFTPEntry,
  SFTPTransferEvent,
  StatusEvent,
  SudoPromptEvent,
  TunnelInfo,
  TunnelStatusEvent,
} from '../types'

export const sshService = {
  getServers: (): Promise<ServerNode[]> => GetServers(),
  saveServer: (node: ServerNode): Promise<ServerNode> => SaveServer(node),
  deleteServer: (id: string): Promise<void> => DeleteServer(id),
  testServer: (node: ServerNode): Promise<ServerTestResult> => TestServer(node),
  testServerLogin: (node: ServerNode): Promise<ServerTestResult> => withAuthentication(phrases => withHostTrustResult(() => TestServerLoginAuthenticated(node, phrases))),
  testServers: (nodes: ServerNode[]): Promise<ServerTestResult[]> => TestServers(nodes),
  selectKeyFile: (): Promise<string> => SelectKeyFile(),
  selectImageFile: (): Promise<string> => SelectImageFile(),
  sftpList: (sessionID: string, path: string): Promise<SFTPEntry[]> =>
    SftpList(sessionID, path),
  selectLocalFile: (): Promise<string> => SelectLocalFile(),
  selectLocalFiles: (): Promise<string[]> => SelectLocalFiles(),
  selectLocalFolder: (): Promise<string> => SelectLocalFolder(),
  localPathIsDir: (path: string): Promise<boolean> => LocalPathIsDir(path),
  selectSavePath: (defaultName: string): Promise<string> => SelectSavePath(defaultName),
  sftpUpload: (sessionID: string, localPath: string, remotePath: string): Promise<void> =>
    SftpUpload(sessionID, localPath, remotePath),
  // 上传文件夹：后端保留本地文件夹名，作为 remoteDir 下的子目录递归上传。
  sftpUploadFolder: (sessionID: string, localDir: string, remoteDir: string): Promise<void> =>
    SftpUploadFolder(sessionID, localDir, remoteDir),
  sftpDownload: (sessionID: string, remotePath: string, localPath: string): Promise<void> =>
    SftpDownload(sessionID, remotePath, localPath),
  // 下载文件夹：后端保留远端文件夹名，作为 localDir 下的子目录递归下载。
  sftpDownloadFolder: (sessionID: string, remoteDir: string, localDir: string): Promise<void> =>
    SftpDownloadFolder(sessionID, remoteDir, localDir),
  selectSaveFolder: (): Promise<string> => SelectSaveFolder(),
  sftpMkdir: (sessionID: string, path: string): Promise<void> =>
    SftpMkdir(sessionID, path),
  sftpRename: (sessionID: string, oldPath: string, newPath: string): Promise<void> =>
    SftpRename(sessionID, oldPath, newPath),
  sftpRemove: (sessionID: string, path: string): Promise<void> =>
    SftpRemove(sessionID, path),
  sftpCancelTransfer: (sessionID: string, direction: string, name: string): Promise<void> =>
    SftpCancelTransfer(sessionID, direction, name),
  startTunnel: (
    node: ServerNode,
    name: string,
    mode: string,
    localPort: number,
    remoteHost: string,
    remotePort: number,
    listenHost = '',
  ): Promise<TunnelInfo> => withAuthentication(phrases => withHostTrust(() =>
    StartTunnelWithListenHost(node, name, mode, localPort, remoteHost, remotePort, listenHost, phrases))),
  updateTunnel: (
    id: string,
    node: ServerNode,
    name: string,
    mode: string,
    localPort: number,
    remoteHost: string,
    remotePort: number,
    listenHost = '',
  ): Promise<TunnelInfo> => withAuthentication(phrases => withHostTrust(() =>
    UpdateTunnelWithListenHost(id, node, name, mode, localPort, remoteHost, remotePort, listenHost, phrases))),
  listLocalIPv4: (): Promise<string[]> => ListLocalIPv4(),
  stopTunnel: (id: string): Promise<void> => StopTunnel(id),
  restartTunnel: (id: string): Promise<void> => RestartTunnel(id),
  removeTunnel: (id: string): Promise<void> => RemoveTunnel(id),
  listTunnels: (): Promise<TunnelInfo[]> => ListTunnels(),
  connect: (sessionID: string, node: ServerNode, cols: number, rows: number, interactive = true): Promise<ConnectResult> =>
    interactive ? withAuthentication(phrases => withHostTrust(() => ConnectAuthenticated(sessionID, node, cols, rows, phrases))) : Connect(sessionID,node,cols,rows),
  connectLocal: (sessionID: string, cols: number, rows: number): Promise<ConnectResult> =>
    ConnectLocal(sessionID, cols, rows),
  getPlatform: (): Promise<string> => GetPlatform(),
  getLocalShellOptions: (): Promise<LocalShellOption[]> => GetLocalShellOptions(),
  disconnect: (sessionId: string): Promise<void> => Disconnect(sessionId),
  write: (sessionId: string, dataBase64: string): Promise<void> =>
    Write(sessionId, dataBase64),
  writeInteractive: (sessionId: string, dataBase64: string, command = ''): Promise<void> =>
    WriteInteractive(sessionId, dataBase64, command),
  submitSudoPassword: (sessionId: string, requestId: string, password: string, useSaved: boolean): Promise<void> =>
    SubmitSudoPassword(sessionId, requestId, password, useSaved),
  cancelSudoPrompt: (sessionId: string, requestId: string): Promise<void> =>
    CancelSudoPrompt(sessionId, requestId),
  resize: (sessionId: string, cols: number, rows: number): Promise<void> =>
    Resize(sessionId, cols, rows),
  // Phase 2: SFTP→Shell 目录同步
  syncSftpToTerminal: (sessionId: string, path: string): Promise<void> =>
    SyncSftpToTerminal(sessionId, path),
  // 在系统文件管理器中打开本地文件 / 目录（SFTP 下载完成后「打开所在文件夹」）。
  revealLocalPath: (path: string): Promise<void> => RevealLocalPath(path),
}

export function onSessionOutput(sessionId: string, handler: (data: string) => void): () => void {
  const event = `ssh:output:${sessionId}`
  return EventsOn(event, (payload: {data: string}) => handler(payload.data))
}

export function onSessionStatus(sessionId: string, handler: (evt: StatusEvent) => void): () => void {
  const event = `ssh:status:${sessionId}`
  return EventsOn(event, (payload: StatusEvent) => handler(payload))
}

export function onSudoPrompt(sessionId: string, handler: (evt: SudoPromptEvent) => void): () => void {
  const event = `ssh:sudo:${sessionId}`
  return EventsOn(event, (payload: SudoPromptEvent) => handler(payload))
}

export function onSessionProgress(sessionId: string, handler: (evt: ProgressEvent) => void): () => void {
  const event = `ssh:progress:${sessionId}`
  return EventsOn(event, (payload: ProgressEvent) => handler(payload))
}

export function onTunnelStatus(handler: (evt: TunnelStatusEvent) => void): () => void {
  const event = 'tunnel:status'
  return EventsOn(event, (payload: TunnelStatusEvent) => handler(payload))
}

export function onSftpTransfer(sessionId: string, handler: (evt: SFTPTransferEvent) => void): () => void {
  const event = `sftp:transfer:${sessionId}`
  return EventsOn(event, (payload: unknown) => {
    const evt = normalizeTransferEvent(payload)
    if (evt) handler(evt)
  })
}

function normalizeTransferEvent(payload: unknown): SFTPTransferEvent | null {
  if (Array.isArray(payload) && payload.length >= 3 && (payload[1] === 'upload' || payload[1] === 'download')) {
    return {
      sessionId: String(payload[0] ?? ''),
      direction: payload[1],
      name: String(payload[2] ?? ''),
      transferred: Number(payload[3] ?? 0) || 0,
      total: Number(payload[4] ?? 0) || 0,
      done: Boolean(payload[5]),
      error: payload[6] ? String(payload[6]) : undefined,
    }
  }
  const obj = unwrapEventObject(payload)
  if (!obj) return null
  const direction = String(obj.direction ?? obj.Direction ?? '')
  const name = String(obj.name ?? obj.Name ?? '')
  if ((direction !== 'upload' && direction !== 'download') || !name) return null
  return {
    transferId: typeof (obj.transferId ?? obj.TransferID) === 'string' ? String(obj.transferId ?? obj.TransferID) : undefined,
    sessionId: String(obj.sessionId ?? obj.SessionID ?? ''),
    direction,
    name,
    transferred: Number(obj.transferred ?? obj.Transferred ?? 0) || 0,
    total: Number(obj.total ?? obj.Total ?? 0) || 0,
    done: Boolean(obj.done ?? obj.Done),
    error: obj.error || obj.Error ? String(obj.error ?? obj.Error) : undefined,
  }
}

function unwrapEventObject(payload: unknown): Record<string, unknown> | null {
  if (Array.isArray(payload)) {
    for (const item of payload) {
      const obj = unwrapEventObject(item)
      if (obj) return obj
    }
    return null
  }
  if (payload && typeof payload === 'object') return payload as Record<string, unknown>
  return null
}

// base64 -> Uint8Array，用于 xterm.write
export function base64ToBytes(b64: string): Uint8Array {
  const bin = atob(b64)
  const bytes = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i)
  return bytes
}

// ---- Phase 2 增量 API ----

// 重新连接已断开的 SSH 会话
export function reconnect(sessionId: string, cols: number, rows: number): Promise<ConnectResult> {
  return Reconnect(sessionId, cols, rows)
}

// 目录同步事件（Shell -> SFTP）
export function onSftpSyncPath(sessionId: string, handler: (path: string) => void): () => void {
  const event = `sftp:sync-path:${sessionId}`
  return EventsOn(event, (payload: unknown) => {
    const p = extractSyncPath(payload)
    console.info(t("[sftp-sync] 收到事件"), event, payload, '→', p)
    handler(p)
  })
}

function extractSyncPath(payload: unknown): string {
  if (typeof payload === 'string') {
    const s = payload.trim()
    if (s.startsWith('/')) return s
    try {
      const parsed: unknown = JSON.parse(s)
      if (parsed && typeof parsed === 'object') return extractSyncPath(parsed)
    } catch {
      return ''
    }
    return ''
  }
  if (Array.isArray(payload)) {
    for (const item of payload) {
      const p = extractSyncPath(item)
      if (p) return p
    }
    return ''
  }
  if (payload && typeof payload === 'object') {
    const o = payload as Record<string, unknown>
    for (const key of ['currentPath', 'CurrentPath', 'path', 'Path']) {
      const v = o[key]
      if (typeof v === 'string' && (v.startsWith('/') || v.startsWith('~'))) return v
    }
    for (const v of Object.values(o)) {
      if (typeof v === 'string' && v.startsWith('/')) return v
    }
  }
  return ''
}

// 目录缓存更新事件（SWR 增量推送）
export function onSftpDirUpdated(sessionId: string, handler: (evt: { path: string; entries: SFTPEntry[] }) => void): () => void {
  const event = `sftp:dir-updated:${sessionId}`
  return EventsOn(event, (payload: { path: string; entries: SFTPEntry[] }) => handler(payload))
}

// Only explicit UI actions call this wrapper; reconnect and backend API calls
// retain fail-closed behavior for unknown hosts.
async function approveUnknownHost(error: unknown): Promise<boolean> {
  const match = errorText(error).match(/SSH_HOST_TRUST:([A-Za-z0-9_-]+)/)
  if (!match) return false
  const raw = match[1]!.replace(/-/g, '+').replace(/_/g, '/')
  const bytes = Uint8Array.from(atob(raw), c => c.charCodeAt(0))
  const challenge = JSON.parse(new TextDecoder().decode(bytes))
  const approved = await requestHostTrust(challenge)
  await ResolveHostTrust(challenge.id, approved, approved && challenge.changed)
  return approved
}
async function withHostTrust<T>(action: () => Promise<T>): Promise<T> {
  // Bounded retries also support a chain with up to five intermediate hosts.
  for (let attempt = 0; ; attempt++) {
    try { return await action() }
    catch (error) {
      if (attempt >= 6 || !await approveUnknownHost(error)) throw error
    }
  }
}
async function withHostTrustResult(action: () => Promise<ServerTestResult>): Promise<ServerTestResult> {
  for (let attempt = 0; ; attempt++) {
    const result = await action()
    if (result.reachable || attempt >= 6 || !await approveUnknownHost(result.error)) return result
  }
}

async function withAuthentication<T>(action: (phrases: Record<string, string>) => Promise<T>): Promise<T> {
 const phrases: Record<string, string> = {}
 try {
  for (let attempt = 0; ; attempt++) {
   let result: T | undefined
   let failure: unknown
   try { result = await action(phrases); failure = (result as any)?.error }
   catch (error) { failure = error }
   const match = String(failure ?? '').match(/SSH_KEY_PASSPHRASE:([A-Za-z0-9_-]+)/)
   if (!match || attempt >= 6) { if (result !== undefined) return result; throw failure }
   const raw = match[1]!.replace(/-/g, '+').replace(/_/g, '/')
   const path = new TextDecoder().decode(Uint8Array.from(atob(raw), c => c.charCodeAt(0)))
   const value = await requestPassphrase(path)
   if (value === null) { if (result !== undefined) return result; throw new Error(t("已取消私钥解锁")) }
   phrases[path] = value
  }
 } finally { for (const path of Object.keys(phrases)) delete phrases[path] }
}
