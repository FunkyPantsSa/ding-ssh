// SFTP 传输记录（历史上传 / 下载）的本地存储。
//
// 记录只在本机保存，用于「传完之后回看」：方向、文件名、大小、耗时、平均速度、结果与本地路径。
// 落 localStorage 而不是后端配置文件：这是界面侧的浏览历史（可随时清空），
// 不需要参与配置迁移 / 加密，也不需要额外 IPC。
import type {TransferRecord} from '../types'

const STORAGE_KEY = 'ding-ssh:sftp-transfer-history'
/** 最多保留的记录条数：只留最近的一批，避免 localStorage 无限增长。 */
export const TRANSFER_HISTORY_LIMIT = 200

function revive(list: unknown): TransferRecord[] {
  if (!Array.isArray(list)) return []
  const out: TransferRecord[] = []
  for (const item of list) {
    if (!item || typeof item !== 'object') continue
    const r = item as Partial<TransferRecord>
    if (typeof r.id !== 'string' || typeof r.name !== 'string') continue
    out.push({
      id: r.id,
      direction: r.direction === 'download' ? 'download' : 'upload',
      name: r.name,
      size: Number(r.size) || 0,
      transferred: Number(r.transferred) || 0,
      startedAt: Number(r.startedAt) || 0,
      finishedAt: Number(r.finishedAt) || 0,
      durationMs: Number(r.durationMs) || 0,
      avgBytesPerSec: Number(r.avgBytesPerSec) || 0,
      serverName: typeof r.serverName === 'string' ? r.serverName : '',
      host: typeof r.host === 'string' ? r.host : '',
      remotePath: typeof r.remotePath === 'string' ? r.remotePath : '',
      localPath: typeof r.localPath === 'string' ? r.localPath : '',
      status: r.status === 'error' ? 'error' : r.status === 'cancelled' ? 'cancelled' : 'done',
      error: typeof r.error === 'string' ? r.error : '',
    })
  }
  return out
}

/** 读取全部传输记录（最新的在最前）。 */
export function loadTransferHistory(): TransferRecord[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    return revive(JSON.parse(raw))
  } catch {
    // 存储不可用 / 数据损坏：当作空历史，不影响主流程
    return []
  }
}

/** 覆盖写入传输记录（调用方负责截断与排序）。 */
export function saveTransferHistory(list: TransferRecord[]): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(list.slice(0, TRANSFER_HISTORY_LIMIT)))
  } catch {
    // 配额不足 / 隐私模式：忽略，历史记录不是关键路径
  }
}
