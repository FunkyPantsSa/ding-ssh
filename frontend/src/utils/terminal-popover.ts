import type {Terminal} from '@xterm/xterm'
import {getDisplayRect} from './dom'

export interface CursorAnchor {left: number; top: number; height: number}

/** Coordinates local to the terminal pane, including UI zoom and scrollback. */
export function terminalCursorAnchor(term: Terminal, container: HTMLElement): CursorAnchor | null {
  const screen = container.querySelector<HTMLElement>('.xterm-screen')
  if (!screen || !container.clientWidth || !term.cols || !term.rows) return null
  const buffer = term.buffer.active
  const row = buffer.baseY + buffer.cursorY - buffer.viewportY
  if (row < 0 || row >= term.rows) return null
  const bounds = getDisplayRect(container)
  const rect = getDisplayRect(screen)
  const zoom = bounds.width / container.clientWidth || 1
  const cellHeight = rect.height / term.rows / zoom
  return {
    left: (rect.left - bounds.left + buffer.cursorX * rect.width / term.cols) / zoom,
    top: (rect.top - bounds.top) / zoom + row * cellHeight,
    height: cellHeight,
  }
}

/** Prefer directly below the cursor; flip above and clamp to this split pane. */
export function placeTerminalPopover(anchor: CursorAnchor, bounds: {width: number; height: number}, size: {width: number; height: number}) {
  const gap = 6
  let top = anchor.top + anchor.height + gap
  if (top + size.height > bounds.height - 8) top = anchor.top - size.height - gap
  return {
    left: Math.max(8, Math.min(anchor.left, bounds.width - size.width - 8)),
    top: Math.max(8, Math.min(top, bounds.height - size.height - 8)),
  }
}
