import {computed, ref} from 'vue'
import {errorText} from '../i18n'
import {GetUpdateChannel, CheckUpdate, DownloadUpdate, InstallUpdateNow} from '../../bindings/ding-ssh/internal/apphost/app'
import {EventsOn} from './desktop'

function readPreference(key: string): string | null {
  try { return localStorage.getItem(key) } catch { return null }
}
function savePreference(key: string, value: string) {
  try { localStorage.setItem(key, value) } catch { /* Keep this session usable if storage is blocked or full. */ }
}

export const updateChannel = ref(readPreference('update-channel') === 'dev' ? 'dev' : 'main')
export const checkUpdatesOnStartup = ref(readPreference('update-auto') !== 'false')
export const updateInfo = ref<Awaited<ReturnType<typeof CheckUpdate>>>()
export const updateBusy = ref(false)
export const updateReady = ref(false)
const updateMessageSource = ref('')
export const updateMessage = computed(() => errorText(updateMessageSource.value))
export const updateProgress = ref('')
export const updateVisible = ref(false)
let dismissedCommit = ''

export function setUpdateChannel(channel: string) {
  if (updateBusy.value || updateReady.value) return
  updateChannel.value = channel
  savePreference('update-channel', channel)
  updateInfo.value = undefined
  updateMessageSource.value = ''
  updateVisible.value = false
}
export function setStartupCheck(enabled: boolean) {
  checkUpdatesOnStartup.value = enabled
  savePreference('update-auto', String(enabled))
}
export function dismissUpdate() {
  dismissedCommit = updateInfo.value?.manifest.commit ?? ''
  updateVisible.value = false
}
export async function checkUpdates(manual = false) {
  if (updateBusy.value || updateReady.value) return
  updateBusy.value = true
  updateMessageSource.value = '正在检查更新…'
  try {
    const result = await CheckUpdate(updateChannel.value)
    updateInfo.value = result
    updateMessageSource.value = result.available ? '发现新版本' : '当前已是最新版本'
    updateVisible.value = result.available && (manual || result.manifest.commit !== dismissedCommit)
  } catch (e) {
    updateMessageSource.value = String(e)
  } finally {
    updateBusy.value = false
  }
}
export async function downloadUpdate(now: boolean) {
  if (updateBusy.value || !updateInfo.value?.available) return
  updateBusy.value = true
  updateProgress.value = ''
  updateMessageSource.value = '正在后台下载并校验…'
  try {
    await DownloadUpdate(now)
    updateReady.value = true
    updateMessageSource.value = '已下载并校验，正常退出后自动安装'
    if (now) await installUpdateNow()
  } catch (e) {
    updateMessageSource.value = String(e)
  } finally {
    updateBusy.value = false
  }
}
export async function installUpdateNow() {
  try { await InstallUpdateNow() } catch (e) { updateMessageSource.value = String(e) }
}
export async function startUpdateChecks() {
  if (!readPreference('update-channel')) {
    try { updateChannel.value = await GetUpdateChannel() } catch { /* preview */ }
  }
  const off = EventsOn('update:progress', (p: {received: number; total: number}) => {
    updateProgress.value = p.total > 0 ? `${Math.round(p.received / p.total * 100)}%` : `${(p.received / 1048576).toFixed(1)} MB`
  })
  if (checkUpdatesOnStartup.value) void checkUpdates()
  return off
}
