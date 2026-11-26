import {afterEach, describe, expect, it} from 'vitest'
import {hasOpenModal, shouldFocusTerminal, type TerminalFocusContext} from './terminal-focus'

afterEach(() => document.body.replaceChildren())

function fixture() {
  document.body.innerHTML = '<button id="outside">Settings</button><div id="root"><div id="terminal" tabindex="0"><div class="xterm"><textarea></textarea></div></div><div id="status" data-terminal-focus-surface tabindex="0"><button>Retry</button></div><div id="search"><input></div></div>'
  const root = document.getElementById('root')!
  const terminal = document.getElementById('terminal')!
  const context: TerminalFocusContext = {root, terminal, active: true, workspace: true}
  return {root, context, focus: (selector: string) => document.querySelector<HTMLElement>(selector)!.focus()}
}

describe('terminal focus ownership', () => {
  it('moves connection focus only from the terminal, passive status surface, or an empty focus', () => {
    const {context, focus} = fixture()
    expect(shouldFocusTerminal(context)).toBe(true)
    for (const selector of ['#terminal', '.xterm textarea', '#status']) {
      focus(selector)
      expect(shouldFocusTerminal(context)).toBe(true)
    }
    for (const selector of ['#outside', '#status button', '#search input']) {
      focus(selector)
      expect(shouldFocusTerminal(context)).toBe(false)
    }
  })

  it('keeps explicit new-tab activation available while blocking settings, inactive panes, hidden views, and overlays', () => {
    const {root, context, focus} = fixture()
    focus('#outside')
    expect(shouldFocusTerminal({...context, intent: 'activate'})).toBe(true)
    expect(shouldFocusTerminal({...context, intent: 'activate', workspace: false})).toBe(false)
    expect(shouldFocusTerminal({...context, intent: 'activate', active: false})).toBe(false)
    expect(shouldFocusTerminal({...context, intent: 'activate', overlayOpen: true})).toBe(false)
    root.style.display = 'none'
    expect(shouldFocusTerminal({...context, intent: 'activate'})).toBe(false)
  })

  it('never takes focus through a visible modal, including a dialog above the terminal', () => {
    const {context, focus} = fixture()
    focus('.xterm textarea')
    const modal = document.createElement('div')
    modal.setAttribute('aria-modal', 'true')
    document.body.append(modal)
    expect(hasOpenModal()).toBe(true)
    expect(shouldFocusTerminal({...context, intent: 'activate'})).toBe(false)
    modal.hidden = true
    expect(hasOpenModal()).toBe(false)
    expect(shouldFocusTerminal(context)).toBe(true)
    modal.hidden = false
    const hiddenParent = document.createElement('div')
    hiddenParent.style.display = 'none'
    document.body.append(hiddenParent)
    hiddenParent.append(modal)
    expect(hasOpenModal()).toBe(false)
  })

  it('restores focus after a terminal popup closes without stealing another control during an async action', () => {
    const {context, focus} = fixture()
    const search = document.getElementById('search')!
    const restore = {...context, intent: 'restore' as const, restoreFrom: [search]}
    focus('#search input')
    expect(shouldFocusTerminal(restore)).toBe(true)
    focus('#outside')
    expect(shouldFocusTerminal(restore)).toBe(false)
    context.root!.remove()
    expect(shouldFocusTerminal(restore)).toBe(false)
  })
})
