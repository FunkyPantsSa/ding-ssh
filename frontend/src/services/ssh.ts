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
  SelectSavePath,
  SftpCancelTransfer,
  SftpDownload,
  SftpList,
  SftpUpload,
  StartTunnel,
  StopTunnel,
  Write,
} from '../../wailsjs/go/main/App'
import {EventsOff, EventsOn} from '../../wailsjs/runtime/runtime'
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
  selectSavePath: (defaultName: string): Promise<string> => SelectSavePath(defaultName),
  sftpUpload: (sessionID: string, localPath: string, remotePath: string): Promise<void> =>
    SftpUpload(sessionID, localPath, remotePath),
  sftpDownload: (sessionID: string, remotePath: string, localPath: string): Promise<void> =>
    SftpDownload(sessionID, remotePath, localPath),
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
