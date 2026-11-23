import { Stream } from '@wailsio/runtime'
import { isDesktop } from './desktop-environment'

export interface TerminalStreamClient {
  send(data: Uint8Array | string): void
  sendInteractive(data: string, command: string): void
  sendResize(cols: number, rows: number): void
  close(): void
  isOpen(): boolean
}

/**
 * openTerminalStream 为指定会话打开高效双向 Stream 连接。
 * 在桌面环境下通过 Wails v3 Stream 传输原始二进制数据，彻底去除 Base64 与事件队列开销。
 * 非桌面环境（如测试、浏览器预览）优雅返回 null，自动回退到常规 RPC/事件通信。
 */
export function openTerminalStream(
  sessionId: string,
  onOutput: (data: Uint8Array) => void,
  onStatusChange?: (open: boolean) => void,
): TerminalStreamClient | null {
  if (!isDesktop() || typeof window === 'undefined') {
    return null
  }

  try {
    const socket = Stream('terminal')
    let open = false

    socket.addEventListener('open', () => {
      open = true
      onStatusChange?.(true)
      socket.send(JSON.stringify({ action: 'attach', sessionId }))
    })

    socket.addEventListener('message', ((ev: Event) => {
      const messageEvent = ev as MessageEvent
      if (messageEvent.data instanceof ArrayBuffer) {
        onOutput(new Uint8Array(messageEvent.data))
      } else if (messageEvent.data instanceof Uint8Array) {
        onOutput(messageEvent.data)
      } else if (typeof messageEvent.data === 'string') {
        onOutput(new TextEncoder().encode(messageEvent.data))
      }
    }) as EventListener)

    socket.addEventListener('close', () => {
      open = false
      onStatusChange?.(false)
    })

    socket.addEventListener('error', () => {
      open = false
      onStatusChange?.(false)
    })

    return {
      send(data: Uint8Array | string) {
        if (!open) return
        socket.send(data)
      },
      sendInteractive(data: string, command: string) {
        if (!open) return
        socket.send(JSON.stringify({
          action: 'input_interactive',
          sessionId,
          data,
          command,
        }))
      },
      sendResize(cols: number, rows: number) {
        if (!open) return
        socket.send(JSON.stringify({
          action: 'resize',
          sessionId,
          cols,
          rows,
        }))
      },
      close() {
        open = false
        try {
          socket.close()
        } catch {
          // ignore
        }
      },
      isOpen() {
        return open
      },
    }
  } catch (err) {
    console.warn('[terminal-stream] 启动终端 Stream 失败，将使用事件降级通道:', err)
    return null
  }
}
