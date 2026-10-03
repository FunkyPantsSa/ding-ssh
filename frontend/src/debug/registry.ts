// 调试模式：终端实例注册表。
//
// 调试 API（internal/debugsrv）需要 xterm 的内部状态（行列数、缓冲区、滚动位置）与
// DOM 几何（视口/屏幕高度），这些只有前端知道，因此 TerminalView 挂载时把自己注册进来，
// 由 src/debug/bridge.ts 统一响应后端的请求。
//
// 为什么条目存放在 globalThis 上，而不是模块级的 Map：
// Vite 的 HMR 会把「被改动模块的导入方」重新取一遍（URL 上带 ?t=<ts>），同一个 .ts 因此
// 可能在同一个页面里出现两份模块实例，各求值一次 —— 模块级 Map 也就变成了两份。
// 一旦 TerminalView 注册进 A、而 debug/bridge.ts 读的是 B，表现就是
// 「DOM 里终端明明挂着并连着，注册表却是空的」，而且再 HMR 多少次都不会自愈。
// 挂到 globalThis 后，无论模块重新求值多少次，读写始终落在同一份 Map 上。
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

/** 注册表在 globalThis 上的键：调试探针 / 回归断言可以直接用它拿到同一份数据。 */
export const DEBUG_REGISTRY_KEY = '__dingDebugRegistry'

interface DebugRegistryGlobal {
  [DEBUG_REGISTRY_KEY]?: Map<string, DebugTerminalEntry>
}

const globalScope = globalThis as unknown as DebugRegistryGlobal

// 第一次求值的模块实例负责创建，后续（HMR 产生的）实例一律复用，因此跨实例共享。
const entries: Map<string, DebugTerminalEntry> = globalScope[DEBUG_REGISTRY_KEY] ?? new Map()
globalScope[DEBUG_REGISTRY_KEY] = entries

export function registerDebugTerminal(entry: DebugTerminalEntry): void {
  entries.set(entry.clientId, entry)
}

/**
 * 注销终端。
 *
 * entry 是调用方自己登记的那一份：只有在它仍然是注册表里的当前项时才删除。
 * 为什么要这层身份判断：HMR 可能「先挂载新实例（写入新 entry）、后卸载旧实例」，
 * 旧实例的 onBeforeUnmount 用同一个 clientId 无条件 delete 会把活着的新终端一起删掉，
 * 于是注册表又空了。带上身份后，旧实例只会注销自己那一份。
 */
export function unregisterDebugTerminal(clientId: string, entry?: DebugTerminalEntry): void {
  if (entry && entries.get(clientId) !== entry) return
  entries.delete(clientId)
}

export function listDebugTerminals(): DebugTerminalEntry[] {
  return [...entries.values()]
}

export function getDebugTerminal(clientId: string | undefined): DebugTerminalEntry | undefined {
  if (!clientId) return undefined
  return entries.get(clientId)
}
