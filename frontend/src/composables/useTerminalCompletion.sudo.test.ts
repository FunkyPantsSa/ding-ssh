import {describe, expect, it, vi} from 'vitest'
import type {Terminal} from '@xterm/xterm'
import type {Colorizer} from '../utils/output-color'
import type {SessionTab} from '../types'
import {useTerminalCompletion, type TerminalCompletionOptions} from './useTerminalCompletion'

vi.mock('../services/history', () => ({historyService: {add: vi.fn(), query: vi.fn()}}))

describe('sudo command tracking before remote echo catches up', () => {
  it('returns the full submitted command even when the screen is several characters behind', () => {
    const text = 'operator@test:~$ su'
    const line = {length: text.length, getCell: (i: number) => ({getChars: () => text[i], getWidth: () => 1})}
    const term = {buffer: {active: {baseY: 0, cursorY: 0, getLine: () => line}}} as unknown as Terminal
    const completion = useTerminalCompletion({
      term: () => term, container: () => undefined, tab: {node: {id: 'one'}} as SessionTab,
      settings: {completionEnabled: false} as TerminalCompletionOptions['settings'],
      isComposing: () => false, write: vi.fn(), colorize: {noteCommand: vi.fn()} as unknown as Colorizer,
    })
    expect(completion.trackLineInput('sudo -i')).toBeUndefined()
    expect(completion.trackLineInput('\r')).toBe('sudo -i')
    completion.dispose()
  })
})
