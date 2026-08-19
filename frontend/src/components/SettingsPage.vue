<script lang="ts" setup>
import {onBeforeUnmount, onMounted, reactive, ref, watch} from 'vue'
import Icon from './Icon.vue'
import {
  DEFAULT_COMPLETION_NAV_HOTKEY,
  formatHotkeyLabel,
  hotkeyFromEvent,
} from '../completion/hotkey'
import {historyService} from '../services/history'
import {securityService} from '../services/security'
import {sshService} from '../services/ssh'
import {useCredentialsStore} from '../stores/credentials'
import {useServersStore} from '../stores/servers'
import {defaultTheme, useSettingsStore} from '../stores/settings'
import type {SecurityStatus, Theme} from '../types'
import ToggleSwitch from './ToggleSwitch.vue'

const settings = useSettingsStore()
const credentials = useCredentialsStore()
const servers = useServersStore()
const saving = ref(false)

// 一级菜单：通用 / 终端主题 / 凭证 / 安全 / 导入导出
const menuItems = [
  {key: 'general', label: '通用', icon: 'clock'},
  {key: 'theme', label: '终端主题', icon: 'palette'},
  {key: 'credentials', label: '保存的凭证', icon: 'key'},
  {key: 'security', label: '安全', icon: 'lock'},
  {key: 'migrate', label: '导入导出', icon: 'package'},
] as const
const section = ref<'general' | 'theme' | 'credentials' | 'security' | 'migrate'>('general')
const themeForm = reactive<Theme>(defaultTheme())
const credForm = reactive({name: '', user: '', password: '', authType: 'password', keySource: 'file' as 'file' | 'content', keyPath: '', keyContent: ''})
const credError = ref('')
const confirmCredId = ref('')

const security = ref<SecurityStatus | null>(null)
const secForm = reactive({password: '', confirm: '', oldPassword: '', newPassword: '', newConfirm: ''})
const secError = ref('')
const secMsg = ref('')
const secBusy = ref(false)

const packPass = ref('')
const packPassConfirm = ref('')
const packOverwrite = ref(false)
const packError = ref('')
const packMsg = ref('')
const packBusy = ref(false)

async function refreshSecurity() {
  try {
    security.value = await securityService.getStatus()
  } catch {
    security.value = null
  }
}

async function toggleLog(v: boolean) {
  saving.value = true
  try {
    await settings.setLogEnabled(v)
  } finally {
    saving.value = false
  }
}

async function toggleCopy(v: boolean) {
  saving.value = true
  try {
    await settings.setCopyOnSelect(v)
  } finally {
    saving.value = false
  }
}

async function toggleWebGL(v: boolean) {
  saving.value = true
  try {
    await settings.setWebGLEnabled(v)
  } finally {
    saving.value = false
  }
}

async function toggleCompletion(v: boolean) {
  saving.value = true
  try {
    await settings.setCompletionEnabled(v)
  } finally {
    saving.value = false
  }
}

async function toggleSftpSync(v: boolean) {
  saving.value = true
  try {
    await settings.setSftpToTerminalSync(v)
  } finally {
    saving.value = false
  }
}

async function toggleTerminalSftpSync(v: boolean) {
  saving.value = true
  try {
    await settings.setTerminalToSftpSync(v)
  } finally {
    saving.value = false
  }
}

async function toggleAutoReconnect(v: boolean) {
  saving.value = true
  try {
    await settings.setAutoReconnect(v)
  } finally {
    saving.value = false
  }
}

async function toggleKeepAlive(v: boolean) {
  saving.value = true
  try {
    await settings.setKeepAliveEnabled(v)
  } finally {
    saving.value = false
  }
}

async function setUIScale(v: number) {
  saving.value = true
  try {
    await settings.setUIScale(v)
  } finally {
    saving.value = false
  }
}

async function onPanelLimitChange(e: Event) {
  const raw = Number((e.target as HTMLInputElement).value)
  saving.value = true
  try {
    await settings.setCompletionPanelLimit(raw)
  } finally {
    saving.value = false
  }
}

const capturingHotkey = ref(false)
const hotkeyCaptureError = ref('')

function startCaptureHotkey() {
  capturingHotkey.value = true
  hotkeyCaptureError.value = ''
}

