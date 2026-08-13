// Zmodem (rz/sz) 会话接管：基于 zmodem.js Sentry。
import type {Terminal} from '@xterm/xterm'
import {base64ToBytes, sshService} from './ssh'

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AnyZ = any

export interface ZmodemProgress {
  name: string
  direction: 'upload' | 'download'
  transferred: number
  total: number
  done: boolean
  error?: string
}

export interface ZmodemController {
  consume: (bytes: Uint8Array) => void
  dispose: () => void
  active: () => boolean
}

function bytesToBase64(bytes: Uint8Array | number[]): string {
  const arr = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes)
  let binary = ''
  const chunk = 0x8000
  for (let i = 0; i < arr.length; i += chunk) {
    binary += String.fromCharCode(...arr.subarray(i, i + chunk))
  }
  return btoa(binary)
}

function mergePayloads(payloads: Array<Uint8Array | number[]>): Uint8Array {
  const parts = payloads.map((p) => (p instanceof Uint8Array ? p : new Uint8Array(p)))
  const totalLen = parts.reduce((n, c) => n + c.length, 0)
  const merged = new Uint8Array(totalLen)
  let offset = 0
  for (const c of parts) {
    merged.set(c, offset)
    offset += c.length
  }
  return merged
}

async function loadZmodem(): Promise<AnyZ> {
  const mod = await import('zmodem.js/src/zmodem_browser.js')
  return (mod as AnyZ).default ?? mod
}

/**
 * 在终端会话上挂载 Zmodem Sentry。
 * 输出流应优先喂给 consume；非 Zmodem 数据经 to_terminal 写回 xterm。
 */
