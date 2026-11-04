<script lang="ts" setup>
import {onMounted, ref, watch} from 'vue'
import ToggleSwitch from '../ToggleSwitch.vue'
import {Button, Input} from '../ui'
import {
  ANSI_FIELDS,
  MODE_OPTIONS,
  TERMINAL_FONT_OPTIONS,
  TONE_OPTIONS,
  UI_FONT_OPTIONS,
  useSettingsAppearance,
} from '../../composables/useSettingsAppearance'
import {useSettingsStore} from '../../stores/settings'
import {readableOn} from '../../theme/engine'

const settings = useSettingsStore()
const saving = ref(false)
const {
  themeForm,
  appearanceForm,
  fontsForm,
  currentTone,
  syncForms,
  selectPreset,
  setTone,
  setMode,
  resetAppearance,
  pickBgImage,
  PRESETS,
} = useSettingsAppearance(settings, saving)

/** 计算在色块上可读的文字颜色。 */
const contrastFor = readableOn

onMounted(() => {
  syncForms()
})

watch(() => settings.loaded, (loaded) => {
  if (loaded) syncForms()
})
</script>

<template>
  <section class="max-w-3xl fade-rise" data-section-content="theme" aria-labelledby="settings-theme-title">
    <div class="mb-6">
      <h3 id="settings-theme-title" class="text-[18px] font-semibold text-white tracking-tight">外观</h3>
      <p class="text-[13px] text-mist mt-1.5">
        选择预设主题一键套用整站配色；也可自定义界面、终端与字体。终端 ANSI 16 色会同步到 ls、vim、提示符等程序输出。
      </p>
    </div>

    <!-- 明暗模式 -->
    <div class="neo px-5 py-4 mb-4">
      <div class="flex items-center justify-between gap-4">
        <div class="min-w-0">
          <p class="text-sm font-medium text-slate-200">界面明暗</p>
          <p class="text-xs text-slate-500 mt-0.5">「跟随系统」时，界面与预设终端色板随系统明暗自动切换。</p>
        </div>
        <div class="seg shrink-0">
          <button
            v-for="opt in TONE_OPTIONS"
            :key="opt.key"
            type="button"
            :class="appearanceForm.baseTone === opt.key ? 'active' : ''"
            :aria-pressed="appearanceForm.baseTone === opt.key"
            @click="setTone(opt.key)"
          >
            {{ opt.label }}
          </button>
        </div>
      </div>
    </div>

    <!-- 预设 / 自定义 -->
    <div class="neo p-5 mb-4">
      <div class="flex items-center justify-between mb-4">
        <p class="text-sm font-medium text-slate-200">配色方案</p>
        <div class="seg shrink-0">
          <button
            v-for="m in MODE_OPTIONS"
            :key="m.key"
            type="button"
            :class="appearanceForm.mode === m.key ? 'active' : ''"
            :aria-pressed="appearanceForm.mode === m.key"
            @click="setMode(m.key)"
          >
            {{ m.label }}
          </button>
        </div>
      </div>

      <!-- 预设主题卡片 -->
      <div v-if="appearanceForm.mode === 'preset'" class="grid grid-cols-4 gap-3">
        <button
          v-for="p in PRESETS"
          :key="p.id"
          type="button"
          class="preset-card"
          :class="appearanceForm.presetId === p.id ? 'active' : ''"
          :aria-pressed="appearanceForm.presetId === p.id"
          @click="selectPreset(p.id)"
        >
          <span
            class="preset-swatch"
            :style="{background: `linear-gradient(135deg, ${p.primary} 0%, ${p.secondary} 100%)`}"
          >
            <span class="preset-swatch-dark" :style="{background: p.dark.background}"></span>
            <span class="preset-swatch-cursor" :style="{background: p.dark.cursor}"></span>
          </span>
          <span class="preset-name">{{ p.name }}</span>
          <span class="preset-desc">{{ p.desc }}</span>
        </button>
      </div>

      <!-- 自定义：UI 品牌色 -->
      <div v-else class="grid grid-cols-3 gap-4">
        <label class="block">
          <span class="field-label">主色（界面强调）</span>
          <div class="mt-1 flex gap-2 items-center">
            <input v-model="appearanceForm.primary" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" aria-label="主色" />
            <Input v-model="appearanceForm.primary" class="flex-1 font-mono" aria-label="主色 Hex" />
          </div>
        </label>
        <label class="block">
          <span class="field-label">辅色（点缀）</span>
          <div class="mt-1 flex gap-2 items-center">
            <input v-model="appearanceForm.secondary" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" aria-label="辅色" />
            <Input v-model="appearanceForm.secondary" class="flex-1 font-mono" aria-label="辅色 Hex" />
          </div>
        </label>
        <label class="block">
          <span class="field-label">界面主文字色</span>
          <div class="mt-1 flex gap-2 items-center">
            <input v-model="appearanceForm.uiText" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" aria-label="界面主文字色" />
            <Input v-model="appearanceForm.uiText" class="flex-1 font-mono" aria-label="界面主文字色 Hex" />
          </div>
        </label>
      </div>
    </div>

    <!-- 终端颜色 -->
    <div class="neo p-5 mb-4">
      <div class="flex items-center justify-between mb-1">
        <p class="text-sm font-medium text-slate-200">终端颜色</p>
        <span v-if="appearanceForm.mode === 'preset'" class="badge live">
          预设 · {{ currentTone === 'light' ? '浅色' : '深色' }}色板
        </span>
        <span v-else class="badge stop">自定义</span>
      </div>
      <p v-if="appearanceForm.mode === 'preset'" class="text-xs text-slate-500 mt-1 mb-4">
        预设模式下终端色板（含 ANSI 16 色）随明暗自动配置。切到「自定义」可完全手动调整。
      </p>

      <!-- 预设模式：终端色板只读预览 -->
      <div v-if="appearanceForm.mode === 'preset'">
        <div class="grid grid-cols-2 gap-3 text-[12px] text-slate-400">
          <div class="flex items-center gap-2">
            <span class="color-dot" :style="{background: themeForm.background}"></span>
            背景 {{ themeForm.background }}
          </div>
          <div class="flex items-center gap-2">
            <span class="color-dot" :style="{background: themeForm.foreground}"></span>
            前景 {{ themeForm.foreground }}
          </div>
        </div>
        <div class="flex flex-wrap gap-1.5 mt-3">
          <span
            v-for="f in ANSI_FIELDS"
            :key="f.key"
            class="ansi-chip"
            :style="{background: themeForm[f.key], color: contrastFor(themeForm[f.key])}"
          >
            {{ f.label }}
          </span>
        </div>
      </div>

      <!-- 自定义模式：完整编辑 -->
      <template v-else>
        <div class="grid grid-cols-2 gap-4 text-[13px]">
          <label class="block">
            <span class="field-label">背景色</span>
            <div class="mt-1 flex gap-2 items-center">
              <input v-model="themeForm.background" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" aria-label="背景色" />
              <Input v-model="themeForm.background" class="flex-1 font-mono" aria-label="背景色 Hex" />
            </div>
          </label>
          <label class="block">
            <span class="field-label">文字颜色</span>
            <div class="mt-1 flex gap-2 items-center">
              <input v-model="themeForm.foreground" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" aria-label="文字颜色" />
              <Input v-model="themeForm.foreground" class="flex-1 font-mono" aria-label="文字颜色 Hex" />
            </div>
          </label>
          <label class="block">
            <span class="field-label">光标颜色</span>
            <div class="mt-1 flex gap-2 items-center">
              <input v-model="themeForm.cursor" type="color" class="w-9 h-9 rounded-md bg-slate-800 border border-slate-700/60 p-1 cursor-pointer" aria-label="光标颜色" />
              <Input v-model="themeForm.cursor" class="flex-1 font-mono" aria-label="光标颜色 Hex" />
            </div>
          </label>
          <label class="block">
            <span class="field-label">选中背景色</span>
            <Input v-model="themeForm.selection" class="mt-1 font-mono" placeholder="例如：半透明品牌色" aria-label="选中背景色" />
          </label>
        </div>

        <div class="mt-5">
          <p class="text-slate-400 text-xs mb-2">ANSI 16 色 · 程序输出（ls / vim / 提示符）</p>
          <div class="grid grid-cols-8 gap-2">
            <label v-for="f in ANSI_FIELDS" :key="f.key" class="block">
              <span class="text-[10px] text-slate-400">{{ f.label }}</span>
              <input
                v-model="themeForm[f.key]"
                type="color"
                class="mt-0.5 w-full h-7 rounded-[5px] bg-slate-800 border border-slate-700/60 p-0.5 cursor-pointer"
                :aria-label="`ANSI ${f.label}`"
              />
            </label>
          </div>
        </div>

        <div class="mt-5">
          <span class="field-label">背景图</span>
          <div class="mt-1 flex gap-2">
            <Input v-model="themeForm.bgImage" readonly class="flex-1" placeholder="无（可选）" aria-label="终端背景图路径" />
            <Button variant="secondary" size="sm" @click="pickBgImage">选择…</Button>
            <Button v-if="themeForm.bgImage" variant="ghost" size="sm" @click="themeForm.bgImage = ''">清除</Button>
          </div>
        </div>

        <label class="block mt-4">
          <span class="field-label">背景模糊：{{ themeForm.blurAmount }}px</span>
          <input v-model.number="themeForm.blurAmount" type="range" min="0" max="30" class="mt-2 w-full accent-[var(--primary-hover)]" />
        </label>

        <div class="flex items-center justify-between gap-4 mt-4">
          <div>
            <p class="text-sm text-slate-300">文字阴影</p>
            <p class="text-xs text-slate-500 mt-0.5">为终端文字添加阴影，提升可读性。</p>
          </div>
          <ToggleSwitch v-model="themeForm.textShadow" />
        </div>

        <label class="block mt-4" :class="themeForm.textShadow ? '' : 'opacity-40 pointer-events-none'">
          <span class="field-label">阴影强度：{{ themeForm.shadowBlur }}px</span>
          <input v-model.number="themeForm.shadowBlur" type="range" min="0" max="10" class="mt-2 w-full accent-[var(--primary-hover)]" />
        </label>
      </template>
    </div>

    <!-- 字体 -->
    <div class="neo p-5 mb-4">
      <p class="text-sm font-medium text-slate-200 mb-4">字体</p>
      <div class="grid grid-cols-2 gap-4">
        <label class="block">
          <span class="field-label">界面字体</span>
          <Input
            v-model="fontsForm.uiFont"
            list="uiFontList"
            class="mt-1 font-mono"
            placeholder="Geist Sans / system"
            aria-label="界面字体"
          />
          <datalist id="uiFontList">
            <option v-for="f in UI_FONT_OPTIONS" :key="f" :value="f" />
          </datalist>
        </label>
        <label class="block">
          <span class="field-label">终端等宽字体</span>
          <Input
            v-model="fontsForm.terminalFont"
            list="termFontList"
            class="mt-1 font-mono"
            placeholder="Geist Mono / system"
            aria-label="终端等宽字体"
          />
          <datalist id="termFontList">
            <option v-for="f in TERMINAL_FONT_OPTIONS" :key="f" :value="f" />
          </datalist>
        </label>
      </div>
      <label class="block mt-4">
        <span class="field-label">终端字号：{{ fontsForm.terminalFontSize }}px</span>
        <input
          v-model.number="fontsForm.terminalFontSize"
          type="range"
          min="8"
          max="24"
          class="mt-2 w-full accent-[var(--primary-hover)]"
        />
      </label>
      <p class="text-xs text-slate-500 mt-2">已捆绑的开源字体开箱即用；输入系统已安装字体名亦可生效（字体名需与系统一致）。</p>
    </div>

    <div class="flex justify-end gap-2">
      <Button variant="ghost" size="sm" @click="resetAppearance">恢复默认</Button>
    </div>
  </section>
