// 封装 Wails 自动生成的绑定与事件，统一供各组件使用。
import {
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
  SelectSavePath,
  SftpCancelTransfer,
  SftpDownload,
  SftpList,
  SftpMkdir,
  SftpRemove,
  SftpRename,
  SftpUpload,
  StartTunnel,
  StopTunnel,
  TestServer,
  TestServers,
  UpdateTunnel,
  Write,
} from '../../wailsjs/go/main/App'
import {EventsOn, EventsOff} from '../../wailsjs/runtime/runtime'
import type {
  ConnectResult,
  ProgressEvent,
  ServerNode,
  ServerTestResult,
  SFTPEntry,
  SFTPTransferEvent,
  StatusEvent,
  TunnelInfo,
  TunnelStatusEvent,
} from '../types'

export const sshService = {
  getServers: (): Promise<ServerNode[]> => GetServers(),
  saveServer: (node: ServerNode): Promise<ServerNode> => SaveServer(node),
  deleteServer: (id: string): Promise<void> => DeleteServer(id),
  testServer: (node: ServerNode): Promise<ServerTestResult> => TestServer(node),
  testServers: (nodes: ServerNode[]): Promise<ServerTestResult[]> => TestServers(nodes),
  selectKeyFile: (): Promise<string> => SelectKeyFile(),
  selectImageFile: (): Promise<string> => SelectImageFile(),
  sftpList: (sessionID: string, path: string): Promise<SFTPEntry[]> =>
    SftpList(sessionID, path),
  selectLocalFile: (): Promise<string> => SelectLocalFile(),
  selectLocalFiles: (): Promise<string[]> => SelectLocalFiles(),
  selectSavePath: (defaultName: string): Promise<string> => SelectSavePath(defaultName),
  sftpUpload: (sessionID: string, localPath: string, remotePath: string): Promise<void> =>
    SftpUpload(sessionID, localPath, remotePath),
  sftpDownload: (sessionID: string, remotePath: string, localPath: string): Promise<void> =>
    SftpDownload(sessionID, remotePath, localPath),
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
  ): Promise<TunnelInfo> => StartTunnel(node, name, mode, localPort, remoteHost, remotePort),
  updateTunnel: (
    id: string,
    node: ServerNode,
    name: string,
    mode: string,
    localPort: number,
    remoteHost: string,
    remotePort: number,
  ): Promise<TunnelInfo> => UpdateTunnel(id, node, name, mode, localPort, remoteHost, remotePort),
  stopTunnel: (id: string): Promise<void> => StopTunnel(id),
  restartTunnel: (id: string): Promise<void> => RestartTunnel(id),
  removeTunnel: (id: string): Promise<void> => RemoveTunnel(id),
  listTunnels: (): Promise<TunnelInfo[]> => ListTunnels(),
  connect: (sessionID: string, node: ServerNode, cols: number, rows: number): Promise<ConnectResult> =>
    Connect(sessionID, node, cols, rows),
  disconnect: (sessionId: string): Promise<void> => Disconnect(sessionId),
  write: (sessionId: string, dataBase64: string): Promise<void> =>
    Write(sessionId, dataBase64),
  resize: (sessionId: string, cols: number, rows: number): Promise<void> =>
    Resize(sessionId, cols, rows),
  // Phase 2: SFTP→Shell 目录同步
  syncSftpToTerminal: (sessionId: string, path: string): Promise<void> =>
    (window as any)?.go?.main?.App?.SyncSftpToTerminal(sessionId, path),
}

export function onSessionOutput(sessionId: string, handler: (data: string) => void): () => void {
  const event = `ssh:output:${sessionId}`
  EventsOn(event, (payload: {data: string}) => handler(payload.data))
  return () => EventsOff(event)
}

export function onSessionStatus(sessionId: string, handler: (evt: StatusEvent) => void): () => void {
  const event = `ssh:status:${sessionId}`
  EventsOn(event, (payload: StatusEvent) => handler(payload))
  return () => EventsOff(event)
}

export function onSessionProgress(sessionId: string, handler: (evt: ProgressEvent) => void): () => void {
  const event = `ssh:progress:${sessionId}`
  EventsOn(event, (payload: ProgressEvent) => handler(payload))
  return () => EventsOff(event)
}

export function onTunnelStatus(handler: (evt: TunnelStatusEvent) => void): () => void {
  const event = 'tunnel:status'
  EventsOn(event, (payload: TunnelStatusEvent) => handler(payload))
  return () => EventsOff(event)
}

export function onSftpTransfer(sessionId: string, handler: (evt: SFTPTransferEvent) => void): () => void {
  const event = `sftp:transfer:${sessionId}`
  EventsOn(event, (...args: unknown[]) => {
    const evt = normalizeTransferEvent(args.length === 1 ? args[0] : args)
    if (evt) handler(evt)
  })
  return () => EventsOff(event)
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
  // 使用 window['go']['main']['App']['Reconnect'] 直接调用，避免 Wails 绑定未生成时出错
  return (window as any)?.go?.main?.App?.Reconnect(sessionId, cols, rows)
}

// 目录同步事件（Shell -> SFTP）
export function onSftpSyncPath(sessionId: string, handler: (path: string) => void): () => void {
  const event = `sftp:sync-path:${sessionId}`
  EventsOn(event, (...args: unknown[]) => {
    const p = extractSyncPath(args.length === 1 ? args[0] : args)
    console.info('[sftp-sync] 收到事件', event, args, '→', p)
    handler(p)
  })
  return () => EventsOff(event)
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
  EventsOn(event, (payload: { path: string; entries: SFTPEntry[] }) => handler(payload))
  return () => EventsOff(event)
}