async function onHotkeyCapture(e: KeyboardEvent) {
  if (!capturingHotkey.value) return
  e.preventDefault()
  e.stopPropagation()
  if (e.key === 'Escape') {
    capturingHotkey.value = false
    hotkeyCaptureError.value = ''
    return
  }
  if (['Control', 'Alt', 'Shift', 'Meta'].includes(e.key)) return
  const hk = hotkeyFromEvent(e)
  if (!hk) {
    hotkeyCaptureError.value = '请使用修饰键组合（如 Alt+↓），或方向键 / F 键'
    return
  }
  capturingHotkey.value = false
  hotkeyCaptureError.value = ''
  saving.value = true
  try {
    await settings.setCompletionNavHotkey(hk)
  } finally {
    saving.value = false
  }
}

async function resetNavHotkey() {
  saving.value = true
  try {
    await settings.setCompletionNavHotkey(DEFAULT_COMPLETION_NAV_HOTKEY)
  } finally {
    saving.value = false
  }
}

const clearingHistory = ref(false)
const historyMsg = ref('')
const historyError = ref('')
const confirmClearHistory = ref(false)

async function clearAllHistory() {
  historyError.value = ''
  historyMsg.value = ''
  clearingHistory.value = true
  try {
    await historyService.clear('')
    confirmClearHistory.value = false
    historyMsg.value = '已清空全部命令历史'
  } catch (e) {
    historyError.value = String(e)
  } finally {
    clearingHistory.value = false
  }
}

async function applyTheme() {
  saving.value = true
  try {
    await settings.setTheme({...themeForm})
  } finally {
    saving.value = false
  }
}

async function resetTheme() {
  Object.assign(themeForm, defaultTheme())
  await settings.setTheme({...themeForm})
}

async function pickBgImage() {
  try {
    const path = await sshService.selectImageFile()
    if (path) themeForm.bgImage = path
  } catch (e) {
    // 用户取消或选择失败时静默处理
  }
}

async function pickCredKeyFile() {
  try {
    const path = await sshService.selectKeyFile()
    if (path) credForm.keyPath = path
  } catch (e) {
  }
}

async function addCredential() {
  if (!credForm.name.trim() || !credForm.user.trim()) {
    credError.value = '请填写凭证名称和用户名'
    return
  }
  if (credForm.authType === 'password' && !credForm.password) {
    credError.value = '请填写密码'
    return
  }
  if (credForm.authType === 'privateKey') {
    if (credForm.keySource === 'file' && !credForm.keyPath) {
      credError.value = '请选择私钥文件'
      return
    }
    if (credForm.keySource === 'content' && !credForm.keyContent) {
      credError.value = '请粘贴私钥内容'
      return
    }
    if (credForm.keySource === 'content') credForm.keyPath = ''
    else credForm.keyContent = ''
  }
  credError.value = ''
  await credentials.save({id: '', ...credForm})
  credForm.name = ''
  credForm.user = ''
  credForm.password = ''
  credForm.authType = 'password'
  credForm.keyPath = ''
  credForm.keyContent = ''
}

async function removeCredential(id: string) {
  confirmCredId.value = ''
  await credentials.remove(id)
}

async function enableMaster() {
  secError.value = ''
  secMsg.value = ''
  if (!secForm.password || secForm.password !== secForm.confirm) {
    secError.value = '请填写并确认主密码'
    return
  }
  secBusy.value = true
  try {
    await securityService.enableMasterPassword(secForm.password)
    secForm.password = ''
    secForm.confirm = ''
    secMsg.value = '主密码已启用，下次启动需输入解锁'
    await refreshSecurity()
  } catch (e) {
    secError.value = String(e)
  } finally {
    secBusy.value = false
  }
}

async function disableMaster() {
  secError.value = ''
  secMsg.value = ''
  if (!secForm.oldPassword) {
    secError.value = '请输入当前主密码'
    return
  }
  secBusy.value = true
  try {
    await securityService.disableMasterPassword(secForm.oldPassword)
    secForm.oldPassword = ''
    secMsg.value = '已关闭主密码，改回系统钥匙串保管'
    await refreshSecurity()
  } catch (e) {
    secError.value = String(e)
  } finally {
    secBusy.value = false
  }
}

