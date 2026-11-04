<script lang="ts" setup>
import {computed, onMounted, ref, watch} from 'vue'
import Icon from '../Icon.vue'
import ToggleSwitch from '../ToggleSwitch.vue'
import {Button, Input} from '../ui'
import {TERMINAL_FONT_OPTIONS, TONE_OPTIONS, useSettingsAppearance} from '../../composables/useSettingsAppearance'
import {useSettingsStore} from '../../stores/settings'
import {hexToRgb} from '../../theme/engine'

const settings = useSettingsStore()
const saving = ref(false)
const picker = ref<HTMLElement | null>(null)
const dragging = ref(false)
const {themeForm, appearanceForm, fontsForm, syncForms, setTone, setSeedColor, resetAppearance} = useSettingsAppearance(settings, saving)
const PRESET_COLORS = [
  {name: 'Neutral', color: '#E8E4DF'}, {name: 'Rose', color: '#F19BC0'},
  {name: 'Lavender', color: '#9E709F'}, {name: 'Coral', color: '#F15E6A'},
  {name: 'Orange', color: '#FF805C'}, {name: 'Yellow', color: '#F4D258'},
  {name: 'Mint', color: '#35DBA4'}, {name: 'Sky', color: '#71B5D2'},
  {name: 'Slate', color: '#64698C'},
]
const hsv = computed(() => rgbToHsv(hexToRgb(appearanceForm.primary)))
const markerStyle = computed(() => ({left: `${hsv.value.s * 100}%`, top: `${(1 - hsv.value.v) * 100}%`}))
const pickerStyle = computed(() => ({backgroundColor: `hsl(${hsv.value.h} 100% 50%)`}))
const modeIcon = computed(() => appearanceForm.baseTone === 'light' ? 'sun' : appearanceForm.baseTone === 'dark' ? 'moon' : 'sparkles')

onMounted(syncForms)
watch(() => settings.loaded, (loaded) => { if (loaded) syncForms() })

function rgbToHsv({r, g, b}: {r: number; g: number; b: number}) {
  const rr = r / 255, gg = g / 255, bb = b / 255
  const max = Math.max(rr, gg, bb), min = Math.min(rr, gg, bb), d = max - min
  let h = 0
  if (d) {
    if (max === rr) h = 60 * (((gg - bb) / d) % 6)
    else if (max === gg) h = 60 * ((bb - rr) / d + 2)
    else h = 60 * ((rr - gg) / d + 4)
  }
  return {h: h < 0 ? h + 360 : h, s: max ? d / max : 0, v: max}
}
function hsvToHex(h: number, s: number, v: number) {
  const c = v * s, x = c * (1 - Math.abs((h / 60) % 2 - 1)), m = v - c
  let rgb = [0, 0, 0]
  if (h < 60) rgb = [c, x, 0]
  else if (h < 120) rgb = [x, c, 0]
  else if (h < 180) rgb = [0, c, x]
  else if (h < 240) rgb = [0, x, c]
  else if (h < 300) rgb = [x, 0, c]
  else rgb = [c, 0, x]
  return `#${rgb.map((n) => Math.round((n + m) * 255).toString(16).padStart(2, '0')).join('')}`.toUpperCase()
}
function updateFromPointer(event: PointerEvent) {
  const rect = picker.value?.getBoundingClientRect()
  if (!rect) return
  const s = Math.min(1, Math.max(0, (event.clientX - rect.left) / rect.width))
  const v = 1 - Math.min(1, Math.max(0, (event.clientY - rect.top) / rect.height))
  void setSeedColor(hsvToHex(hsv.value.h, s, v))
}
function startPicker(event: PointerEvent) {
  dragging.value = true
  picker.value?.setPointerCapture(event.pointerId)
  updateFromPointer(event)
}
function chooseColor(color: string) {
  void setSeedColor(color)
}
</script>

