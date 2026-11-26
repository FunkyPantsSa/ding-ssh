import {afterEach, describe, expect, it, vi} from 'vitest'
import {focusableControls, installKeyboardNavigation, navigateKeyboardFocus} from './keyboard-navigation'

afterEach(() => { document.body.innerHTML = ''; vi.restoreAllMocks() })

function key(element: HTMLElement, value: string, options: KeyboardEventInit = {}) {
  const event = new KeyboardEvent('keydown', {key: value, bubbles: true, cancelable: true, ...options})
  element.dispatchEvent(event)
  return event
}

describe('application keyboard navigation', () => {
  it('skips unavailable controls and cycles actions in all four directions', () => {
    document.body.innerHTML = '<div data-keyboard-nav="linear"><button id="a">Retry</button><button hidden>Hidden</button><button disabled>Disabled</button><div style="display:none"><button>Collapsed</button></div><button id="b">Edit</button><button aria-disabled="true">Unavailable</button><button id="c">Close</button></div>'
    const stop = installKeyboardNavigation()
    try {
      const a = document.getElementById('a')!, b = document.getElementById('b')!, c = document.getElementById('c')!
      a.focus()
      expect(key(a, 'ArrowRight').defaultPrevented).toBe(true)
      expect(document.activeElement).toBe(b)
      key(b, 'ArrowDown'); expect(document.activeElement).toBe(c)
      key(c, 'ArrowLeft'); expect(document.activeElement).toBe(b)
      key(b, 'ArrowUp'); expect(document.activeElement).toBe(a)
      key(a, 'End'); expect(document.activeElement).toBe(c)
      key(c, 'ArrowRight'); expect(document.activeElement).toBe(a)
    } finally { stop() }
  })

  it('uses visible geometry for direction choices rather than DOM order', () => {
    document.body.innerHTML = '<main><button id="a">Top left</button><button id="b">Bottom left</button><button id="c">Top right</button></main>'
    const [a,b,c] = ['a','b','c'].map(id => document.getElementById(id)!)
    for (const [element,x,y] of [[a,0,0],[b,0,70],[c,120,0]] as const) {
      vi.spyOn(element,'getBoundingClientRect').mockReturnValue({left:x,right:x+80,top:y,bottom:y+30,width:80,height:30,x,y,toJSON(){}})
    }
    const event = new KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true,cancelable:true})
    a.addEventListener('keydown', e => navigateKeyboardFocus(e))
    a.focus(); a.dispatchEvent(event)
    expect(document.activeElement).toBe(c)
  })

  it('leaves a vertical navigation region toward the adjacent workspace', () => {
    document.body.innerHTML = '<div class="app-shell"><nav data-keyboard-nav="vertical"><button id="general">General</button><button>Appearance</button></nav><main><button id="save">Save</button></main></div>'
    const general=document.getElementById('general')!,save=document.getElementById('save')!
    vi.spyOn(general,'getBoundingClientRect').mockReturnValue({left:0,right:80,top:0,bottom:30,width:80,height:30,x:0,y:0,toJSON(){}})
    vi.spyOn(save,'getBoundingClientRect').mockReturnValue({left:200,right:280,top:0,bottom:30,width:80,height:30,x:200,y:0,toJSON(){}})
    const stop=installKeyboardNavigation()
    try { general.focus();key(general,'ArrowRight');expect(document.activeElement).toBe(save) }
    finally { stop() }
  })

  it('leaves input editing, native sliders, terminal keys and handled widgets intact', () => {
    document.body.innerHTML = '<main><input id="text"><input id="range" type="range"><textarea></textarea><select><option>A</option></select><div class="xterm"><textarea></textarea></div><div data-keyboard-native tabindex="0"></div><button id="handled">Menu</button><button>Next</button></main>'
    const stop = installKeyboardNavigation()
    try {
      for (const element of Array.from(document.querySelectorAll<HTMLElement>('input,textarea,select,[data-keyboard-native]'))) {
        element.focus()
        const event = key(element,'ArrowRight')
        expect(event.defaultPrevented).toBe(false)
        expect(document.activeElement).toBe(element)
      }
      const handled = document.getElementById('handled')!
      handled.addEventListener('keydown', event => event.preventDefault())
      handled.focus(); key(handled,'ArrowDown')
      expect(document.activeElement).toBe(handled)
      for (const options of [{isComposing:true},{ctrlKey:true},{metaKey:true},{altKey:true},{shiftKey:true}]) {
        const event = key(document.getElementById('text')!,'ArrowRight',options)
        expect(event.defaultPrevented).toBe(false)
      }
    } finally { stop() }
  })

  it('keeps vertical list movement on row actions outside nested toolbars', () => {
    document.body.innerHTML = '<main data-keyboard-nav="vertical"><button id="row1">Server 1</button><div data-keyboard-nav="horizontal"><button>Edit</button><button>Delete</button></div><button id="row2">Server 2</button></main>'
    const stop = installKeyboardNavigation()
    try {
      const first = document.getElementById('row1')!, second = document.getElementById('row2')!
      first.focus(); key(first,'ArrowDown')
      expect(document.activeElement).toBe(second)
    } finally { stop() }
  })

  it('activates custom buttons once and leaves native button activation to the browser', () => {
    document.body.innerHTML = '<main><div role="button" tabindex="0" id="custom">Custom</div><button id="native">Native</button><div role="button" aria-disabled="true" tabindex="0" id="disabled">No</div></main>'
    const stop = installKeyboardNavigation()
    try {
      const custom = document.getElementById('custom')!, native = document.getElementById('native')!, disabled = document.getElementById('disabled')!
      const customClick = vi.fn(), nativeClick = vi.fn(), disabledClick = vi.fn()
      custom.addEventListener('click',customClick); native.addEventListener('click',nativeClick);disabled.addEventListener('click',disabledClick)
      key(custom,'Enter');expect(customClick).toHaveBeenCalledTimes(1)
      expect(key(native,'Enter').defaultPrevented).toBe(false);expect(nativeClick).not.toHaveBeenCalled()
      key(disabled,'Enter');expect(disabledClick).not.toHaveBeenCalled()
    } finally { stop() }
  })

  it('excludes hidden ancestors, inert controls and closed details from focus loops', () => {
    document.body.innerHTML = '<div id="root"><details><summary>Show</summary><button>Inside</button></details><div inert><button>Blocked</button></div><div aria-hidden="true"><button>Hidden</button></div><button tabindex="-1">Programmatic</button><button>Visible</button></div>'
    expect(focusableControls(document.getElementById('root')!).map(el => el.textContent)).toEqual(['Show','Visible'])
  })
})
