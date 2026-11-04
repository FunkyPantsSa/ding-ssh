import { onMounted, reactive, ref } from 'vue'
import { securityService } from '../services/security'
import type { SecurityStatus } from '../types'

/** Owns master-password state; no SettingsPage lifecycle details leak into the section. */
export function useSettingsSecurity() {
  const security = ref<SecurityStatus | null>(null)
  const form = reactive({ password: '', confirm: '', oldPassword: '', newPassword: '', newConfirm: '' })
  const error = ref(''); const message = ref(''); const busy = ref(false)
  const refresh = async () => { try { security.value = await securityService.getStatus() } catch { security.value = null } }
  const run = async (validate: () => string, action: () => Promise<void>, success: string, clear: () => void) => {
    error.value = ''; message.value = ''; const invalid = validate(); if (invalid) { error.value = invalid; return }
    busy.value = true
    try { await action(); clear(); message.value = success; await refresh() } catch (cause) { error.value = String(cause) } finally { busy.value = false }
  }
  const enableMaster = () => run(() => !form.password || form.password !== form.confirm ? '请填写并确认主密码' : '', () => securityService.enableMasterPassword(form.password), '主密码已启用，下次启动需输入解锁', () => { form.password = ''; form.confirm = '' })
  const disableMaster = () => run(() => !form.oldPassword ? '请输入当前主密码' : '', () => securityService.disableMasterPassword(form.oldPassword), '已关闭主密码，改回系统钥匙串保管', () => { form.oldPassword = '' })
  const changeMaster = () => run(() => !form.oldPassword || !form.newPassword || form.newPassword !== form.newConfirm ? '请填写旧密码与一致的新密码' : '', () => securityService.changeMasterPassword(form.oldPassword, form.newPassword), '主密码已更新', () => { form.oldPassword = ''; form.newPassword = ''; form.newConfirm = '' })
  onMounted(() => { void refresh() })
  return { security, form, error, message, busy, refresh, enableMaster, disableMaster, changeMaster }
}
