<script lang="ts" setup>
import {onBeforeUnmount, onMounted, ref} from 'vue'
import Icon from '../Icon.vue'
import ToggleSwitch from '../ToggleSwitch.vue'
import {Button, Input, Select} from '../ui'
import {
  DEFAULT_COMPLETION_NAV_HOTKEY,
  formatHotkeyLabel,
  hotkeyFromEvent,
} from '../../completion/hotkey'
import {historyService} from '../../services/history'
import {sshService} from '../../services/ssh'
import {useSettingsStore} from '../../stores/settings'
import type {LocalShellOption, NavSectionOrder, RightClickAction, TabBarPlacement, TerminalColorMode} from '../../types'

const settings = useSettingsStore()
const saving = ref(false)
const localShellOptions = ref<LocalShellOption[]>([])

const TAB_PLACEMENT_OPTIONS = [
  {key: 'side', label: '左侧导航'},
  {key: 'top', label: '顶栏'},
] as const
const RIGHT_CLICK_OPTIONS = [
  {key: 'menu', label: '打开选项栏'},
  {key: 'paste', label: '直接粘贴'},
] as const
const TERMINAL_COLOR_OPTIONS = [
  {key: 'off', label: '关'},
  {key: 'auto', label: '语义'},
  {key: 'vivid', label: '强调'},
] as const
const NAV_SECTION_ORDER_OPTIONS = [
  {value: 'nav,sessions,tabs', label: '导航 · 会话 · 标签页'},
  {value: 'nav,tabs,sessions', label: '导航 · 标签页 · 会话'},
  {value: 'sessions,nav,tabs', label: '会话 · 导航 · 标签页'},
  {value: 'sessions,tabs,nav', label: '会话 · 标签页 · 导航'},
  {value: 'tabs,nav,sessions', label: '标签页 · 导航 · 会话'},
  {value: 'tabs,sessions,nav', label: '标签页 · 会话 · 导航'},
] as const

async function set(fn: () => Promise<void>) {
  saving.value = true
  try {
    await fn()
  } finally {
    saving.value = false
  }
}

const toggleCopy = (v: boolean) => set(() => settings.setCopyOnSelect(v))
const toggleWebGL = (v: boolean) => set(() => settings.setWebGLEnabled(v))
const toggleCompletion = (v: boolean) => set(() => settings.setCompletionEnabled(v))
const toggleSftpSync = (v: boolean) => set(() => settings.setSftpToTerminalSync(v))
const toggleTerminalSftpSync = (v: boolean) => set(() => settings.setTerminalToSftpSync(v))
const toggleAutoReconnect = (v: boolean) => set(() => settings.setAutoReconnect(v))
const toggleKeepAlive = (v: boolean) => set(() => settings.setKeepAliveEnabled(v))
const toggleAutoLoginLastServer = (v: boolean) => set(() => settings.setAutoLoginLastServer(v))

async function loadLocalShellOptions() {
  try {
    localShellOptions.value = await sshService.getLocalShellOptions()
    if (!settings.localShell && localShellOptions.value.length) {
      // 仅填充 UI 默认展示，不立刻写库；用户改选时再保存
      settings.localShell = localShellOptions.value[0].value
    } else if (
      settings.localShell &&
      localShellOptions.value.length &&
      !localShellOptions.value.some((o) => o.value === settings.localShell)
    ) {
      settings.localShell = localShellOptions.value[0].value
    }
  } catch {
    localShellOptions.value = []
  }
}

const onLocalShellChange = (value?: string | number) => set(() => settings.setLocalShell(String(value)))
const setUIScale = (v: number) => set(() => settings.setUIScale(v))
const setTabBarPlacement = (v: TabBarPlacement) => set(() => settings.setTabBarPlacement(v))
const setRightClickAction = (v: RightClickAction) => set(() => settings.setRightClickAction(v))
const setNavSectionOrder = (v: NavSectionOrder) => set(() => settings.setNavSectionOrder(v))
const setTerminalColorMode = (v: TerminalColorMode) => set(() => settings.setTerminalColorMode(v))
const onNavSectionOrderChange = (value?: string | number) => setNavSectionOrder(value as NavSectionOrder)
const onPanelLimitChange = (value?: string | number) => set(() => settings.setCompletionPanelLimit(Number(value)))

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
  await set(() => settings.setCompletionNavHotkey(hk))
}

