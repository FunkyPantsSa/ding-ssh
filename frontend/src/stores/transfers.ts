import {defineStore} from 'pinia'
import {TRANSFER_HISTORY_LIMIT, loadTransferHistory, saveTransferHistory} from '../services/transfers'
import type {TransferRecord} from '../types'

/**
 * SFTP 传输记录 store（历史上传 / 下载页签的数据源）。
 *
 * 记录来自「传输结束后的落库调用」（SftpPanel 在每次传输结束时 add），持久化在 localStorage，
 * 因此重启应用后仍然能看到历史。
 */
export const useTransfersStore = defineStore('transfers', {
  state: () => ({
    records: loadTransferHistory(),
    loaded: true,
  }),
  getters: {
    uploads: (s) => s.records.filter((r) => r.direction === 'upload'),
    downloads: (s) => s.records.filter((r) => r.direction === 'download'),
    failed: (s) => s.records.filter((r) => r.status !== 'done'),
  },
  actions: {
    load() {
      this.records = loadTransferHistory()
    },
    /** 追加一条传输记录（最新的在最前），并裁剪到上限。 */
    add(record: TransferRecord) {
      this.records = [record, ...this.records].slice(0, TRANSFER_HISTORY_LIMIT)
      saveTransferHistory(this.records)
    },
    remove(id: string) {
      this.records = this.records.filter((r) => r.id !== id)
      saveTransferHistory(this.records)
    },
    clear() {
      this.records = []
      saveTransferHistory([])
    },
  },
})
