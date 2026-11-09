import {LogClient} from '../../wailsjs/go/main/App'
import {listDebugTerminals} from '../debug/registry'

let lastDelayMs = 0
let maxDelayMs = 0

export function frontendRuntimeStatus() {
  const terminals = listDebugTerminals().map((entry) => entry.runtimeStatus?.())
  const memory = (performance as Performance & {memory?: {usedJSHeapSize: number}}).memory
  return {
    jsHeapBytes: memory?.usedJSHeapSize,
    lastDelayMs,
    maxDelayMs,
    terminalCount: terminals.length,
    webglCount: terminals.filter((terminal) => terminal?.webgl).length,
    domCount: terminals.filter((terminal) => terminal && !terminal.webgl).length,
    peakOutputBytes: terminals.reduce((max, terminal) => Math.max(max, terminal?.peakOutputBytes ?? 0), 0),
    pendingOutputBytes: terminals.reduce((sum, terminal) => sum + (terminal?.pendingOutputBytes ?? 0), 0),
  }
}

export function monitorFrontendRuntime() {
  const interval = 1000
  let expected = performance.now() + interval
  let lastWarning = -Infinity
  function sample() {
    const now = performance.now()
    if (document.visibilityState === 'visible') {
      lastDelayMs = Math.max(0, Math.round(now - expected))
      maxDelayMs = Math.max(maxDelayMs, lastDelayMs)
      if (lastDelayMs >= 1000 && now - lastWarning >= 30000) {
        lastWarning = now
        const snapshot = frontendRuntimeStatus()
        void Promise.resolve().then(() => LogClient('warn', `[runtime] 前端定时器延迟 ${lastDelayMs}ms，WebGL=${snapshot.webglCount}/${snapshot.terminalCount}，待解析终端输出=${snapshot.pendingOutputBytes}B，单终端峰值=${snapshot.peakOutputBytes}B`)).catch(() => {})
      }
    }
    expected = now + interval
    window.setTimeout(sample, interval)
  }
  document.addEventListener('visibilitychange', () => {
    expected = performance.now() + interval
  })
  window.setTimeout(sample, interval)
}