async function changeMaster() {
  secError.value = ''
  secMsg.value = ''
  if (!secForm.oldPassword || !secForm.newPassword || secForm.newPassword !== secForm.newConfirm) {
    secError.value = '请填写旧密码与一致的新密码'
    return
  }
  secBusy.value = true
  try {
    await securityService.changeMasterPassword(secForm.oldPassword, secForm.newPassword)
    secForm.oldPassword = ''
    secForm.newPassword = ''
    secForm.newConfirm = ''
    secMsg.value = '主密码已更新'
    await refreshSecurity()
  } catch (e) {
    secError.value = String(e)
  } finally {
    secBusy.value = false
  }
}

async function exportPack() {
  packError.value = ''
  packMsg.value = ''
  if (!packPass.value || packPass.value !== packPassConfirm.value) {
    packError.value = '请填写并确认导出密码'
    return
  }
  packBusy.value = true
  try {
    const path = await securityService.exportConfig(packPass.value)
    if (path) {
      packMsg.value = `已导出到 ${path}`
      packPass.value = ''
      packPassConfirm.value = ''
    }
  } catch (e) {
    packError.value = String(e)
  } finally {
    packBusy.value = false
  }
}

async function importPack() {
  packError.value = ''
  packMsg.value = ''
  if (!packPass.value) {
    packError.value = '请填写导入密码'
    return
  }
  packBusy.value = true
  try {
    const r = await securityService.importConfig(packPass.value, packOverwrite.value)
    packMsg.value = `已导入服务器 ${r.servers}、凭证 ${r.credentials}、分组 ${r.groups}`
    packPass.value = ''
    await servers.load()
    await credentials.load()
  } catch (e) {
    packError.value = String(e)
  } finally {
    packBusy.value = false
  }
}

onMounted(async () => {
  window.addEventListener('keydown', onHotkeyCapture, true)
  await Promise.all([settings.load(), credentials.load(), refreshSecurity()])
  Object.assign(themeForm, settings.theme)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onHotkeyCapture, true)
})

watch(
  () => section.value,
  (s) => {
    if (s === 'theme') Object.assign(themeForm, settings.theme)
    if (s === 'security') void refreshSecurity()
  },
)
</script>

