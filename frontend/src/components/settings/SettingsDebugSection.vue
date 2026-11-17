<script lang="ts" setup>
import {computed, onMounted, ref} from 'vue'
import ToggleSwitch from '../ToggleSwitch.vue'
import {Button, Input, Textarea} from '../ui'
import {DebugInfo, GetCapabilities, SetCapability} from '../../../wailsjs/go/main/App'
import {useSettingsStore} from '../../stores/settings'
import type {CapabilitiesView, DebugSettings} from '../../types'

const settings = useSettingsStore()
const saving = ref(false)
const debugInfo = ref<Record<string, any> | null>(null)

async function refreshDebugInfo() {
  try {
    debugInfo.value = await DebugInfo()
  } catch {
    debugInfo.value = null
  }
}

async function setDebug(patch: Partial<DebugSettings>) {
  saving.value = true
  try {
    await settings.setDebug(patch)
    await refreshDebugInfo()
  } finally {
    saving.value = false
  }
}

const onDebugPortChange = (value?: string | number) => {
  const raw = Number(value)
  return setDebug({port: Number.isFinite(raw) ? raw : 8765})
}

// ---- MCP 能力（权限内核，对应 Go 端 internal/debugsrv 的能力位）----
const CAP_ORDER = ['terminal.input', 'ui.write', 'config.write', 'secrets.write', 'fs.remote.write', 'sudo.credential', 'lifecycle'] as const
const CAP_NEEDS_CONFIRM = new Set(['fs.remote.write', 'sudo.credential', 'lifecycle'])
const capInfo = ref<CapabilitiesView | null>(null)
const pendingCap = ref('')
const capError = ref('')
const capMsg = ref('')
const allowlistText = ref('')
const allowlistDirty = ref(false)

const capRows = computed(() => {
  const caps = capInfo.value?.caps ?? {}
  const labels = capInfo.value?.labels ?? {}
  const descs = capInfo.value?.descriptions ?? {}
  return CAP_ORDER.map((name) => ({
    name,
    label: labels[name] ?? name,
    desc: descs[name] ?? '',
    enabled: !!caps[name],
    needsConfirm: CAP_NEEDS_CONFIRM.has(name),
    blocked: (name === 'secrets.write' || name === 'sudo.credential') && !settings.debug.allowSecrets,
  }))
})

async function refreshCapabilities() {
  try {
    capInfo.value = (await GetCapabilities()) as CapabilitiesView
    if (!allowlistDirty.value) {
      allowlistText.value = (capInfo.value.sftpWriteAllowlist ?? []).join('\n')
    }
  } catch (e) {
    capInfo.value = null
    capError.value = String(e)
  }
}

async function toggleCapability(name: string, next: boolean) {
  capError.value = ''
  capMsg.value = ''
  if (next && CAP_NEEDS_CONFIRM.has(name)) {
    pendingCap.value = name
    return
  }
  await applyCapability(name, next)
}

async function applyCapability(name: string, next: boolean) {
  saving.value = true
  capError.value = ''
  capMsg.value = ''
  try {
    await SetCapability(name, next)
    pendingCap.value = ''
    if (name === 'terminal.input') await settings.setDebug({capTerminalInput: next})
    else if (name === 'ui.write') await settings.setDebug({capUiWrite: next})
    else if (name === 'config.write') await settings.setDebug({capConfigWrite: next})
    else if (name === 'secrets.write') await settings.setDebug({capSecretWrite: next})
    else if (name === 'fs.remote.write') await settings.setDebug({capRemoteFsWrite: next})
    else if (name === 'sudo.credential') await settings.setDebug({capSudoCredential: next})
    else if (name === 'lifecycle') await settings.setDebug({capLifecycle: next})
    await refreshCapabilities()
    capMsg.value = next ? `已开启「${capLabelOf(name)}」` : `已关闭「${capLabelOf(name)}」`
  } catch (e) {
    capError.value = String(e)
  } finally {
    saving.value = false
  }
}

const capLabelOf = (name: string): string => capInfo.value?.labels?.[name] ?? name

async function saveAllowlist() {
  saving.value = true
  capError.value = ''
  capMsg.value = ''
  try {
    const list = allowlistText.value
      .split('\n')
      .map((s) => s.trim().replace(/\/+$/, ''))
      .filter((s, i, arr) => s.length > 0 && arr.indexOf(s) === i)
    await settings.setDebug({sftpWriteAllowlist: list})
    allowlistDirty.value = false
    await refreshCapabilities()
    capMsg.value = list.length > 0 ? `白名单已保存（${list.length} 条）` : '白名单已清空：将禁止任何远端写入路径'
  } catch (e) {
    capError.value = String(e)
  } finally {
    saving.value = false
  }
}

onMounted(() => {
  void refreshDebugInfo()
  void refreshCapabilities()
})
</script>

