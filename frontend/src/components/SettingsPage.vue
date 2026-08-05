<script lang="ts" setup>
import {onMounted, reactive, ref} from 'vue'
import {sshService} from '../services/ssh'
import {useCredentialsStore} from '../stores/credentials'
import {defaultTheme, useSettingsStore} from '../stores/settings'
import type {Theme} from '../types'
import ToggleSwitch from './ToggleSwitch.vue'

const settings = useSettingsStore()
const credentials = useCredentialsStore()
const saving = ref(false)
const themeForm = reactive<Theme>(defaultTheme())
const credForm = reactive({name: '', user: '', password: ''})
const credError = ref('')
const confirmCredId = ref('')

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

async function applyTheme() {
  saving.value = true
  try {
    await settings.setTheme({...themeForm})
  } finally {
    saving.value = false
  }
}

function resetTheme() {
  Object.assign(themeForm, defaultTheme())
}

async function pickBgImage() {
  try {
    const path = await sshService.selectImageFile()
    if (path) themeForm.bgImage = path
  } catch (e) {
    // 用户取消或选择失败时静默处理
  }
}

async function addCredential() {
  if (!credForm.name.trim() || !credForm.user.trim() || !credForm.password) {
    credError.value = '请填写凭证名称、用户名和密码'
    return
  }
  credError.value = ''
  await credentials.save({id: '', ...credForm})
  credForm.name = ''
  credForm.user = ''
  credForm.password = ''
}

async function removeCredential(id: string) {
  confirmCredId.value = ''
  await credentials.remove(id)
}

onMounted(async () => {
  await Promise.all([settings.load(), credentials.load()])
  // 回填已保存的主题到表单
  Object.assign(themeForm, settings.theme)
})
</script>

<template>
  <div class="h-full overflow-y-auto">
    <div class="max-w-2xl mx-auto px-8 py-8 space-y-6">
      <div>
        <h2 class="text-lg font-semibold text-slate-100">设置</h2>
        <p class="text-xs text-slate-500 mt-1">应用偏好设置，修改后即时生效。</p>
      </div>

      <!-- 通用 -->
      <div class="rounded-xl border border-slate-700/60 bg-slate-900/60">
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

      <div class="rounded-xl border border-slate-700/60 bg-slate-900/60">
        <div class="flex items-center justify-between gap-4 px-5 py-4">
          <div class="min-w-0">
            <p class="text-sm font-medium text-slate-200">选中内容自动复制</p>
            <p class="text-xs text-slate-500 mt-1 leading-relaxed">开启后在终端中选中文本，内容将自动复制到剪贴板。</p>
          </div>
          <ToggleSwitch :model-value="settings.copyOnSelect" :disabled="saving" @update:model-value="toggleCopy" />
        </div>
      </div>

      <!-- 终端主题 -->
      <div class="rounded-xl border border-slate-700/60 bg-slate-900/60">
        <div class="px-5 py-4 border-b border-slate-800/60">
          <p class="text-sm font-medium text-slate-200">终端主题</p>
          <p class="text-xs text-slate-500 mt-1">自定义终端颜色、背景图与文字阴影，点击「保存主题」后生效。</p>
        </div>

        <div class="px-5 py-4 space-y-4 text-[13px]">
          <div class="grid grid-cols-2 gap-4">
            <label class="block">
              <span class="text-slate-400">背景色</span>
              <div class="mt-1 flex gap-2 items-center">
                <input v-model="themeForm.background" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" />
                <input v-model="themeForm.background" class="flex-1 min-w-0 px-2.5 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 font-mono text-xs outline-none focus:border-sky-500/60" />
              </div>
            </label>
            <label class="block">
              <span class="text-slate-400">文字颜色</span>
              <div class="mt-1 flex gap-2 items-center">
                <input v-model="themeForm.foreground" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" />
                <input v-model="themeForm.foreground" class="flex-1 min-w-0 px-2.5 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 font-mono text-xs outline-none focus:border-sky-500/60" />
              </div>
            </label>
            <label class="block">
              <span class="text-slate-400">光标颜色</span>
              <div class="mt-1 flex gap-2 items-center">
                <input v-model="themeForm.cursor" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" />
                <input v-model="themeForm.cursor" class="flex-1 min-w-0 px-2.5 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 font-mono text-xs outline-none focus:border-sky-500/60" />
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
          <button class="px-4 py-1.5 rounded-md bg-slate-700/70 hover:bg-slate-600 text-slate-200 text-xs" @click="resetTheme">恢复默认</button>
          <button class="px-4 py-1.5 rounded-md bg-sky-500/80 hover:bg-sky-400 text-slate-900 font-medium text-xs" :disabled="saving" @click="applyTheme">
            {{ saving ? '保存中…' : '保存主题' }}
          </button>
        </div>
      </div>

      <!-- 凭证管理 -->
      <div class="rounded-xl border border-slate-700/60 bg-slate-900/60">
        <div class="px-5 py-4 border-b border-slate-800/60">
          <p class="text-sm font-medium text-slate-200">保存的凭证</p>
          <p class="text-xs text-slate-500 mt-1">保存常用用户名密码，新建服务器时可直接选择。</p>
        </div>

        <div class="px-5 py-4 space-y-2">
          <div v-if="!credentials.list.length" class="text-xs text-slate-500 py-2">暂无凭证，在下方添加。</div>
          <div
            v-for="c in credentials.list"
            :key="c.id"
            class="flex items-center justify-between gap-3 px-3 py-2 rounded-md bg-slate-800/50 border border-slate-700/40"
          >
            <div class="min-w-0">
              <p class="text-[13px] text-slate-200 truncate">{{ c.name }}</p>
              <p class="text-[11px] text-slate-500 truncate">{{ c.user }} · 密码已保存</p>
            </div>
            <div v-if="confirmCredId !== c.id" class="flex items-center gap-1 shrink-0">
              <button class="px-2 py-1 rounded bg-slate-700/70 hover:bg-rose-600/80 text-slate-300 text-xs" @click="confirmCredId = c.id">删除</button>
            </div>
            <div v-else class="flex items-center gap-1 shrink-0">
              <button class="px-2 py-1 rounded bg-rose-600/80 hover:bg-rose-500 text-white text-xs" @click="removeCredential(c.id)">确认</button>
              <button class="px-2 py-1 rounded bg-slate-700/70 hover:bg-slate-600 text-slate-300 text-xs" @click="confirmCredId = ''">取消</button>
            </div>
          </div>

          <div class="pt-2 border-t border-slate-800/60 space-y-2">
            <p class="text-xs text-slate-400 pt-1">新增凭证</p>
            <div class="grid grid-cols-3 gap-2">
              <input v-model="credForm.name" class="px-2.5 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 text-xs outline-none focus:border-sky-500/60" placeholder="名称，如：生产 root" />
              <input v-model="credForm.user" class="px-2.5 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 text-xs outline-none focus:border-sky-500/60" placeholder="用户名" />
              <input v-model="credForm.password" type="password" class="px-2.5 py-1.5 rounded-md bg-slate-800 border border-slate-700/60 text-slate-200 text-xs outline-none focus:border-sky-500/60" placeholder="密码" />
            </div>
            <div class="flex items-center justify-between">
              <p v-if="credError" class="text-xs text-rose-400 break-all">{{ credError }}</p>
              <button class="ml-auto px-4 py-1.5 rounded-md bg-sky-500/80 hover:bg-sky-400 text-slate-900 font-medium text-xs" @click="addCredential">保存凭证</button>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
