import {isDesktop} from './services/desktop-environment'
import {ref, type App} from 'vue'
import en from './locales/en.json'

export type Language = 'zh-CN' | 'en'
const key = 'ding-ssh.language'
function savedLanguage(): Language {
  try { return localStorage.getItem(key) === 'en' ? 'en' : 'zh-CN' } catch { return 'zh-CN' }
}
export const language = ref<Language>(savedLanguage())
export function t(text: string, values: readonly unknown[] = []): string {
  const message = language.value === 'en' ? (en as Record<string, string>)[text] ?? text : text
  return message.replace(/\{(\d+)\}/g, (token, index) => Number(index) < values.length ? String(values[Number(index)]) : token)
}
/** Localize application errors created before the backend receives the locale. */
export function errorText(error: unknown): string {
  const message = String(error)
  if (language.value !== 'en') return message
  return message.split(/(: |：)/).map(part => t(part)).join('')
}
export async function syncBackendLanguage(value: Language = language.value) {
  if (!isDesktop()) return
  const {SetUILanguage} = await import('../bindings/ding-ssh/internal/apphost/app')
  await SetUILanguage(value)
}
export async function setLanguage(value: Language) {
  if (value === language.value) return
  await syncBackendLanguage(value)
  language.value = value
  document.documentElement.lang = value
  try { localStorage.setItem(key, value) } catch { /* Session preference still applies. */ }
}
export function installI18n(app: App) {
  document.documentElement.lang = language.value
  app.config.globalProperties.$t = t
}
declare module 'vue' {
  interface ComponentCustomProperties { $t: typeof t }
}