<template>
  <section class="max-w-2xl space-y-6 fade-rise" data-section-content="debug" aria-labelledby="settings-debug-title">
    <div>
      <h3 id="settings-debug-title" class="text-[18px] font-semibold text-white tracking-tight">调试模式</h3>
      <p class="text-[13px] text-mist mt-1.5 leading-relaxed">
        在本地开一个控制面（HTTP，后续提供 MCP），供 AI / 自动化读取状态、驱动终端、订阅输出。
        默认关闭；开启后仅监听本机（可切换局域网），token 每次启动重新生成。
      </p>
    </div>

    <div class="neo border border-amber-500/30">
      <div class="px-5 py-4 flex items-start gap-3">
        <div class="min-w-0 debug-notice-text text-xs leading-relaxed">
          调试模式允许调用方读写终端；「允许执行 JS」等于把整个界面交出去。请只在可信环境开启，
          不用时立刻关闭。token 与端口写在
          <span class="font-mono">{{ debugInfo?.infoFile || '%APPDATA%\\ding-ssh\\debug.json' }}</span>。
        </div>
      </div>
    </div>

    <div class="neo">
      <div class="flex items-center justify-between gap-4 px-5 py-4">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-200">启用调试模式</p>
          <p class="text-xs text-slate-500 mt-1 leading-relaxed">关闭时完全不监听任何端口。修改后立即生效（无需重启）。</p>
        </div>
        <ToggleSwitch
          :model-value="settings.debug.enabled"
          :disabled="saving"
          @update:model-value="(v: boolean) => setDebug({enabled: v})"
        />
      </div>
      <div class="px-5 py-3 border-t border-slate-800/60 grid grid-cols-[minmax(0,1fr)_8rem] items-center gap-4">
        <span class="field-label">监听端口（0 = 自动挑空闲端口）</span>
        <Input
          type="number"
          min="0"
          max="65535"
          :model-value="settings.debug.port"
          :disabled="saving"
          aria-label="调试模式监听端口"
          @update:model-value="onDebugPortChange"
        />
      </div>
      <div class="flex items-center justify-between gap-4 px-5 py-4 border-t border-slate-800/60">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-200">允许局域网访问</p>
          <p class="text-xs text-slate-500 mt-1 leading-relaxed">关闭时只监听 127.0.0.1；开启后绑定 0.0.0.0（请配合 token 使用）。</p>
        </div>
        <ToggleSwitch
          :model-value="settings.debug.bindLan"
          :disabled="saving"
          @update:model-value="(v: boolean) => setDebug({bindLan: v})"
        />
      </div>
      <div class="flex items-center justify-between gap-4 px-5 py-4 border-t border-slate-800/60">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-200">允许执行 JS（危险）</p>
          <p class="text-xs text-slate-500 mt-1 leading-relaxed">
            放开 <span class="font-mono">/v1/eval</span>：可在页面上下文执行任意脚本，等于完全控制界面。默认关闭。
          </p>
        </div>
        <ToggleSwitch
          :model-value="settings.debug.allowEval"
          :disabled="saving"
          @update:model-value="(v: boolean) => setDebug({allowEval: v})"
        />
      </div>
      <div class="flex items-center justify-between gap-4 px-5 py-4 border-t border-slate-800/60">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-200">允许读取敏感数据</p>
          <p class="text-xs text-slate-500 mt-1 leading-relaxed">放开设置 / 服务器配置中可能含密钥字段的读取。默认关闭。</p>
        </div>
        <ToggleSwitch
          :model-value="settings.debug.allowSecrets"
          :disabled="saving"
          @update:model-value="(v: boolean) => setDebug({allowSecrets: v})"
        />
      </div>
    </div>

    <!-- MCP 能力（权限内核）：AI 能做什么，由这里的开关决定 -->
    <div class="neo">
      <div class="px-5 py-4 border-b border-slate-800/60">
        <p class="text-sm font-medium text-slate-200">MCP 能力</p>
        <p class="text-xs text-slate-500 mt-1 leading-relaxed">
          逐项控制 AI 能做什么。读取类能力永远允许（敏感字段仍受「允许读取敏感数据」控制）；
          关闭的能力会让工具返回中文拒绝原因，不会影响其余功能。
        </p>
      </div>
      <div
        v-for="row in capRows"
        :key="row.name"
        class="px-5 py-4 border-b border-slate-800/60 flex items-start justify-between gap-4"
      >
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-200">
            {{ row.label }}
            <span class="font-mono text-[11px] text-slate-500 ml-1">{{ row.name }}</span>
            <span v-if="row.needsConfirm" class="ml-2 text-[11px] text-amber-300/90">高风险 · 需二次确认</span>
          </p>
          <p class="text-xs text-slate-500 mt-1 leading-relaxed">{{ row.desc }}</p>
          <p v-if="row.blocked" class="text-[11px] text-amber-300/90 mt-1">
            需先开启「允许读取敏感数据」（由 secrets.read 控制）才能打开本能力。
          </p>
        </div>
        <div v-if="pendingCap === row.name" class="flex items-center gap-2 shrink-0">
          <Button variant="danger" size="sm" :disabled="saving" @click="applyCapability(row.name, true)">确认开启</Button>
          <Button variant="ghost" size="sm" :disabled="saving" @click="pendingCap = ''">取消</Button>
        </div>
        <ToggleSwitch
          v-else
          :model-value="row.enabled"
          :disabled="saving"
          @update:model-value="(v: boolean) => toggleCapability(row.name, v)"
        />
      </div>

      <!-- 远端文件写入的路径白名单：留空 = 禁止任何写入路径 -->
      <div class="px-5 py-4 border-b border-slate-800/60 space-y-2">
        <p class="text-sm font-medium text-slate-200">远端可写路径白名单</p>
        <p class="text-xs text-slate-500 leading-relaxed">
          一行一个<b>绝对路径前缀</b>，只有命中前缀的远端路径才允许写入 / 删除。
          <b>留空 = 禁止任何写入路径</b>；仅「远端文件写入」能力开启时生效。
        </p>
        <Textarea
          v-model="allowlistText"
          class="font-mono"
          :rows="3"
          spellcheck="false"
          placeholder="/srv/app&#10;/home/deploy/releases"
          aria-label="远端可写路径白名单"
          @input="allowlistDirty = true"
        />
        <div class="flex items-center gap-2">
          <Button variant="primary" size="sm" :disabled="saving" data-testid="settings-save-allowlist" @click="saveAllowlist">保存白名单</Button>
          <span class="text-[11px] text-slate-500">当前生效：{{ (capInfo?.sftpWriteAllowlist ?? []).join('、') || '（空，禁止任何写入路径）' }}</span>
        </div>
      </div>

      <div class="px-5 py-4">
        <p class="text-sm font-medium text-slate-200">破坏性操作两段式确认（固定开启）</p>
        <p class="text-xs text-slate-500 mt-1 leading-relaxed">
          只对<b>不可逆</b>操作生效（删服务器 / 删凭据 / 改隧道 / SFTP 写删 / 改设置 / 清日志 / 退出重载 / 开启敏感能力）。
          AI / 脚本必须先调用
          <span class="font-mono">confirm.prepare</span>
          拿到一次性 token（默认 120 秒有效、绑定本次参数），再用
          <span class="font-mono">confirm.commit</span>
          或把 token 传给对应工具才会执行；缺少 token 一律拒绝。此项<b>不提供开关</b>。
        </p>
        <p class="text-xs text-slate-500 mt-1 leading-relaxed">
          <b>可逆 / 高频操作不需要 token</b>：终端输入（
          <span class="font-mono">send_input</span>）、
          <span class="font-mono">eval_js</span>
          、界面写入只受上面的能力位约束 —— 否则 AI 管 SSH 与「改一点看一眼」的界面迭代都没法用。
        </p>
      </div>
      <div v-if="capError || capMsg" class="px-5 pb-4 text-xs space-y-1">
        <p v-if="capError" class="text-rose-400 break-all" role="alert">{{ capError }}</p>
        <p v-else-if="capMsg" class="text-emerald-400 break-all" role="status">{{ capMsg }}</p>
      </div>
    </div>

    <div class="neo">
      <div class="px-5 py-3 border-b border-slate-800/60 flex items-center justify-between">
        <span class="field-label">运行状态</span>
        <Button variant="ghost" size="sm" :disabled="saving" @click="refreshDebugInfo">刷新</Button>
      </div>
      <div class="px-5 py-4 text-xs space-y-2">
        <div class="flex items-center gap-2">
          <span class="w-2 h-2 rounded-full" :class="debugInfo?.running ? 'bg-emerald-400' : 'bg-slate-600'"></span>
          <span class="field-label">{{ debugInfo?.running ? '运行中' : '未运行' }}</span>
        </div>
        <div v-if="debugInfo?.url" class="font-mono break-all text-slate-300">{{ debugInfo.url }}</div>
        <div v-if="debugInfo?.lanUrl" class="font-mono break-all text-slate-500">局域网：{{ debugInfo.lanUrl }}</div>
        <div v-if="debugInfo?.token" class="font-mono break-all text-slate-500">token：{{ debugInfo.token }}</div>
        <p class="text-slate-500">{{ debugInfo?.hint }}</p>
        <p v-if="debugInfo?.url" class="text-slate-500">
          示例：<span class="font-mono break-all">curl -H "Authorization: Bearer &lt;token&gt;" {{ debugInfo.url }}/v1/terminals</span>
        </p>
      </div>
    </div>
  </section>
</template>

<style scoped>
.debug-notice-text{color:var(--foreground-secondary);overflow-wrap:anywhere}
</style>
