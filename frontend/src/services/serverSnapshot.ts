import {reactive} from 'vue'
import type {ServerNode, SysInfoSnapshot} from '../types'
const key = (node: ServerNode) => `${node.id}:${node.user}@${node.host}:${node.port}`
function load(): Record<string, SysInfoSnapshot> {
  try { return JSON.parse(localStorage.getItem('server-config-snapshots') || '{}') } catch { return {} }
}
export const serverSnapshots = reactive(load())
export function getServerSnapshot(node: ServerNode) { return serverSnapshots[key(node)] }
export function cacheServerSnapshot(node: ServerNode, snap: SysInfoSnapshot) {
  const id = key(node)
  for (const old of Object.keys(serverSnapshots)) {
    if (old.startsWith(`${node.id}:`) && old !== id) delete serverSnapshots[old]
  }
  serverSnapshots[id] = snap.error && serverSnapshots[id] ? {...serverSnapshots[id], error: snap.error} : snap
  try { localStorage.setItem('server-config-snapshots', JSON.stringify(serverSnapshots)) } catch { /* storage unavailable */ }
}

export function clearServerSnapshot(nodeId: string) {
 for (const id of Object.keys(serverSnapshots)) if (id.startsWith(`${nodeId}:`)) delete serverSnapshots[id]
 try { localStorage.setItem('server-config-snapshots', JSON.stringify(serverSnapshots)) } catch {}
}