</template>

<style scoped>
.preset-card{display:flex;flex-direction:column;align-items:flex-start;gap:6px;padding:12px;border-radius:var(--radius-md);background:var(--hover);box-shadow:inset 0 0 0 1px var(--border);transition:box-shadow var(--duration-base) var(--ease-out),background var(--duration-base) var(--ease-out)}
.preset-card:hover{background:var(--hover-strong)}
.preset-card.active{background:var(--primary-weak);box-shadow:inset 0 0 0 1px var(--primary-strong-border)}
.preset-swatch{position:relative;width:100%;height:44px;border-radius:var(--radius-sm);overflow:hidden;box-shadow:inset 0 0 0 1px var(--preview-border)}
.preset-swatch-dark{position:absolute;left:6px;top:6px;width:22px;height:22px;border-radius:4px;box-shadow:inset 0 0 0 1px var(--preview-border-strong)}
.preset-swatch-cursor{position:absolute;right:6px;bottom:6px;width:10px;height:10px;border-radius:2px}
.preset-name{font-size:12.5px;font-weight:600;color:var(--foreground)}
.preset-desc{font-size:11px;color:var(--muted-foreground);line-height:1.4}
.ansi-chip{display:inline-flex;align-items:center;height:22px;padding:0 8px;border-radius:var(--radius-sm);font-family:var(--font-mono);font-size:10.5px;font-weight:600;box-shadow:inset 0 0 0 1px var(--preview-border)}
.color-dot{width:14px;height:14px;border-radius:3px;flex-shrink:0;box-shadow:inset 0 0 0 1px var(--preview-border-strong)}
</style>
