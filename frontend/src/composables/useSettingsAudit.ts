import { computed, onBeforeUnmount, ref, type Ref } from 'vue'
import { EventsOn } from '../../wailsjs/runtime/runtime'
import { GetAuditRecords, UndoAuditRecord } from '../../wailsjs/go/main/App'
import type { AuditRecordView, CapabilitiesView } from '../types'

/** State and event lifetime for the Settings audit section. */
export function useSettingsAudit(capInfo: Ref<CapabilitiesView | null>) {
  const records = ref<AuditRecordView[]>([])
  const loading = ref(false)
  const error = ref('')
  const message = ref('')
  const filter = ref<'all' | 'write' | 'failed'>('all')
  const cap = ref('')
  const limit = ref(50)
  const expanded = ref('')
  let off: (() => void) | null = null

  const capOptions = computed(() => {
    const caps = capInfo.value?.caps ?? {}
    const labels = capInfo.value?.labels ?? {}
    return Object.keys(caps).map((name) => ({ value: name, label: `${labels[name] ?? name}（${name}）` }))
  })
  const filtered = computed(() => records.value.filter((record) =>
    filter.value === 'failed' ? !record.ok : filter.value === 'write' ? !!record.cap : true,
  ))
  const refresh = async () => {
    loading.value = true
    error.value = ''
    try {
      const result = await GetAuditRecords(limit.value, cap.value, '')
      records.value = Array.isArray(result) ? result as AuditRecordView[] : []
    } catch (cause) {
      error.value = String(cause)
      records.value = []
    } finally { loading.value = false }
  }
  const subscribe = () => {
    if (off) return
    off = EventsOn('debug.audit', (payload: AuditRecordView) => {
      if (!payload?.id || (cap.value && payload.cap !== cap.value) || records.value.some((record) => record.id === payload.id)) return
      records.value = [payload, ...records.value].slice(0, Math.max(limit.value, 50))
    })
  }
  const unsubscribe = () => { off?.(); off = null }
  const undo = async (record: AuditRecordView) => {
    error.value = ''; message.value = ''
    try {
      await UndoAuditRecord(record.id, record.tool, record.args)
      message.value = `已撤销 ${record.tool} 的改动（设置已恢复为改动前的值）`
      await refresh()
    } catch (cause) { error.value = String(cause) }
  }
  onBeforeUnmount(unsubscribe)
  return { records, loading, error, message, filter, cap, limit, expanded, capOptions, filtered, refresh, subscribe, unsubscribe, undo }
}
