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
  Write,
} from '../../wailsjs/go/main/App'
import {EventsOn, EventsOff} from '../../wailsjs/runtime/runtime'
import type {
  ConnectResult,
  ProgressEvent,
  ServerNode,
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
  startTunnel: (node: ServerNode, name: string, localPort: number, remoteHost: string, remotePort: number): Promise<TunnelInfo> =>
    StartTunnel(node, name, localPort, remoteHost, remotePort),
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
  EventsOn(event, (payload: SFTPTransferEvent) => handler(payload))
  return () => EventsOff(event)
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
  EventsOn(event, (payload: { currentPath: string }) => handler(payload.currentPath))
  return () => EventsOff(event)
}

// 目录缓存更新事件（SWR 增量推送）
export function onSftpDirUpdated(sessionId: string, handler: (evt: { path: string; entries: SFTPEntry[] }) => void): () => void {
  const event = `sftp:dir-updated:${sessionId}`
  EventsOn(event, (payload: { path: string; entries: SFTPEntry[] }) => handler(payload))
  return () => EventsOff(event)
}
