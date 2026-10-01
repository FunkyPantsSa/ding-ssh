// 调试模式：终端实例注册表。
//
// 调试 API（internal/debugsrv）需要 xterm 的内部状态（行列数、缓冲区、滚动位置）与
// DOM 几何（视口/屏幕高度），这些只有前端知道，因此 TerminalView 挂载时把自己注册进来，
// 由 src/debug/bridge.ts 统一响应后端的请求。
import type {Terminal} from '@xterm/xterm'

export interface DebugTerminalEntry {
  /** 标签 clientId（同时作为会话 id 使用）。 */
  clientId: string
  /** 展示名（服务器名 / 本机）。 */
  title: string
  kind: string
  term: Terminal
  /** 终端容器（absolute inset-0 那层），用于测量几何。 */
  container: () => HTMLElement | null
  /** 向会话写入数据（走应用原有通道，含连接态/冻结态校验）。 */
  write: (data: string) => void
  /** 触发重连（复用标签自身的重连逻辑，保留缓冲区）。 */
  reconnect: () => void
  /** 关闭标签。 */
  close: () => void
}

const entries = new Map<string, DebugTerminalEntry>()

export function registerDebugTerminal(entry: DebugTerminalEntry): void {
  entries.set(entry.clientId, entry)
}

export function unregisterDebugTerminal(clientId: string): void {
  entries.delete(clientId)
}

export function listDebugTerminals(): DebugTerminalEntry[] {
  return [...entries.values()]
}

export function getDebugTerminal(clientId: string | undefined): DebugTerminalEntry | undefined {
  if (!clientId) return undefined
  return entries.get(clientId)
}
