export interface RecentServer {id: string; name: string; host: string; connectedAt: number}

const KEY = 'ding-ssh:recent-servers'
const LIMIT = 8

export function loadRecentServers(): RecentServer[] {
  try {
    const raw = localStorage.getItem(KEY)
    const entries = JSON.parse(raw || '[]') as unknown
    if (!Array.isArray(entries)) return []
    return entries.filter((entry): entry is RecentServer => !!entry && typeof entry === 'object' && typeof (entry as RecentServer).id === 'string' && typeof (entry as RecentServer).connectedAt === 'number').slice(0, LIMIT)
  } catch { return [] }
}

export function recordRecentServer(entry: RecentServer) {
  const next = [entry, ...loadRecentServers().filter((item) => item.id !== entry.id)].slice(0, LIMIT)
  try { localStorage.setItem(KEY, JSON.stringify(next)) } catch { /* persistence is optional */ }
}
