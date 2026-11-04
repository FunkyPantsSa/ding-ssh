import {reactive} from 'vue'

export type ToastTone = 'success' | 'error' | 'info'
export interface ToastItem {id: number; key: string; title: string; detail?: string; tone: ToastTone}

const items = reactive<ToastItem[]>([])
let nextId = 1
const timers = new Map<number, number>()

function dismiss(id: number) {
  const index = items.findIndex((item) => item.id === id)
  if (index >= 0) items.splice(index, 1)
  window.clearTimeout(timers.get(id))
  timers.delete(id)
}

function show(title: string, options: {detail?: string; tone?: ToastTone; key?: string; duration?: number} = {}) {
  const key = options.key || `${options.tone || 'info'}:${title}`
  const existing = items.find((item) => item.key === key)
  if (existing) dismiss(existing.id)
  const item: ToastItem = {id: nextId++, key, title, detail: options.detail, tone: options.tone || 'info'}
  items.push(item)
  const duration = options.duration ?? (item.tone === 'error' ? 6000 : 3000)
  timers.set(item.id, window.setTimeout(() => dismiss(item.id), duration))
  return item.id
}

export function useToast() {
  return {
    items,
    dismiss,
    show,
    success: (title: string, detail?: string, key?: string) => show(title, {detail, key, tone: 'success'}),
    error: (title: string, detail?: string, key?: string) => show(title, {detail, key, tone: 'error'}),
  }
}
