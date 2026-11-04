<script lang="ts" setup>
import {computed, onBeforeUnmount, onMounted, ref} from 'vue'
import Icon from '../Icon.vue'
import {Button} from '../ui'
import {sshService} from '../../services/ssh'
import {APP_COMMIT, APP_VERSION, buildDateLabel, buildTimeLabel, detectPlatform, platformLabel} from '../../version'
import {ClipboardSetText} from '../../../wailsjs/runtime/runtime'

const platform = ref('')
const copied = ref(false)
let copiedTimer: number | undefined

const rows = computed(() => [
  {label: '版本标签', value: APP_VERSION || 'dev'},
  {label: '构建日期', value: buildDateLabel()},
  {label: '构建时间', value: buildTimeLabel()},
  {label: '代码提交', value: APP_COMMIT || '未知'},
  {label: '运行平台', value: platformLabel(platform.value || detectPlatform())},
])

async function copyAbout() {
  const text = ['ding-ssh', ...rows.value.map((r) => `${r.label}: ${r.value}`)].join('\n')
  try {
    await ClipboardSetText(text)
    copied.value = true
    window.clearTimeout(copiedTimer)
    copiedTimer = window.setTimeout(() => {
      copied.value = false
    }, 1600)
  } catch {
    /* clipboard unavailable */
  }
}

onMounted(async () => {
  try {
    platform.value = await sshService.getPlatform()
  } catch {
    platform.value = ''
  }
})

onBeforeUnmount(() => window.clearTimeout(copiedTimer))
</script>

<template>
  <section class="max-w-2xl space-y-6 fade-rise" data-section-content="about" aria-labelledby="settings-about-title">
    <div>
      <h3 id="settings-about-title" class="text-[18px] font-semibold text-white tracking-tight">关于</h3>
      <p class="text-[13px] text-mist mt-1.5">版本信息与构建详情，反馈问题时请一并附上。</p>
    </div>

    <div class="neo">
      <div class="flex items-center gap-3 px-5 py-4 border-b border-slate-800/60">
        <div class="brand-mark">
          <Icon name="zap" :size="20" extra-class="text-signal" />
        </div>
        <div class="min-w-0">
          <div class="brand-name">ding<span>-ssh</span></div>
          <p class="text-xs text-slate-500 mt-0.5">Local-first SSH workstation</p>
        </div>
        <span class="chip ml-auto">{{ rows[0].value }}</span>
      </div>
      <div class="px-5 py-2">
        <div
          v-for="row in rows"
          :key="row.label"
          class="flex items-center justify-between gap-4 py-2.5 border-b border-slate-800/40 last:border-b-0"
        >
          <span class="text-xs text-slate-500 shrink-0">{{ row.label }}</span>
          <span class="font-mono text-xs text-slate-200 text-right break-all">{{ row.value }}</span>
        </div>
      </div>
    </div>

    <div class="flex items-center gap-3">
      <Button variant="ghost" size="sm" @click="copyAbout">
        <Icon name="copy" :size="14" />
        {{ copied ? '已复制' : '复制版本信息' }}
      </Button>
      <p class="text-xs text-slate-500">构建时间取本次打包时的时间戳，可用于确认是否运行了最新版本。</p>
    </div>
  </section>
</template>