const resetNavHotkey = () => set(() => settings.setCompletionNavHotkey(DEFAULT_COMPLETION_NAV_HOTKEY))

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

onMounted(async () => {
  window.addEventListener('keydown', onHotkeyCapture, true)
  if (!settings.loaded) await settings.load()
  await loadLocalShellOptions()
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onHotkeyCapture, true)
})
</script>

<template>
  <section class="max-w-2xl space-y-6 fade-rise" data-section-content="general" aria-labelledby="settings-general-title">
    <div>
      <h3 id="settings-general-title" class="text-[18px] font-semibold text-white tracking-tight">通用</h3>
      <p class="text-[13px] text-mist mt-1.5 leading-relaxed">控制选中复制、鼠标右键行为、终端输出语义着色、导航区段顺序、补全热键与命令历史。</p>
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
          <p class="text-sm font-medium text-slate-200">鼠标右键行为</p>
          <p class="text-xs text-slate-500 mt-1 leading-relaxed">
            在终端中单击鼠标右键时：「打开选项栏」弹出复制 / 粘贴 / 全屏等菜单；「直接粘贴」把剪贴板内容写入终端。
            键盘粘贴始终可用（Ctrl+V / Ctrl+Shift+V / Shift+Insert）。
          </p>
        </div>
        <div class="seg shrink-0">
          <button
            v-for="opt in RIGHT_CLICK_OPTIONS"
            :key="opt.key"
            type="button"
            :class="settings.rightClickAction === opt.key ? 'active' : ''"
            :aria-pressed="settings.rightClickAction === opt.key"
            :disabled="saving"
            @click="setRightClickAction(opt.key)"
          >
            {{ opt.label }}
          </button>
        </div>
      </div>
    </div>

    <div class="neo">
      <div class="flex items-center justify-between gap-4 px-5 py-4">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-200">终端输出语义着色</p>
          <p class="text-xs text-slate-500 mt-1 leading-relaxed">
            按输出含义着色：错误红、警告黄、成功绿、信息青。「强调」额外高亮数值、路径与键名。
            只补充颜色，远端自带颜色（如 ls --color）与 vim / top 等全屏程序的画面始终原样显示。
          </p>
        </div>
        <div class="seg shrink-0">
          <button
            v-for="opt in TERMINAL_COLOR_OPTIONS"
            :key="opt.key"
            type="button"
            :class="settings.terminalColorMode === opt.key ? 'active' : ''"
            :aria-pressed="settings.terminalColorMode === opt.key"
            :disabled="saving"
            @click="setTerminalColorMode(opt.key)"
          >
            {{ opt.label }}
          </button>
        </div>
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
            <Button
              variant="ghost"
              size="sm"
              class="min-w-[7.5rem] font-mono"
              :class="capturingHotkey ? 'border-[var(--primary-strong-border)] bg-[var(--primary-weak)] text-signal animate-pulse' : ''"
              :disabled="saving"
              @click="startCaptureHotkey"
            >
              {{
                capturingHotkey
                  ? '按下组合键…'
                  : formatHotkeyLabel(settings.completionNavHotkey || DEFAULT_COMPLETION_NAV_HOTKEY)
              }}
            </Button>
            <Button variant="secondary" size="sm" :disabled="saving || capturingHotkey" @click="resetNavHotkey">默认</Button>
          </div>
        </div>
        <p v-if="hotkeyCaptureError" class="text-xs text-rose-400" role="alert">{{ hotkeyCaptureError }}</p>
        <p v-else-if="capturingHotkey" class="text-[12px] text-slate-500">Esc 取消录制</p>
        <div class="flex items-start justify-between gap-4 pt-1">
          <div class="min-w-0">
            <p class="text-sm text-slate-300">补全面板条数</p>
            <p class="text-xs text-slate-500 mt-1 leading-relaxed">
              历史 / 屏幕 / 字典合计最多展示条数（3–30，默认 8）。
            </p>
          </div>
          <div class="w-16 shrink-0">
            <Input
              type="number"
              min="3"
              max="30"
              step="1"
              class="font-mono"
              aria-label="补全面板条数"
              :model-value="settings.completionPanelLimit || 8"
              :disabled="saving"
              @update:model-value="onPanelLimitChange"
            />
          </div>
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
          <Button variant="secondary" size="sm" :disabled="saving || settings.uiScale <= 80" @click="setUIScale(settings.uiScale - 10)">−</Button>
          <span class="w-14 text-center text-xs font-mono text-slate-200">{{ settings.uiScale }}%</span>
          <Button variant="secondary" size="sm" :disabled="saving || settings.uiScale >= 150" @click="setUIScale(settings.uiScale + 10)">+</Button>
          <Button variant="secondary" size="sm" :disabled="saving || settings.uiScale === 100" @click="setUIScale(100)">默认</Button>
        </div>
      </div>
    </div>

    <div class="neo">
      <div class="flex items-center justify-between gap-4 px-5 py-4">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-200">会话标签页位置</p>
          <p class="text-xs text-slate-500 mt-1 leading-relaxed">
            默认「左侧导航」：标签并入左侧栏的纵向列表，可留出更多终端高度；「顶栏」为终端上方的横向标签栏。
          </p>
        </div>
        <div class="seg shrink-0">
          <button
            v-for="opt in TAB_PLACEMENT_OPTIONS"
            :key="opt.key"
            type="button"
            :class="settings.tabBarPlacement === opt.key ? 'active' : ''"
            :aria-pressed="settings.tabBarPlacement === opt.key"
            :disabled="saving"
            @click="setTabBarPlacement(opt.key)"
          >
            {{ opt.label }}
          </button>
        </div>
      </div>
    </div>

    <div class="neo">
      <div class="grid grid-cols-[minmax(0,1fr)_11rem] items-center gap-4 px-5 py-4">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-200">左侧导航区段顺序</p>
          <p class="text-xs text-slate-500 mt-1 leading-relaxed">
            自定义左侧导航中「导航 / 会话 / 标签页」三块的排列顺序（「标签页」仅在设为侧栏时显示）。
          </p>
        </div>
        <Select
          :model-value="settings.navSectionOrder"
          aria-label="左侧导航区段顺序"
          :disabled="saving"
          @update:model-value="onNavSectionOrderChange"
        >
          <option v-for="opt in NAV_SECTION_ORDER_OPTIONS" :key="opt.value" :value="opt.value">
            {{ opt.label }}
          </option>
        </Select>
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
          <p class="text-sm font-medium text-slate-200">启动时自动连接上次的服务器</p>
          <p class="text-xs text-slate-500 mt-1 leading-relaxed">
            开启后，重启应用（解锁完成）会自动重新打开上次关闭时仍在连接的服务器会话，并先弹确认框供你取消。
          </p>
        </div>
        <ToggleSwitch
          :model-value="settings.autoLoginLastServer"
          :disabled="saving"
          @update:model-value="toggleAutoLoginLastServer"
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
      <div class="grid grid-cols-[minmax(0,1fr)_11rem] items-center gap-4 px-5 py-4">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-200">本机终端 Shell</p>
          <p class="text-xs text-slate-500 mt-1 leading-relaxed">
            工作区「本地终端」使用的本机 Shell。macOS 可选 zsh / bash，Windows 可选 PowerShell / cmd，Linux 使用系统 $SHELL。
          </p>
        </div>
        <Select
          :model-value="settings.localShell"
          aria-label="本机终端 Shell"
          :disabled="saving || localShellOptions.length <= 1"
          @update:model-value="onLocalShellChange"
        >
          <option v-for="opt in localShellOptions" :key="opt.value" :value="opt.value">
            {{ opt.label }}
          </option>
        </Select>
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
            <Button variant="ghost" size="sm" :disabled="clearingHistory" @click="confirmClearHistory = true">清空全部</Button>
          </template>
          <template v-else>
            <Button variant="danger" size="sm" :loading="clearingHistory" @click="clearAllHistory">
              {{ clearingHistory ? '清理中…' : '确认清空' }}
            </Button>
            <Button variant="ghost" size="sm" :disabled="clearingHistory" @click="confirmClearHistory = false">取消</Button>
          </template>
        </div>
      </div>
      <div v-if="historyError || historyMsg" class="px-5 pb-3 text-xs">
        <p v-if="historyError" class="text-rose-400" role="alert">{{ historyError }}</p>
        <p v-else class="text-emerald-400" role="status">{{ historyMsg }}</p>
      </div>
    </div>
  </section>
</template>