<template>
  <section class="theme-page fade-rise" data-section-content="theme" aria-labelledby="settings-theme-title">
    <header class="theme-header">
      <div><span class="theme-kicker">APPEARANCE</span><h3 id="settings-theme-title">主题</h3><p>选择一个颜色，ding-ssh 会自动生成协调、清晰的完整界面。</p></div>
      <span class="save-state"><span :class="{saving}"></span>{{ saving ? '正在保存' : '已自动保存' }}</span>
    </header>
    <div class="theme-layout">
      <section class="theme-editor">
        <div class="tone-switch" aria-label="界面明暗模式">
          <button v-for="opt in TONE_OPTIONS" :key="opt.key" type="button" :class="{active: appearanceForm.baseTone === opt.key}" :aria-label="opt.label" :aria-pressed="appearanceForm.baseTone === opt.key" @click="setTone(opt.key)">
            <Icon :name="opt.key === 'auto' ? 'sparkles' : opt.key === 'light' ? 'sun' : 'moon'" :size="18" />
          </button>
        </div>
        <div ref="picker" class="color-field" :style="pickerStyle" role="slider" tabindex="0" aria-label="主题颜色二维选择器" @pointerdown="startPicker" @pointermove="dragging && updateFromPointer($event)" @pointerup="dragging = false" @pointercancel="dragging = false">
          <span class="field-white"></span><span class="field-black"></span>
          <span class="color-marker" :style="markerStyle"><span :style="{background: appearanceForm.primary}"></span></span>
        </div>
        <label class="hue-control"><span>自定义色相</span><input type="range" min="0" max="359" :value="hsv.h" aria-label="自定义色相" @input="setSeedColor(hsvToHex(Number(($event.target as HTMLInputElement).value), Math.max(hsv.s, 0.05), Math.max(hsv.v, 0.05)))" /></label>
        <label class="native-color"><span>取色盘 · 任意主色</span><input type="color" :value="appearanceForm.primary" aria-label="自定义主色取色盘" @input="chooseColor(($event.target as HTMLInputElement).value)" /></label>
        <div class="color-meta">
          <div class="preset-row" aria-label="预设主题色">
            <button v-for="preset in PRESET_COLORS" :key="preset.name" type="button" class="preset-dot" :class="{active: appearanceForm.primary.toUpperCase() === preset.color}" :style="{background: preset.color}" :title="preset.name" :aria-label="preset.name" @click="chooseColor(preset.color)"></button>
          </div>
          <label class="hex-field"><span>HEX</span><Input v-model="appearanceForm.primary" aria-label="主题颜色 HEX" @change="chooseColor(appearanceForm.primary)" /></label>
        </div>
        <div class="slider-list">
          <label><span><strong>玻璃通透度</strong><output>{{ 100 - appearanceForm.glassOpacity }}%</output></span><input :value="100 - appearanceForm.glassOpacity" @input="appearanceForm.glassOpacity = 100 - Number(($event.target as HTMLInputElement).value)" :disabled="!appearanceForm.glassEnabled" type="range" min="0" max="85" aria-label="玻璃通透度" /></label>
          <label class="glass-toggle"><span><strong>桌面毛玻璃</strong><ToggleSwitch :model-value="!!appearanceForm.glassEnabled" @update:model-value="appearanceForm.glassEnabled = $event" /></span></label>
          <p class="glass-help">透出并模糊窗口后的背景，模糊程度由系统控制。关闭后使用纯色背景。</p>
        </div>
      </section>
      <aside class="theme-preview">
        <div class="preview-head"><div><span class="theme-kicker">LIVE PREVIEW</span><h4>界面预览</h4></div><span><Icon :name="modeIcon" :size="13" /> {{ appearanceForm.baseTone === 'auto' ? '自动' : appearanceForm.baseTone === 'light' ? '浅色' : '深色' }}</span></div>
        <div class="preview-window">
          <div class="preview-sidebar"><i></i><b></b><b></b><b></b></div>
          <div class="preview-main"><div class="preview-toolbar"></div><div class="preview-card"><span></span><span></span><span></span></div><div class="preview-terminal" :style="{background: themeForm.background}"><i :style="{background: themeForm.blue}"></i><i :style="{background: themeForm.green}"></i><i :style="{background: themeForm.yellow}"></i></div></div>
        </div>
        <div class="preview-note"><Icon name="terminal" :size="15" /><span><strong>终端保持清晰</strong>全窗口共享毛玻璃背景，文字保持清晰；独立终端背景与图片优先保留。</span></div>
      </aside>
    </div>
        <section class="terminal-font-controls" aria-label="终端字体设置">
          <h4>终端字体</h4>
          <label>终端字体<select v-model="fontsForm.terminalFont" aria-label="终端字体"><option v-for="font in TERMINAL_FONT_OPTIONS" :key="font" :value="font">{{ font === 'system' ? '系统等宽字体' : font }}{{ font !== 'system' ? ' + 思源黑体中文' : '' }}</option></select></label>
          <label>字号<input v-model.number="fontsForm.terminalFontSize" type="number" min="10" max="32" aria-label="终端字号" /></label>
          <p :style="{fontFamily: `'${fontsForm.terminalFont}', 'Noto Sans SC', monospace`, fontSize: `${fontsForm.terminalFontSize}px`}">终端预览 中文 你好 / English 0123456789 ~ % →</p>
        </section>
    <footer class="theme-footer"><p>更改会实时应用到整个应用。</p><Button variant="ghost" size="sm" @click="resetAppearance">恢复默认</Button></footer>
  </section>
