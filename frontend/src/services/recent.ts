import {readonly, shallowRef} from 'vue'

export interface RecentServer {id: string; name: string; host: string; connectedAt: number}
interface ServerUsage {id: string; connectedAt: number; count: number}

const KEY = 'ding-ssh:server-usage'
const LEGACY_KEY = 'ding-ssh:recent-servers'

function loadUsage(): ServerUsage[] {
  try {
    const saved = localStorage.getItem(KEY)
    const entries: unknown = JSON.parse(saved ?? localStorage.getItem(LEGACY_KEY) ?? '[]')
    if (!Array.isArray(entries)) return []
    const usage = new Map<string, ServerUsage>()
    for (const entry of entries) {
      const id = typeof entry === 'string' ? entry : entry?.id
      if (typeof id !== 'string' || !id || usage.has(id)) continue
      usage.set(id, {
        id,
        connectedAt: Number.isFinite(entry?.connectedAt) ? entry.connectedAt : 0,
        count: saved === null ? 1 : Number.isSafeInteger(entry?.count) && entry.count >= 0 ? entry.count : 0,
      })
    }
    return [...usage.values()]
  } catch {
    return []
  }
}

const usage = shallowRef(loadUsage())
export const serverUsage = readonly(usage)

export function recordRecentServer(entry: RecentServer, countLogin = true) {
  if (!entry.id) return
  const previous = usage.value.find((item) => item.id === entry.id)
  const count = (previous?.count ?? 0) + Number(countLogin)
  usage.value = [{id: entry.id, connectedAt: entry.connectedAt, count}, ...usage.value.filter((item) => item.id !== entry.id)]
  try {
    localStorage.setItem(KEY, JSON.stringify(usage.value))
  } catch {
    // Keep the current session's statistics if persistence is unavailable.
  }
}
