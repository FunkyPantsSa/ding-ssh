import {errorText} from '../i18n'
import {t} from '../i18n'
import {copyText as ClipboardSetText} from '../utils/clipboard'
import {createApp, defineComponent, h, ref} from 'vue'
import {Button, Dialog} from '../components/ui'

export interface HostTrustChallenge {id: string; host: string; fingerprint: string; keyType: string; changed: boolean; oldFingerprints: string[]; canReplace: boolean}
export function requestHostTrust(challenge: HostTrustChallenge): Promise<boolean> {
 return new Promise(resolve => {
  const container = document.createElement('div'); document.body.appendChild(container)
  let settled = false
  const finish = (value: boolean) => {if (settled) return; settled = true; resolve(value); queueMicrotask(() => {app.unmount(); container.remove()})}
  const app = createApp(defineComponent({setup() {
   const verified = ref(false)
   const error = ref('')
   return () => h(Dialog, {modelValue: true, title: challenge.changed ? t("服务器主机密钥发生变化") : t("首次连接 SSH 服务器"), 'onUpdate:modelValue': () => finish(false)}, {
    default: () => [
     h('p', {class: 'text-sm'}, t("主机：{0}", [challenge.host])),
     h('p', {class: 'text-sm'}, t("密钥类型：{0}", [challenge.keyType])),
     ...(challenge.oldFingerprints ?? []).map(fp => h('p', {class: 'text-xs break-all select-text'}, t("旧指纹：{0}", [fp]))),
     h('p', {class: 'text-xs break-all select-text'}, t("新指纹：{0}", [challenge.fingerprint])),
     h(Button, {size: 'sm', onClick: () => ClipboardSetText(challenge.fingerprint).catch(e => error.value = errorText(e))}, () => t("复制指纹")),
     challenge.changed && challenge.canReplace ? h('p', {class: 'text-xs text-mist'}, t("确认后仅更新 ding-ssh 的信任记录，不修改系统或 OpenSSH 源文件。")) : null,
     h('p', {class: 'text-sm text-amber-500'}, challenge.changed ? t("可能是服务器重装或密钥轮换，也可能存在中间人攻击。请通过可信渠道核对新指纹后再覆盖。") : t("该服务器的身份尚未得到验证，请通过可信渠道核对指纹。")),
     challenge.changed && !challenge.canReplace ? h('p', {class: 'text-sm text-rose-400'}, t("冲突来自只读或特殊信任记录，请先更新对应源文件。")) : null,
     challenge.changed ? h('label', {class: 'flex items-center gap-2 text-sm'}, [h('input', {type: 'checkbox', checked: verified.value, onChange: (e: Event) => verified.value = (e.target as HTMLInputElement).checked}), t("我已通过可信渠道核对新指纹")]) : null,
     error.value ? h('p', {role: 'alert'}, error.value) : null,
    ],
    footer: () => [h(Button, {variant: 'ghost', onClick: () => finish(false)}, () => t("取消连接")), h(Button, {variant: challenge.changed ? 'danger' : 'primary', disabled: challenge.changed && (!verified.value || !challenge.canReplace), onClick: () => finish(true)}, () => challenge.changed ? t("覆盖当前密钥并连接") : t("信任并连接"))],
   })
  }})); app.mount(container)
 })
}
