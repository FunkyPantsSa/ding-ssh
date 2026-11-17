import {afterEach, expect, it} from 'vitest'
import {isTerminalSearchShortcut} from './terminal-search'
afterEach(() => document.body.replaceChildren())
function key(target: HTMLElement, options: KeyboardEventInit = {}) {
 const event = new KeyboardEvent('keydown', {key:'f',metaKey:true,bubbles:true,...options})
 target.dispatchEvent(event)
 return event
}
it('opens from the focused xterm helper textarea on macOS and Windows', () => {
 const root=document.createElement('div');root.className='xterm'
 const input=document.createElement('textarea');root.append(input);document.body.append(root)
 expect(isTerminalSearchShortcut(key(input),true)).toBe(true)
 expect(isTerminalSearchShortcut(key(input,{metaKey:false,ctrlKey:true}),true)).toBe(true)
 expect(isTerminalSearchShortcut(key(input),false)).toBe(false)
})
it('does not intercept ordinary form fields, composition or shifted shortcuts', () => {
 const input=document.createElement('input');document.body.append(input)
 expect(isTerminalSearchShortcut(key(input),true)).toBe(false)
 expect(isTerminalSearchShortcut(key(document.body,{isComposing:true}),true)).toBe(false)
 expect(isTerminalSearchShortcut(key(document.body,{shiftKey:true}),true)).toBe(false)
 const search=document.createElement('div');search.append(input);document.body.append(search)
 expect(isTerminalSearchShortcut(key(input),true,search)).toBe(true)
})
it('leaves modal dialogs in control of keyboard input', () => {
 const dialog=document.createElement('div');dialog.setAttribute('role','dialog');document.body.append(dialog)
 expect(isTerminalSearchShortcut(key(document.body),true)).toBe(false)
})
