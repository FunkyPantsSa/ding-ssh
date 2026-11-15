import {flushPromises} from '@vue/test-utils'
import {afterEach, expect, it} from 'vitest'
import {confirmAppClose, confirmTabs} from './closeConfirmation'
import {language} from '../i18n'
afterEach(() => { language.value = 'zh-CN' })
function button(label: string) {
 return Array.from(document.querySelectorAll<HTMLButtonElement>('[role="dialog"] button')).find(el => el.textContent === label)!
}
it('uses the themed application dialog and cancels without closing tabs', async () => {
 const result = confirmTabs(2)
 await flushPromises()
 expect(document.querySelector('[role="dialog"]')?.classList.contains('ui-dialog')).toBe(true)
 expect(document.body.textContent).toContain('关闭 2 个标签页')
 expect(document.activeElement).toBe(button('取消'))
 button('取消').click()
 expect(await result).toBe(false)
 await flushPromises()
 expect(document.querySelector('[role="dialog"]')).toBeNull()
})
it('confirms in English and prevents duplicate dialogs', async () => {
 language.value = 'en'
 const result = confirmTabs(3)
 await flushPromises()
 expect(await confirmTabs(1)).toBe(false)
 expect(document.querySelectorAll('[role="dialog"]')).toHaveLength(1)
 expect(document.body.textContent).toContain('Closing 3 tabs')
 button('Confirm close').click()
 expect(await result).toBe(true)
 await flushPromises()
})
it('treats Escape as cancellation', async () => {
 const result = confirmTabs(1)
 await flushPromises()
 document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}))
 expect(await result).toBe(false)
 await flushPromises()
})

it('uses the same themed dialog for application exit and cancels on Escape', async () => {
 const result = confirmAppClose()
 await flushPromises()
 const dialog = document.querySelector('[role="dialog"]')
 expect(dialog?.classList.contains('ui-dialog')).toBe(true)
 expect(dialog?.textContent).toContain('确认退出应用')
 expect(button('确认关闭').classList.contains('ui-button-secondary')).toBe(true)
 expect(button('确认关闭').classList.contains('ui-button-danger')).toBe(false)
 document.activeElement?.dispatchEvent(new KeyboardEvent('keydown', {key:'Escape', bubbles:true}))
 expect(await result).toBe(false)
 await flushPromises()
})
