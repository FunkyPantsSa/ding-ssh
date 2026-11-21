import {describe, expect, it, vi} from 'vitest'
import type {Terminal} from '@xterm/xterm'
import {placeTerminalPopover, terminalCursorAnchor} from './terminal-popover'

vi.mock('./dom', () => ({getDisplayRect: (el: HTMLElement) => el.getBoundingClientRect()}))

describe('terminal cursor popover positioning', () => {
  it('opens below the input cursor instead of centering in the window', () => {
    expect(placeTerminalPopover({left: 240, top: 60, height: 20}, {width: 900, height: 600}, {width: 320, height: 150})).toEqual({left: 240, top: 86})
  })
  it('flips above a bottom-row cursor and stays within a narrow split pane', () => {
    const result = placeTerminalPopover({left: 400, top: 360, height: 20}, {width: 460, height: 400}, {width: 320, height: 160})
    expect(result).toEqual({left: 132, top: 194})
    expect(placeTerminalPopover({left: 0, top: 0, height: 20}, {width: 200, height: 100}, {width: 184, height: 84})).toEqual({left: 8, top: 8})
  })
  it('accounts for zoom, screen padding and scrollback viewport offsets', () => {
    const container = document.createElement('div'), screen = document.createElement('div')
    screen.className = 'xterm-screen'; container.appendChild(screen)
    Object.defineProperty(container, 'clientWidth', {value: 800})
    container.getBoundingClientRect = () => ({left: 120, top: 60, width: 960, height: 480} as DOMRect)
    screen.getBoundingClientRect = () => ({left: 132, top: 72, width: 960, height: 480} as DOMRect)
    const buffer = {baseY: 10, cursorY: 2, viewportY: 10, cursorX: 30}
    const term = {cols: 80, rows: 20, buffer: {active: buffer}} as unknown as Terminal
    expect(terminalCursorAnchor(term, container)).toEqual({left: 310, top: 50, height: 20})
    buffer.viewportY = 0
    expect(terminalCursorAnchor(term, container)?.top).toBe(250)
    buffer.viewportY = 100
    expect(terminalCursorAnchor(term, container)).toBeNull()
  })
})