<template>
  <div class="h-full flex min-h-0">
    <nav class="set-nav">
      <button
        v-for="item in menuItems"
        :key="item.key"
        :class="section === item.key ? 'active' : ''"
        @click="section = item.key"
      >
        <Icon :name="item.icon" :size="14" />
        {{ item.label }}
      </button>
    </nav>

    <div class="flex-1 min-w-0 overflow-y-auto px-8 py-6">
      <!-- 通用设置 -->
      <div v-if="section === 'general'" class="max-w-2xl space-y-6 fade-rise">
        <div>
          <h3 class="text-[18px] font-semibold text-white tracking-tight">通用</h3>
          <p class="text-[13px] text-mist mt-1.5 leading-relaxed">控制日志、选中复制、补全热键与命令历史。</p>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">输出调试日志</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                开启后输出应用运行日志与 Wails 框架日志（运行终端），便于排查 SSH
                连接等问题；关闭后不输出日志。
              </p>
            </div>
            <ToggleSwitch :model-value="settings.logEnabled" :disabled="saving" @update:model-value="toggleLog" />
          </div>
          <div class="px-5 py-3 border-t border-slate-800/60 flex items-center gap-2 text-xs">
            <span class="w-2 h-2 rounded-full" :class="settings.logEnabled ? 'bg-emerald-400' : 'bg-slate-600'"></span>
            <span class="text-slate-400">当前状态：{{ settings.logEnabled ? '日志输出中' : '日志已关闭' }}</span>
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">选中内容自动复制</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">开启后在终端中选中文本，内容将自动复制到剪贴板。</p>
            </div>
            <ToggleSwitch :model-value="settings.copyOnSelect" :disabled="saving" @update:model-value="toggleCopy" />
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">WebGL 硬件加速</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                优先使用 GPU 渲染终端；不可用或上下文丢失时自动降级 Canvas。关闭后强制使用 Canvas。
              </p>
            </div>
            <ToggleSwitch :model-value="settings.webGLEnabled" :disabled="saving" @update:model-value="toggleWebGL" />
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">智能命令补全</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                输入时展示历史 / 屏幕上下文 / 静态字典建议；热键或悬停进入导航后，↑↓ 切换、Tab/Enter 插入；未导航时 Tab/↑↓ 交给终端。密码场景可关闭本开关。
              </p>
            </div>
            <ToggleSwitch
              :model-value="settings.completionEnabled"
              :disabled="saving"
              @update:model-value="toggleCompletion"
            />
          </div>
          <div
            v-if="settings.completionEnabled"
            class="px-5 py-4 border-t border-slate-800/60 space-y-3"
          >
            <div class="flex items-start justify-between gap-4">
              <div class="min-w-0">
                <p class="text-sm text-slate-300">补全导航热键</p>
                <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                  面板打开时用于开启 / 关闭导航；默认 Alt+↓。点击右侧后按下新组合键。
                </p>
              </div>
              <div class="shrink-0 flex items-center gap-2">
                <button
                  type="button"
                  class="min-w-[7.5rem] px-3 py-1.5 rounded-md border text-xs font-mono transition-colors"
                  :class="
                    capturingHotkey
                      ? 'border-sky-500/70 bg-sky-500/15 text-sky-300 animate-pulse'
                      : 'border-slate-700/60 bg-slate-800/60 text-slate-200 hover:border-sky-500/40'
                  "
                  :disabled="saving"
                  @click="startCaptureHotkey"
                >
                  {{
                    capturingHotkey
                      ? '按下组合键…'
                      : formatHotkeyLabel(settings.completionNavHotkey || DEFAULT_COMPLETION_NAV_HOTKEY)
                  }}
                </button>
                <button
                  type="button"
                  class="px-2.5 py-1.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs"
                  :disabled="saving || capturingHotkey"
                  @click="resetNavHotkey"
                >
                  默认
                </button>
              </div>
            </div>
            <p v-if="hotkeyCaptureError" class="text-xs text-rose-400">{{ hotkeyCaptureError }}</p>
            <p v-else-if="capturingHotkey" class="text-[12px] text-slate-500">Esc 取消录制</p>
            <div class="flex items-start justify-between gap-4 pt-1">
              <div class="min-w-0">
                <p class="text-sm text-slate-300">补全面板条数</p>
                <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                  历史 / 屏幕 / 字典合计最多展示条数（3–30，默认 8）。
                </p>
              </div>
              <input
                type="number"
                min="3"
                max="30"
                step="1"
                class="w-16 shrink-0 px-2 py-1.5 rounded-md border border-slate-700/60 bg-slate-800/60 text-xs font-mono text-slate-200 outline-none focus:border-sky-500/50"
                :value="settings.completionPanelLimit || 8"
                :disabled="saving"
                @change="onPanelLimitChange"
              />
            </div>
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">终端→SFTP 目录同步</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                终端执行 cd 或提示符路径变化时，SFTP 面板跟随跳转到同一目录。
              </p>
            </div>
            <ToggleSwitch
              :model-value="settings.terminalToSftpSync"
              :disabled="saving"
              @update:model-value="toggleTerminalSftpSync"
            />
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">SFTP→终端目录同步</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                在 SFTP 面板进入目录时，自动向终端发送 cd 命令同步当前路径。
              </p>
            </div>
            <ToggleSwitch
              :model-value="settings.sftpToTerminalSync"
              :disabled="saving"
              @update:model-value="toggleSftpSync"
            />
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">界面缩放</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                整体等比缩放界面（含布局与字号），适配不同屏幕尺寸；范围 80%–150%，默认 100%。
              </p>
            </div>
            <div class="shrink-0 flex items-center gap-2">
              <button
                type="button"
                class="px-2.5 py-1.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs"
                :disabled="saving || settings.uiScale <= 80"
                @click="setUIScale(settings.uiScale - 10)"
              >
                −
              </button>
              <span class="w-14 text-center text-xs font-mono text-slate-200">{{ settings.uiScale }}%</span>
              <button
                type="button"
                class="px-2.5 py-1.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs"
                :disabled="saving || settings.uiScale >= 150"
                @click="setUIScale(settings.uiScale + 10)"
              >
                +
              </button>
              <button
                type="button"
                class="px-2.5 py-1.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs"
                :disabled="saving || settings.uiScale === 100"
                @click="setUIScale(100)"
              >
                默认
              </button>
            </div>
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">自动重连</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                检测到 SSH 连接断开后自动重新连接（网络抖动、NAT 超时等情况）。
              </p>
            </div>
            <ToggleSwitch
              :model-value="settings.autoReconnect"
              :disabled="saving"
              @update:model-value="toggleAutoReconnect"
            />
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">终端保活心跳</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                每 15 秒向服务器发送心跳包，防止 NAT / 防火墙或服务端 ClientAliveInterval 超时断开终端。
              </p>
            </div>
            <ToggleSwitch
              :model-value="settings.keepAliveEnabled"
              :disabled="saving"
              @update:model-value="toggleKeepAlive"
            />
          </div>
        </div>

        <div class="neo">
          <div class="flex items-center justify-between gap-4 px-5 py-4">
            <div class="min-w-0">
              <p class="text-sm font-medium text-slate-200">清理命令历史</p>
              <p class="text-xs text-slate-500 mt-1 leading-relaxed">
                删除本地 SQLite 中记录的命令历史（不影响远程服务器 ~/.bash_history）。用于清除误记的控制字符、缺字命令或隐私命令。
              </p>
            </div>
            <div class="shrink-0 flex items-center gap-2">
              <template v-if="!confirmClearHistory">
                <button
                  type="button"
                  class="btn btn-ghost btn-sm"
                  :disabled="clearingHistory"
                  @click="confirmClearHistory = true"
                >
                  清空全部
                </button>
              </template>
              <template v-else>
                <button
                  type="button"
                  class="btn btn-danger btn-sm"
                  :disabled="clearingHistory"
                  @click="clearAllHistory"
                >
                  {{ clearingHistory ? '清理中…' : '确认清空' }}
                </button>
                <button
                  type="button"
                  class="btn btn-ghost btn-sm"
                  :disabled="clearingHistory"
                  @click="confirmClearHistory = false"
                >
                  取消
                </button>
              </template>
            </div>
          </div>
          <div v-if="historyError || historyMsg" class="px-5 pb-3 text-xs">
            <p v-if="historyError" class="text-rose-400">{{ historyError }}</p>
            <p v-else class="text-emerald-400">{{ historyMsg }}</p>
          </div>
        </div>
      </div>

      <!-- 终端主题 -->
      <div v-else-if="section === 'theme'" class="max-w-2xl fade-rise">
        <div class="mb-6">
          <h3 class="text-[18px] font-semibold text-white tracking-tight">终端主题</h3>
          <p class="text-[13px] text-mist mt-1.5">主题仅作用于 xterm 画布，外壳品牌色保持 Signal Desk 不变。</p>
        </div>

        <div class="neo">
          <div class="px-5 py-4 space-y-4 text-[13px]">
            <div class="grid grid-cols-2 gap-4">
              <label class="block">
                <span class="text-slate-400">背景色</span>
                <div class="mt-1 flex gap-2 items-center">
                <input v-model="themeForm.background" type="color" class="w-9 h-9 rounded-[6px] bg-[rgba(0,0,0,0.28)] p-1 cursor-pointer" />
                <input v-model="themeForm.background" class="input input-sm flex-1 font-mono" />
                </div>
              </label>
              <label class="block">
                <span class="text-slate-400">文字颜色</span>
                <div class="mt-1 flex gap-2 items-center">
                  <input v-model="themeForm.foreground" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" />
                  <input v-model="themeForm.foreground" class="input input-sm flex-1 font-mono" />
                </div>
              </label>
              <label class="block">
                <span class="text-slate-400">光标颜色</span>
                <div class="mt-1 flex gap-2 items-center">
                  <input v-model="themeForm.cursor" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" />
                  <input v-model="themeForm.cursor" class="input input-sm flex-1 font-mono" />
                </div>
              </label>
              <label class="block">
                <span class="text-slate-400">选中背景色</span>
                <input v-model="themeForm.selection" class="mt-1 w-full px-2.5 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 font-mono text-xs outline-none focus:border-sky-500/60" placeholder="rgba(56, 189, 248, 0.25)" />
              </label>
            </div>

            <div>
              <span class="text-slate-400">背景图</span>
              <div class="mt-1 flex gap-2">
                <input v-model="themeForm.bgImage" readonly class="flex-1 min-w-0 px-2.5 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 text-xs outline-none" placeholder="无（可选）" />
                <button class="px-3 py-1.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-200 text-xs shrink-0" @click="pickBgImage">选择…</button>
                <button v-if="themeForm.bgImage" class="px-3 py-1.5 rounded-md bg-slate-700/70 hover:bg-rose-600/80 text-slate-300 text-xs shrink-0" @click="themeForm.bgImage = ''">清除</button>
              </div>
            </div>

            <label class="block">
              <span class="text-slate-400">背景模糊：{{ themeForm.blurAmount }}px</span>
              <input v-model.number="themeForm.blurAmount" type="range" min="0" max="30" class="mt-2 w-full accent-sky-500" />
            </label>

            <div class="flex items-center justify-between gap-4">
              <div>
                <p class="text-sm text-slate-300">文字阴影</p>
                <p class="text-xs text-slate-500 mt-0.5">为终端文字添加阴影，提升可读性。</p>
              </div>
              <ToggleSwitch v-model="themeForm.textShadow" />
            </div>

            <label class="block" :class="themeForm.textShadow ? '' : 'opacity-40 pointer-events-none'">
              <span class="text-slate-400">阴影强度：{{ themeForm.shadowBlur }}px</span>
              <input v-model.number="themeForm.shadowBlur" type="range" min="0" max="10" class="mt-2 w-full accent-sky-500" />
            </label>
          </div>

          <div class="flex justify-end gap-2 px-5 py-4 border-t border-slate-800/60">
            <button class="btn btn-ghost btn-sm" @click="resetTheme">恢复默认</button>
            <button class="btn btn-primary btn-sm" :disabled="saving" @click="applyTheme">
              {{ saving ? '保存中…' : '保存主题' }}
            </button>
          </div>
        </div>
      </div>

      <!-- 保存的凭证 -->
      <div v-else-if="section === 'credentials'" class="max-w-2xl fade-rise">
        <div class="mb-6">
          <h3 class="text-[18px] font-semibold text-white tracking-tight">保存的凭证</h3>
          <p class="text-[13px] text-mist mt-1.5">保存常用用户名密码或私钥，新建服务器时可直接选择自动填充。</p>
        </div>

        <div class="neo">
          <div class="px-5 py-4 space-y-2">
            <div v-if="!credentials.list.length" class="text-xs text-slate-500 py-2">暂无凭证，在下方添加。</div>
            <div
              v-for="c in credentials.list"
              :key="c.id"
              class="flex items-center justify-between gap-3 px-3 py-2 rounded-md bg-slate-800/50 border border-slate-700/40"
            >
              <div class="min-w-0">
                <p class="text-[13px] text-slate-200 truncate">{{ c.name }}</p>
                <p class="text-[12px] text-slate-500 truncate">
                  {{ c.user }}
                  <template v-if="c.authType === 'privateKey'"> · 私钥已保存</template>
                  <template v-else> · 密码已保存</template>
                </p>
              </div>
              <div v-if="confirmCredId !== c.id" class="flex items-center gap-1 shrink-0">
                <button class="px-2 py-1 rounded-[4px] btn-ghost text-xs h-auto" @click="confirmCredId = c.id">删除</button>
              </div>
              <div v-else class="flex items-center gap-1 shrink-0">
                <button class="px-2 py-1 rounded bg-rose-600/80 hover:bg-rose-500 text-white text-xs" @click="removeCredential(c.id)">确认</button>
                <button class="px-2 py-1 rounded bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs" @click="confirmCredId = ''">取消</button>
              </div>
            </div>

            <div class="pt-2 border-t border-slate-800/60 space-y-2">
              <p class="text-xs text-slate-400 pt-1">新增凭证</p>
              <div class="space-y-3">
                <div class="grid grid-cols-2 gap-2">
                  <input v-model="credForm.name" class="input input-sm" placeholder="名称，如：生产 root" />
                  <input v-model="credForm.user" class="input input-sm" placeholder="用户名" />
                </div>
                <div class="flex gap-2">
                  <button
                    class="flex-1 px-3 py-1.5 rounded-md border text-slate-300 text-xs transition-colors"
                    :class="credForm.authType === 'password' ? 'border-sky-500/70 bg-sky-500/10' : 'border-slate-700/60 bg-slate-800/60'"
                    @click="credForm.authType = 'password'"
                  >
                    密码
                  </button>
                  <button
                    class="flex-1 px-3 py-1.5 rounded-md border text-slate-300 text-xs transition-colors"
                    :class="credForm.authType === 'privateKey' ? 'border-sky-500/70 bg-sky-500/10' : 'border-slate-700/60 bg-slate-800/60'"
                    @click="credForm.authType = 'privateKey'"
                  >
                    私钥
                  </button>
                </div>
                <template v-if="credForm.authType === 'password'">
                  <input v-model="credForm.password" type="password" class="input input-sm" placeholder="密码" />
                </template>
                <template v-else>
                  <div class="flex gap-2">
                    <button
                      class="flex-1 px-3 py-1.5 rounded-md border text-slate-300 text-xs transition-colors"
                      :class="credForm.keySource === 'file' ? 'border-sky-500/70 bg-sky-500/10' : 'border-slate-700/60 bg-slate-800/60'"
                      @click="credForm.keySource = 'file'"
                    >
                      密钥文件
                    </button>
                    <button
                      class="flex-1 px-3 py-1.5 rounded-md border text-slate-300 text-xs transition-colors"
                      :class="credForm.keySource === 'content' ? 'border-sky-500/70 bg-sky-500/10' : 'border-slate-700/60 bg-slate-800/60'"
                      @click="credForm.keySource = 'content'"
                    >
                      粘贴内容
                    </button>
                  </div>
                  <div v-if="credForm.keySource === 'file'" class="flex gap-2">
                    <input v-model="credForm.keyPath" readonly class="flex-1 min-w-0 px-2.5 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 text-xs outline-none" placeholder="~/.ssh/id_rsa" />
                    <button class="px-3 py-1.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-200 text-xs shrink-0" @click="pickCredKeyFile">选择…</button>
                  </div>
                  <textarea v-else v-model="credForm.keyContent" rows="4" spellcheck="false" class="textarea font-mono" placeholder="-----BEGIN OPENSSH PRIVATE KEY-----&#10;..."></textarea>
                </template>
              </div>
              <div class="flex items-center justify-between">
                <p v-if="credError" class="text-xs text-rose-400 break-all">{{ credError }}</p>
                <button class="btn btn-primary btn-sm ml-auto" @click="addCredential">保存凭证</button>
              </div>
            </div>
          </div>
        </div>
      </div>

      <!-- 安全 -->
      <div v-else-if="section === 'security'" class="max-w-2xl space-y-6 fade-rise">
        <div>
          <h3 class="text-[18px] font-semibold text-white tracking-tight">安全</h3>
          <p class="text-[13px] text-mist mt-1.5">
            敏感字段 AES-256-GCM 加密；可选启动主密码（Argon2id）。
          </p>
        </div>

        <div class="rounded-xl border border-slate-700/60 bg-slate-900/60 px-5 py-4 text-xs space-y-2">
          <p class="text-slate-300">
            状态：
            <span :class="security?.unlocked ? 'text-emerald-400' : 'text-amber-400'">
              {{ security?.unlocked ? '已解锁' : '未解锁' }}
            </span>
          </p>
          <p class="text-slate-500">
            主密码：{{ security?.masterPasswordEnabled ? '已启用' : '未启用' }}
            · 钥匙串：{{ security?.keyringAvailable ? '可用' : '不可用（已回退本地密钥文件）' }}
          </p>
        </div>

        <div v-if="!security?.masterPasswordEnabled" class="neo">
          <div class="px-5 py-4 border-b border-slate-800/60">
            <p class="text-sm font-medium text-slate-200">启用启动主密码</p>
            <p class="text-xs text-slate-500 mt-1">开启后每次启动需输入密码才能访问服务器与凭证。</p>
          </div>
          <div class="px-5 py-4 space-y-3">
            <input v-model="secForm.password" type="password" class="input input-sm" placeholder="新主密码" />
            <input v-model="secForm.confirm" type="password" class="input input-sm" placeholder="确认主密码" />
            <div class="flex items-center justify-between">
              <p v-if="secError" class="text-xs text-rose-400">{{ secError }}</p>
              <p v-else-if="secMsg" class="text-xs text-emerald-400">{{ secMsg }}</p>
              <button class="btn btn-primary btn-sm" :disabled="secBusy" @click="enableMaster">
                {{ secBusy ? '处理中…' : '启用' }}
              </button>
            </div>
          </div>
        </div>

        <template v-else>
          <div class="neo">
            <div class="px-5 py-4 border-b border-slate-800/60">
              <p class="text-sm font-medium text-slate-200">更换主密码</p>
            </div>
            <div class="px-5 py-4 space-y-3">
              <input v-model="secForm.oldPassword" type="password" class="input input-sm" placeholder="当前主密码" />
              <input v-model="secForm.newPassword" type="password" class="input input-sm" placeholder="新主密码" />
              <input v-model="secForm.newConfirm" type="password" class="input input-sm" placeholder="确认新主密码" />
              <div class="flex justify-end">
                <button class="btn btn-primary btn-sm" :disabled="secBusy" @click="changeMaster">更换</button>
              </div>
            </div>
          </div>

          <div class="neo">
            <div class="px-5 py-4 border-b border-slate-800/60">
              <p class="text-sm font-medium text-slate-200">关闭主密码</p>
              <p class="text-xs text-slate-500 mt-1">关闭后主密钥改存系统钥匙串（失败时回退本地密钥文件）。</p>
            </div>
            <div class="px-5 py-4 space-y-3">
              <input v-model="secForm.oldPassword" type="password" class="input input-sm" placeholder="当前主密码" />
              <div class="flex items-center justify-between">
                <p v-if="secError" class="text-xs text-rose-400">{{ secError }}</p>
                <p v-else-if="secMsg" class="text-xs text-emerald-400">{{ secMsg }}</p>
                <button class="ml-auto px-4 py-1.5 rounded-md bg-slate-700/70 hover:bg-rose-600/80 text-slate-200 text-xs" :disabled="secBusy" @click="disableMaster">关闭主密码</button>
              </div>
            </div>
          </div>
        </template>
      </div>

      <!-- 导入导出 -->
      <div v-else class="max-w-2xl space-y-6 fade-rise">
        <div>
          <h3 class="text-[18px] font-semibold text-white tracking-tight">导入导出</h3>
          <p class="text-[13px] text-mist mt-1.5">使用加密的 .dingpack 在设备间迁移服务器、凭证与设置。</p>
        </div>

        <div class="neo">
          <div class="px-5 py-4 border-b border-slate-800/60">
            <p class="text-sm font-medium text-slate-200">导出 .dingpack</p>
          </div>
          <div class="px-5 py-4 space-y-3">
            <input v-model="packPass" type="password" class="input input-sm" placeholder="导出密码" />
            <input v-model="packPassConfirm" type="password" class="input input-sm" placeholder="确认导出密码" />
            <div class="flex justify-end">
              <button class="btn btn-primary btn-sm" :disabled="packBusy" @click="exportPack">
                {{ packBusy ? '处理中…' : '导出…' }}
              </button>
            </div>
          </div>
        </div>

        <div class="neo">
          <div class="px-5 py-4 border-b border-slate-800/60">
            <p class="text-sm font-medium text-slate-200">导入 .dingpack</p>
          </div>
          <div class="px-5 py-4 space-y-3">
            <input v-model="packPass" type="password" class="input input-sm" placeholder="导入密码" />
            <label class="flex items-center gap-2 text-xs text-slate-400">
              <input v-model="packOverwrite" type="checkbox" class="rounded border-slate-600" />
              同 ID 覆盖已有服务器 / 凭证
            </label>
            <div class="flex items-center justify-between">
              <p v-if="packError" class="text-xs text-rose-400 break-all">{{ packError }}</p>
              <p v-else-if="packMsg" class="text-xs text-emerald-400 break-all">{{ packMsg }}</p>
              <button class="btn btn-primary btn-sm" :disabled="packBusy" @click="importPack">
                导入…
              </button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