export async function attachZmodem(opts: {
  term: Terminal
  sessionId: string
  onProgress?: (p: ZmodemProgress) => void
  onStatus?: (msg: string) => void
  /** active 变化时回调，用于挂起/恢复键盘 */
  onActiveChange?: (active: boolean) => void
}): Promise<ZmodemController> {
  const Zmodem = await loadZmodem()
  let active = false
  let disposed = false
  const timeoutMs = 120_000
  let timeoutTimer: ReturnType<typeof setTimeout> | null = null
  let currentSession: AnyZ = null

  const clearTimer = () => {
    if (timeoutTimer) {
      clearTimeout(timeoutTimer)
      timeoutTimer = null
    }
  }

  const setActive = (v: boolean) => {
    if (active === v) return
    active = v
    opts.onActiveChange?.(v)
  }

  const refocusTerminal = () => {
    try {
      opts.term.focus()
    } catch {
      /* ignore */
    }
  }

  /** 结束 Zmodem：清状态、恢复输入、尽量把焦点还给终端 */
  const finish = (session: AnyZ | null, status?: string) => {
    clearTimer()
    setActive(false)
    currentSession = null
    if (status) opts.onStatus?.(status)
    // 下一帧再聚焦，避开系统对话框关闭后的焦点竞争
    requestAnimationFrame(() => refocusTerminal())
  }

  const armTimeout = (session: AnyZ) => {
    clearTimer()
    timeoutTimer = setTimeout(() => {
      try {
        session.abort?.()
      } catch {
        /* ignore */
      }
      finish(session, 'Zmodem 超时，已恢复终端；请改用 SFTP 传输')
    }, timeoutMs)
  }

  const writeSSH = (octets: number[] | Uint8Array) => {
    if (!opts.sessionId || disposed) return
    void sshService.write(opts.sessionId, bytesToBase64(octets)).catch(() => {})
  }

  const bindSessionEnd = (session: AnyZ, okMsg: string) => {
    currentSession = session
    session.on('session_end', () => {
      finish(session, okMsg)
    })
  }

  const handleReceive = (session: AnyZ) => {
    setActive(true)
    armTimeout(session)
    bindSessionEnd(session, 'Zmodem 接收完成')

    session.on('offer', (offer: AnyZ) => {
      void (async () => {
        try {
          const details = offer.get_details?.() ?? {}
          const name = String(details.name || 'download.bin')
          const total = Number(details.size || 0)
          const savePath = await sshService.selectSavePath(name)
          // 对话框关闭后立刻抢回焦点（即使后续还在收尾握手）
          refocusTerminal()

          if (!savePath) {
            // 用户取消：中止整次会话，避免 active 悬挂导致无法输入
            try {
              offer.skip?.()
            } catch {
              /* ignore */
            }
            try {
              session.abort?.()
            } catch {
              /* ignore */
            }
            finish(session, '已取消 Zmodem 接收')
            return
          }

          offer.on('input', (payload: Uint8Array) => {
            const len = payload?.length ?? 0
            opts.onProgress?.({
              name,
              direction: 'download',
              transferred: offer.get_offset?.() ?? len,
              total,
              done: false,
            })
            armTimeout(session)
          })

          const payloads = await offer.accept()
          const merged = mergePayloads(Array.isArray(payloads) ? payloads : [])
          await (window as AnyZ).go.main.App.WriteLocalFileBase64(savePath, bytesToBase64(merged))
          opts.onProgress?.({
            name,
            direction: 'download',
            transferred: merged.length,
            total: total || merged.length,
            done: true,
          })
          // 文件已落盘：若对端迟迟不发 ZFIN/OO，短暂等待后强制恢复输入
          armTimeout(session)
          setTimeout(() => {
            if (active && currentSession === session) {
              try {
                session.close?.()
              } catch {
                /* ignore */
              }
              // close 不成则 abort，保证解锁
              if (active && currentSession === session) {
                try {
                  session.abort?.()
                } catch {
                  /* ignore */
                }
                finish(session, 'Zmodem 接收完成')
              }
            }
          }, 1500)
        } catch (e) {
          opts.onProgress?.({
            name: '',
            direction: 'download',
            transferred: 0,
            total: 0,
            done: true,
            error: String(e),
          })
          try {
            session.abort?.()
          } catch {
            /* ignore */
          }
          finish(session, 'Zmodem 接收失败，请改用 SFTP')
        }
      })()
    })

    void session
      .start()
      .then(() => {
        // start() 在 ZFIN 时 resolve；若 session_end 已处理则 no-op
        if (active && currentSession === session) {
          finish(session, 'Zmodem 接收完成')
        }
      })
      .catch(() => {
        if (active && currentSession === session) {
          try {
            session.abort?.()
          } catch {
            /* ignore */
          }
          finish(session, 'Zmodem 接收失败，请改用 SFTP')
        }
      })
  }

  const handleSend = async (session: AnyZ) => {
    setActive(true)
    armTimeout(session)
    bindSessionEnd(session, 'Zmodem 发送完成')

    try {
      const path = await sshService.selectLocalFile()
      refocusTerminal()
      if (!path) {
        try {
          session.close?.()
        } catch {
          /* ignore */
        }
        try {
          session.abort?.()
        } catch {
          /* ignore */
        }
        finish(session, '已取消 Zmodem 发送')
        return
      }
      const b64 = await (window as AnyZ).go.main.App.ReadLocalFileBase64(path)
      const bytes = base64ToBytes(b64)
      const name = path.split(/[/\\]/).pop() || 'upload.bin'
      const ab = bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength) as ArrayBuffer
      const file = new File([ab], name)
      await Zmodem.Browser.send_files(session, [file], {
        on_progress: (_f: File, transfer: AnyZ) => {
          opts.onProgress?.({
            name,
            direction: 'upload',
            transferred: transfer?.get_offset?.() ?? 0,
            total: file.size,
            done: false,
          })
          armTimeout(session)
        },
        on_file_complete: () => {
          opts.onProgress?.({
            name,
            direction: 'upload',
            transferred: file.size,
            total: file.size,
            done: true,
          })
        },
      })
      try {
        session.close?.()
      } catch {
        /* ignore */
      }
      // close 后若未触发 session_end，兜底解锁
      setTimeout(() => {
        if (active && currentSession === session) {
          finish(session, 'Zmodem 发送完成')
        }
      }, 800)
    } catch (e) {
      opts.onProgress?.({
        name: '',
        direction: 'upload',
        transferred: 0,
        total: 0,
        done: true,
        error: String(e),
      })
      try {
        session.abort?.()
      } catch {
        /* ignore */
      }
      finish(session, 'Zmodem 发送失败，请改用 SFTP')
    }
  }

  const sentry = new Zmodem.Sentry({
    // 切勿因 active 丢弃输出：收尾字节与后续 shell 输出都要写回终端
    to_terminal: (octets: number[]) => {
      if (disposed || !octets?.length) return
      opts.term.write(Uint8Array.from(octets))
    },
    sender: (octets: number[]) => writeSSH(octets),
    on_retract: () => {
      finish(null)
    },
    on_detect: (detection: AnyZ) => {
      if (!detection?.is_valid?.()) return
      const session = detection.confirm()
      const role = String(detection.get_session_role?.() || '')
      opts.onStatus?.(`检测到 Zmodem（${role || 'transfer'}），请在对话框中选择文件…`)
      if (role === 'receive') {
        handleReceive(session)
      } else {
        void handleSend(session)
      }
    },
  })

  return {
    consume: (bytes: Uint8Array) => {
      if (disposed) {
        opts.term.write(bytes)
        return
      }
      try {
        sentry.consume(bytes)
      } catch {
        // 协议异常时强制恢复，避免永久锁死输入
        if (active) {
          try {
            currentSession?.abort?.()
          } catch {
            /* ignore */
          }
          finish(currentSession, 'Zmodem 异常，已恢复终端')
        }
        opts.term.write(bytes)
      }
    },
    dispose: () => {
      disposed = true
      try {
        currentSession?.abort?.()
      } catch {
        /* ignore */
      }
      finish(null)
    },
    active: () => active,
  }
}
