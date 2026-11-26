import {createApp, defineComponent, h} from 'vue'
import {useSettingsStore} from '../stores/settings'
import {Button, Dialog} from '../components/ui'
import {t} from '../i18n'

// Serialize confirmation dialogs. A single global boolean caused a second
// close request for a different tab to resolve as cancelled without ever
// showing its confirmation. Keep requests queued until the user decides.
let confirmationQueue: Promise<void> = Promise.resolve()
export function needsTabConfirmation(): boolean {
  const settings = useSettingsStore()
  return settings.loaded && settings.confirmCloseTab
}
export function confirmTabs(count: number, names: string[] = []): Promise<boolean> {
  if (count < 1) return Promise.resolve(false)
  const targets = names.filter(Boolean).join('、')
  const message = t('关闭 {0} 个标签页将终止其中的会话，是否继续？', [count])
  return requestCloseConfirmation(t('确认关闭标签页'), targets ? `${message}\n${targets}` : message)
}
export function confirmAppClose(): Promise<boolean> {
  return requestCloseConfirmation(t('确认退出应用'), t('关闭应用将断开所有会话，是否继续？'))
}
function requestCloseConfirmation(title: string, message: string): Promise<boolean> {
  const result = confirmationQueue.then(() => new Promise<boolean>(resolve => {
    const container = document.createElement('div')
    document.body.appendChild(container)
    let settled = false
    const finish = (confirmed: boolean) => {
      if (settled) return
      settled = true
      resolve(confirmed)
      queueMicrotask(() => { app.unmount(); container.remove() })
    }
    const app = createApp(defineComponent({setup() {
      return () => h(Dialog, {
        modelValue: true,
        title,
        width: 440,
        initialFocus: '[data-close-cancel]',
        footerNavigation: true,
        'onUpdate:modelValue': () => finish(false),
      }, {
        default: () => h('p', {class: 'text-sm text-mist leading-relaxed break-words'},
          message),
        footer: () => [
          h(Button, {variant: 'ghost', 'data-close-cancel': '', onClick: () => finish(false)}, () => t('取消')),
          h(Button, {variant: 'secondary', onClick: () => finish(true)}, () => t('确认关闭')),
        ],
      })
    }}))
    app.mount(container)
  }))
  confirmationQueue = result.then(() => undefined, () => undefined)
  return result
}
