import {createApp, defineComponent, h, ref} from 'vue'
import {Button, Dialog, Input} from '../components/ui'

export function requestPassphrase(path: string): Promise<string | null> {
 return new Promise(resolve => {
  const container = document.createElement('div')
  document.body.appendChild(container)
  let settled = false
  const finish = (value: string | null) => {
   if (settled) return
   settled = true
   resolve(value)
   queueMicrotask(() => { app.unmount(); container.remove() })
  }
  const app = createApp(defineComponent({setup() {
   const value = ref('')
   return () => h(Dialog, {modelValue: true, title: '解锁 SSH 私钥', 'onUpdate:modelValue': () => finish(null)}, {
    default: () => [h('p', {class: 'text-xs text-mist break-all'}, path), h(Input, {modelValue: value.value, type: 'password', autocomplete: 'off', placeholder: '私钥密码短语（仅用于本次连接）', 'onUpdate:modelValue': (v: string | number | undefined) => value.value = String(v), onKeydown: (e: KeyboardEvent) => { if (e.key === 'Enter') finish(value.value) }})],
    footer: () => [h(Button, {variant: 'ghost', onClick: () => finish(null)}, () => '取消连接'), h(Button, {variant: 'primary', onClick: () => finish(value.value)}, () => '解锁并连接')],
   })
  }}))
  app.mount(container)
 })
}
