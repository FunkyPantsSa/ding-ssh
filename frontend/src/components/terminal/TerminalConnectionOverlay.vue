<script setup lang="ts">
import {ref} from 'vue'
import Icon from '../Icon.vue'
import {Button} from '../ui'
import type {SessionTab} from '../../types'

type Step = { status: 'pending' | 'running' | 'done' | 'error'; logs: string[]; expanded: boolean; message: string }
defineProps<{
  tab: SessionTab
  settings: { autoReconnect: boolean }
  steps: Record<string, Step>
  connectSteps: ReadonlyArray<{key: string; label: string}>
  activeStepLabel: string
  anyStepError: boolean
  autoReconnectCountdown: number
  autoReconnectAttempt: number
}>()
const emit = defineEmits<{
  close: []
  connect: []
  reconnect: []
  cancelAutoReconnect: []
  toggleStep: [key: string]
}>()
const statusPanel = ref<HTMLElement | null>(null)
const disconnectedPanel = ref<HTMLElement | null>(null)
defineExpose({focus: () => (statusPanel.value ?? disconnectedPanel.value)?.focus()})
</script>

<template>
  <div
    v-if="tab.status === 'connecting' || tab.status === 'error'"
    ref="statusPanel"
    class="absolute inset-0 z-30 grid place-items-center pointer-events-auto overlay-backdrop"
    tabindex="0"
  >
    <div class="neo w-[min(460px,92%)] p-6 max-h-[86vh] overflow-y-auto">
      <div class="flex items-center justify-between gap-3 mb-1">
        <h3 class="text-[15px] font-semibold text-[var(--foreground)] truncate">
          {{ tab.kind === 'local' ? (tab.status === 'connecting' ? `正在打开 ${tab.serverName}` : `打开 ${tab.serverName} 失败`) : (tab.status === 'connecting' ? `正在连接 ${tab.serverName}` : `连接 ${tab.serverName} 失败`) }}
        </h3>
        <Button variant="ghost" size="icon" class="shrink-0" title="关闭标签页" aria-label="关闭标签页" data-testid="terminal-close-session" @click.stop="emit('close')">
          <Icon name="close" :size="14" />
        </Button>
      </div>
      <p v-if="tab.status === 'error' && tab.message" class="text-xs text-[var(--destructive)] break-all mb-3">{{ tab.message }}</p>
      <template v-if="tab.kind === 'local'">
        <p class="text-xs text-mist mb-4">{{ tab.status === 'connecting' ? '正在启动本机 Shell…' : '可关闭后重试，或在设置中更换本机 Shell。' }}</p>
        <div class="flex gap-2">
          <Button v-if="tab.status === 'error'" variant="primary" size="sm" data-testid="terminal-retry" @click="emit('connect')">重试</Button>
          <Button variant="ghost" size="sm" @click="emit('close')">关闭</Button>
        </div>
      </template>
      <template v-else>
        <p class="text-xs text-mist mb-4">
          <template v-if="activeStepLabel">当前：<span class="text-[var(--foreground)]">{{ activeStepLabel }}</span><span v-if="anyStepError" class="text-[var(--destructive)]"> · 已失败</span></template>
          <template v-else-if="tab.status === 'connecting'">解析主机 → 鉴权 → 打开 PTY → 同步环境</template>
        </p>
        <div class="conn-steps">
          <div v-for="(s, i) in connectSteps" :key="s.key" class="conn-step" :class="[steps[s.key].status, {open: steps[s.key].expanded}]">
            <button class="conn-step-head" type="button" @click="emit('toggleStep', s.key)" :aria-expanded="steps[s.key].expanded">
              <span class="n"><Icon v-if="steps[s.key].status === 'done'" name="check" :size="12" /><span v-else>{{ i + 1 }}</span></span>
              <span class="flex-1 min-w-0 text-left truncate">{{ s.label }}</span>
              <span v-if="steps[s.key].status === 'running'" class="badge run">进行中</span>
              <span v-else-if="steps[s.key].status === 'error'" class="badge err">失败</span>
              <span v-else-if="steps[s.key].status === 'done'" class="badge done">完成</span>
              <Icon v-if="steps[s.key].logs.length" name="chevron-down" :size="14" extra-class="chev transition-transform" :class="steps[s.key].expanded ? 'open' : ''" />
            </button>
            <div v-if="steps[s.key].expanded && steps[s.key].logs.length" class="conn-step-logs">
              <p v-for="(l, j) in steps[s.key].logs" :key="j" class="log-line" :class="j === steps[s.key].logs.length - 1 && steps[s.key].status === 'error' ? 'err' : ''">{{ l }}</p>
              <p v-if="steps[s.key].status === 'error' && steps[s.key].message" class="log-line err">{{ steps[s.key].message }}</p>
              <p v-else-if="steps[s.key].status === 'running'" class="log-line hint">等待该步骤完成…</p>
            </div>
          </div>
        </div>
        <div class="flex gap-2 mt-5">
          <template v-if="tab.status === 'connecting'">
            <Button variant="ghost" size="sm" @click.stop="emit('close')">关闭标签页</Button>
          </template>
          <template v-else>
            <Button variant="primary" size="sm" data-testid="terminal-retry" @click.stop="emit('reconnect')">重试连接</Button>
            <Button variant="ghost" size="sm" @click.stop="emit('close')">关闭标签页</Button>
          </template>
        </div>
      </template>
    </div>
  </div>
  <div
    v-else-if="tab.status === 'closed' || tab.status === 'disconnected'"
    ref="disconnectedPanel"
    class="absolute inset-x-3 top-3 z-30 pointer-events-auto disconnect-banner flex items-center gap-3 px-4 py-2.5"
    tabindex="0"
    @keydown.enter.prevent="emit('reconnect')"
  >
    <span class="banner-accent" aria-hidden="true"></span>
    <div class="min-w-0 flex-1">
      <p class="text-[14px] font-semibold leading-snug truncate text-[var(--warning)]">
        {{ tab.kind === 'local' ? '本机终端已结束' : '连接已断开' }}<span class="font-normal text-[12px] text-[var(--foreground)]"> · {{ tab.serverName }}</span>
      </p>
      <p v-if="tab.message" class="mt-0.5 text-[11px] font-normal text-[var(--foreground-secondary)] truncate">{{ tab.message }}</p>
      <p v-if="tab.status === 'disconnected' && settings.autoReconnect && autoReconnectCountdown > 0" class="mt-0.5 text-[11px] text-amber-400 flex items-center gap-1.5">
        <svg class="w-3 h-3 animate-spin" fill="none" viewBox="0 0 24 24"><circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4"/><path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z"/></svg>
        {{ autoReconnectAttempt > 1 ? `第 ${autoReconnectAttempt - 1} 次失败，` : '' }}{{ autoReconnectCountdown }}s 后自动重连…
      </p>
    </div>
    <div class="flex shrink-0 items-center gap-2">
      <Button variant="primary" size="sm" data-testid="terminal-reconnect" @click.stop="emit('reconnect')">立即重连</Button>
      <Button v-if="tab.status === 'disconnected' && settings.autoReconnect && autoReconnectCountdown > 0" variant="ghost" size="sm" @click.stop="emit('cancelAutoReconnect')">取消自动重连</Button>
      <Button variant="ghost" size="sm" @click.stop="emit('close')">关闭标签页</Button>
    </div>
  </div>
</template>