</template>

<style scoped>
.terminal-font-controls{margin:20px 0;padding:22px;background:var(--theme-bg-surface);border:1px solid var(--border-subtle);border-radius:var(--radius-card)}.terminal-font-controls h4{margin:0 0 18px;font-size:14px}.terminal-font-controls label{display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;font-size:12px}.terminal-font-controls select,.terminal-font-controls input{background:var(--field-bg);color:var(--foreground);border:1px solid var(--border);padding:6px;border-radius:6px}.terminal-font-controls input{width:70px}.terminal-font-controls p{margin:8px 0;overflow-wrap:anywhere}.theme-page{max-width:1040px;padding-bottom:28px}.theme-header{display:flex;align-items:flex-end;justify-content:space-between;gap:24px;margin-bottom:24px}.theme-kicker{font:600 10px/1 var(--font-mono);letter-spacing:.16em;color:var(--muted-foreground)}.theme-header h3{margin:8px 0 5px;font-size:24px;line-height:1.1;letter-spacing:-.035em}.theme-header p{margin:0;color:var(--foreground-secondary);font-size:13px}.save-state{display:flex;align-items:center;gap:7px;color:var(--muted-foreground);font-size:11px}.save-state>span{width:6px;height:6px;border-radius:50%;background:var(--success)}.save-state>span.saving{background:var(--warning);animation:pulse 1s infinite}
.theme-layout{display:grid;grid-template-columns:minmax(420px,1.15fr) minmax(300px,.85fr);gap:16px}.theme-editor,.theme-preview{border:1px solid var(--border-subtle);border-radius:var(--radius-card);background:color-mix(in srgb,var(--surface-3) 40%,transparent);box-shadow:none}.theme-editor{padding:22px}.tone-switch{display:flex;justify-content:center;gap:5px;margin-bottom:18px}.tone-switch button{width:38px;height:34px;display:grid;place-items:center;border-radius:var(--radius-control);color:var(--muted-foreground);transition:background var(--dur),color var(--dur),transform var(--dur)}.tone-switch button:hover{color:var(--foreground);background:var(--hover)}.tone-switch button.active{color:var(--primary-hover);background:var(--primary-weak);box-shadow:inset 0 0 0 1px var(--primary-border)}
.color-field{position:relative;height:300px;border-radius:var(--radius-card);overflow:hidden;cursor:crosshair;touch-action:none;box-shadow:inset 0 0 0 1px rgb(0 0 0 / 7%)}.field-white,.field-black{position:absolute;inset:0;pointer-events:none}.field-white{background:linear-gradient(90deg,#fff,transparent)}.field-black{background:linear-gradient(0deg,#16131a,transparent)}.color-marker{position:absolute;width:32px;height:32px;border:4px solid white;border-radius:50%;box-shadow:0 2px 10px rgb(0 0 0 / 35%);transform:translate(-50%,-50%);pointer-events:none}.color-marker span{display:block;width:100%;height:100%;border-radius:50%}
.hue-control,.native-color{display:flex;align-items:center;gap:16px;margin-top:16px;font-size:12px;color:var(--foreground-secondary)}.hue-control input{flex:1;appearance:none;height:12px;border-radius:999px;background:linear-gradient(90deg,#f00,#ff0,#0f0,#0ff,#00f,#f0f,#f00)}.native-color input{width:54px;height:32px;padding:2px;border:1px solid var(--border);border-radius:6px;background:transparent}.color-meta{display:flex;align-items:center;justify-content:space-between;gap:16px;margin:18px 0 22px}.preset-row{display:flex;gap:9px;flex-wrap:wrap}.preset-dot{width:27px;height:27px;border-radius:50%;box-shadow:inset 0 0 0 1px rgb(0 0 0 / 8%);transition:transform var(--dur),box-shadow var(--dur)}.preset-dot:hover{transform:scale(1.08)}.preset-dot.active{box-shadow:0 0 0 2px var(--surface-2),0 0 0 4px var(--foreground)}.hex-field{display:flex;align-items:center;gap:7px;flex-shrink:0}.hex-field>span{font:600 9px var(--font-mono);color:var(--muted-foreground)}.hex-field :deep(input){width:88px;height:30px;font:11px var(--font-mono);text-transform:uppercase}
.glass-help{margin:0;color:var(--muted-foreground);font-size:12px}.slider-list .glass-toggle input{appearance:auto;width:18px;height:18px}.slider-list{display:grid;gap:22px}.slider-list label>span{display:flex;justify-content:space-between;align-items:center;margin-bottom:9px}.slider-list strong{font-size:12.5px;font-weight:500}.slider-list output{font:12px var(--font-mono);color:var(--muted-foreground)}.slider-list input{width:100%;height:5px;appearance:none;border-radius:999px;background:var(--hover-strong);accent-color:var(--primary)}.slider-list input::-webkit-slider-thumb{appearance:none;width:16px;height:16px;border:3px solid var(--surface-2);border-radius:50%;background:var(--primary);box-shadow:0 0 0 1px var(--primary-border),0 2px 6px rgb(0 0 0 / 20%)}
.theme-preview{align-self:start;padding:22px}.preview-head{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:18px}.preview-head h4{margin:7px 0 0;font-size:14px}.preview-head>span{display:flex;align-items:center;gap:5px;padding:5px 8px;border-radius:var(--radius-pill);background:var(--hover);color:var(--muted-foreground);font-size:10px}.preview-window{height:270px;display:grid;grid-template-columns:34% 1fr;overflow:hidden;border:1px solid var(--border-subtle);border-radius:var(--radius-card);background:var(--theme-bg-main);box-shadow:0 10px 24px rgb(0 0 0 / 10%)}.preview-sidebar{padding:14px 10px;background:var(--theme-bg-sidebar)}.preview-sidebar i{display:block;width:55%;height:7px;margin-bottom:17px;border-radius:var(--radius-control);background:var(--primary);opacity:.65}.preview-sidebar b{display:block;height:27px;margin-bottom:7px;border-radius:var(--radius-control);background:var(--hover-strong)}.preview-main{padding:12px;background:var(--theme-bg-main)}.preview-toolbar{height:28px;margin-bottom:10px;border-radius:var(--radius-control);background:var(--theme-bg-toolbar)}.preview-card{height:70px;padding:12px;margin-bottom:10px;border-radius:var(--radius-card);background:var(--theme-bg-surface)}.preview-card span{display:block;width:75%;height:6px;margin-bottom:8px;border-radius:var(--radius-small);background:var(--border-strong);opacity:.55}.preview-card span:nth-child(2){width:52%}.preview-card span:nth-child(3){width:63%}.preview-terminal{height:125px;padding:16px;border-radius:var(--radius-card);background:var(--terminal-bg,#11151b)}.preview-terminal i{display:block;width:60%;height:5px;margin-bottom:9px;border-radius:var(--radius-small);background:#8fb0f2;opacity:.7}.preview-terminal i:nth-child(2){width:78%;background:#7ad8a3}.preview-terminal i:nth-child(3){width:42%;background:#d4a04a}.preview-note{display:flex;gap:10px;margin-top:16px;padding:13px;border-radius:var(--radius-card);background:var(--hover);color:var(--primary-hover)}.preview-note span{color:var(--muted-foreground);font-size:10.5px;line-height:1.45}.preview-note strong{display:block;margin-bottom:2px;color:var(--foreground-secondary);font-size:11px}.theme-footer{display:flex;align-items:center;justify-content:space-between;margin-top:14px;padding:0 4px}.theme-footer p{margin:0;color:var(--muted-foreground);font-size:11px}
@keyframes pulse{50%{opacity:.35}}@media(max-width:900px){.theme-layout{grid-template-columns:1fr}.theme-preview{width:100%}}@media(max-width:600px){.theme-header{align-items:flex-start;flex-direction:column}.theme-layout{display:block}.theme-preview{margin-top:14px}.color-field{height:240px}.color-meta{align-items:flex-start;flex-direction:column}}
</style>
