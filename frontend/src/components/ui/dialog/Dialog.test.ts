import {mount} from '@vue/test-utils'
import {afterEach, describe, expect, it} from 'vitest'
import {nextTick} from 'vue'
import Dialog from './Dialog.vue'

describe('ui.Dialog accessibility', () => {
  afterEach(() => {
    document.body.innerHTML = ''
  })

  it('moves focus into the dialog, traps Tab and restores focus on close', async () => {
    const trigger = document.createElement('button')
    trigger.id = 'dialog-trigger'
    document.body.appendChild(trigger)
    trigger.focus()

    const wrapper = mount(Dialog, {
      attachTo: document.body,
      props: {modelValue: true, title: '测试对话框'},
      slots: {default: '<button id="dialog-first">第一项</button><button id="dialog-last">最后一项</button>'},
    })
    await nextTick()
    await nextTick()

    // 初始焦点落在弹窗内第一个可交互元素（标题栏关闭按钮）。
    expect(document.activeElement?.getAttribute('aria-label')).toBe('关闭对话框')

    const last = document.getElementById('dialog-last') as HTMLButtonElement
    last.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Tab', bubbles: true, cancelable: true}))
    expect(document.activeElement?.getAttribute('aria-label')).toBe('关闭对话框')

    const closeButton = document.querySelector<HTMLButtonElement>('[aria-label="关闭对话框"]')!
    closeButton.focus()
    document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Tab', shiftKey: true, bubbles: true, cancelable: true}))
    expect(document.activeElement).toBe(last)

    await wrapper.setProps({modelValue: false})
    await nextTick()
    expect(document.activeElement).toBe(trigger)
    wrapper.unmount()
  })

  it('keeps an explicit-decision dialog open on backdrop and Escape', async () => {
    const wrapper = mount(Dialog, {
      attachTo: document.body,
      props: {modelValue: true, title: '恢复会话', closeOnOverlay: false, closeOnEscape: false, showClose: false},
      slots: {footer: '<button id="restore-cancel">取消</button><button>自动连接</button>'},
    })
    await nextTick()
    await nextTick()
    document.querySelector('.ui-dialog-root')!.dispatchEvent(new MouseEvent('mousedown', {bubbles: true}))
    document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true}))
    expect(wrapper.emitted('close')).toBeUndefined()
    expect(wrapper.emitted('update:modelValue')).toBeUndefined()
    expect(document.querySelector('.ui-dialog-close')).toBeNull()
    expect(document.activeElement?.id).toBe('restore-cancel')
    wrapper.unmount()
  })

  it('prefers the first form control as the initial focus target', async () => {
    const wrapper = mount(Dialog, {
      attachTo: document.body,
      props: {modelValue: true, title: '表单'},
      slots: {default: '<input id="dialog-input" /><button id="dialog-ok">确定</button>'},
    })
    await nextTick()
    await nextTick()
    expect(document.activeElement?.id).toBe('dialog-input')
    wrapper.unmount()
  })

  it('skips collapsed form fields and keeps Tab able to leave a footer', async () => {
    const wrapper = mount(Dialog, {
      attachTo: document.body,
      props: {modelValue: true, title: '表单', footerNavigation: true},
      slots: {default:'<div style="display:none"><input id="hidden-input"></div><input id="visible-input">', footer:'<button id="cancel">取消</button><button id="save">保存</button>'},
    })
    await nextTick();await nextTick()
    expect(document.activeElement?.id).toBe('visible-input')
    const cancel = document.getElementById('cancel')!
    cancel.focus()
    cancel.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowRight',bubbles:true,cancelable:true}))
    expect(document.activeElement?.id).toBe('save')
    cancel.focus()
    const tab = new KeyboardEvent('keydown',{key:'Tab',shiftKey:true,bubbles:true,cancelable:true})
    cancel.dispatchEvent(tab)
    expect(tab.defaultPrevented).toBe(false)
    wrapper.unmount()
  })

  it('gives Escape and focus ownership to the top nested dialog', async () => {
    const trigger = document.createElement('button');document.body.append(trigger);trigger.focus()
    const outer = mount(Dialog,{attachTo:document.body,props:{modelValue:true,title:'外层'},slots:{default:'<button id="open-inner">Open</button>'}})
    await nextTick();await nextTick()
    document.getElementById('open-inner')!.focus()
    const inner = mount(Dialog,{attachTo:document.body,props:{modelValue:true,title:'内层'},slots:{default:'<button id="inner-ok">OK</button>'}})
    await nextTick();await nextTick()
    document.activeElement!.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}))
    expect(inner.emitted('close')).toHaveLength(1)
    expect(outer.emitted('close')).toBeUndefined()
    await inner.setProps({modelValue:false});await nextTick()
    expect(document.activeElement?.id).toBe('open-inner')
    inner.unmount();outer.unmount()
  })

  it('leaves Escape to a native select before dismissing the dialog', async () => {
    const wrapper=mount(Dialog,{attachTo:document.body,props:{modelValue:true,title:'选择'},slots:{default:'<select><option>One</option><option>Two</option></select>'}})
    await nextTick();await nextTick()
    const event=new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true})
    document.activeElement!.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(false)
    expect(wrapper.emitted('close')).toBeUndefined()
    wrapper.unmount()
  })
})
