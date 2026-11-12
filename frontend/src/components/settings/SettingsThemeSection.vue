<script lang="ts" setup>
import {computed, onMounted, ref, watch} from 'vue'
import Icon from '../Icon.vue'
import SearchSelect from '../ui/select/SearchSelect.vue'
import ToggleSwitch from '../ToggleSwitch.vue'
import {Button, Input, Dialog} from '../ui'
import {MATERIAL_OPTIONS, TONE_OPTIONS, useSettingsAppearance} from '../../composables/useSettingsAppearance'
import {useSettingsStore} from '../../stores/settings'
import {PRESETS, paletteToTheme} from '../../theme/presets'
import {detectCapabilities, hexToRgb, monoFontStack} from '../../theme/engine'
import {TERMINAL_FONT_SCHEMES, loadTerminalFonts, fontPair, encodeFontPair} from '../../theme/terminal-fonts'
import {customFontSizes, formatFontSize, customFontChinese, customFonts, importCustomFont, restoreCustomFonts, removeCustomFont} from '../../theme/custom-fonts'
import {remoteFontProgress, remoteFontStatus} from '../../theme/remote-fonts'

const fontInput = ref<HTMLInputElement | null>(null)
const importError = ref('')
const showImportError = ref(false)
const importMessage = ref('')
const importing = ref(false)
void restoreCustomFonts().catch(() => { importMessage.value = '无法读取已导入字体' })
async function importFont(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  importing.value = true
  try { await importCustomFont(file); importMessage.value = '字体已导入并保存' }
  catch (error) {
    importError.value = error instanceof Error ? error.message : '字体导入失败，请重试。'
    importMessage.value = importError.value
    showImportError.value = true
  }
  finally { importing.value = false; input.value = '' }
}
const settings = useSettingsStore()
const saving = ref(false)
const picker = ref<HTMLElement | null>(null)
const dragging = ref(false)
const {themeForm, appearanceForm, fontsForm, syncForms, setTone, setSeedColor, selectPreset, setMaterialMode, setAtmosphere, setSeparation, resetAppearance} = useSettingsAppearance(settings, saving)
const materialCapabilities = detectCapabilities()
const materialFallback = computed(() => (appearanceForm.materialMode ?? 'solid') !== 'solid' && !materialCapabilities.nativeBackdrop)
const currentMaterial = computed(() => appearanceForm.materialMode ?? (appearanceForm.glassEnabled ? 'soft' : 'solid'))
const terminalPresetOptions = PRESETS.flatMap(preset => [
  {value: preset.id + ':dark', label: preset.name + ' · 深色', keywords: preset.id + ' dark'},
  {value: preset.id + ':light', label: preset.name + ' · 浅色', keywords: preset.id + ' light'},
])
function chooseTerminal(value: string) {
  const [id, tone] = value.split(':')
  const preset = PRESETS.find(p => p.id === id)
  if (preset) Object.assign(themeForm, paletteToTheme(tone === 'light' ? preset.light : preset.dark))
}
const PRESET_COLORS = [
  {name: '黑白', color: '#A3A3A3'},
  {name: '暖灰', color: '#E8E4DF'}, {name: '玫瑰粉', color: '#F19BC0'},
  {name: '薰衣草紫', color: '#9E709F'}, {name: '珊瑚红', color: '#F15E6A'},
  {name: '暖橙', color: '#FF805C'}, {name: '明黄', color: '#F4D258'},
  {name: '薄荷绿', color: '#35DBA4'}, {name: '天空蓝', color: '#71B5D2'},
  {name: '岩灰蓝', color: '#64698C'},
]
const rememberedHue = ref(0)
const hsv = computed(() => {
  const value = rgbToHsv(hexToRgb(appearanceForm.primary))
  return {...value, h: value.s > 0 && value.v > 0 ? value.h : rememberedHue.value}
})
watch(() => appearanceForm.primary, color => {
  const value = rgbToHsv(hexToRgb(color))
  if (value.s > 0 && value.v > 0) rememberedHue.value = value.h
}, {immediate: true})
function setHue(value: number) {
  rememberedHue.value = value
  void setSeedColor(hsvToHex(value, hsv.value.s, hsv.value.v))
}
const saturationGradient = computed(() => ({background: `linear-gradient(90deg, ${hsvToHex(hsv.value.h, 0, hsv.value.v)}, ${hsvToHex(hsv.value.h, 1, hsv.value.v)})`}))
const brightnessGradient = computed(() => ({background: `linear-gradient(90deg, #000000, ${hsvToHex(hsv.value.h, hsv.value.s, 1)})`}))
const markerStyle = computed(() => ({left: `${hsv.value.s * 100}%`, top: `${(1 - hsv.value.v) * 100}%`}))
const pickerStyle = computed(() => ({backgroundColor: `hsl(${hsv.value.h} 100% 50%)`}))
const modeIcon = computed(() => appearanceForm.baseTone === 'light' ? 'sun' : appearanceForm.baseTone === 'dark' ? 'moon' : 'sparkles')
const englishFont = computed({get: () => fontPair(fontsForm.terminalFont)[0], set: value => { fontsForm.terminalFont = encodeFontPair(value, chineseFont.value) }})
const chineseFont = computed({get: () => fontPair(fontsForm.terminalFont)[1], set: value => { fontsForm.terminalFont = encodeFontPair(englishFont.value, value) }})
const fontOptions = computed(() => [...new Set(['Geist Mono', 'JetBrains Mono', 'Fira Code', 'Maple Mono NF CN', 'Sarasa Mono SC', 'Noto Sans SC', 'system', ...customFonts.value])])
const chineseFontOptions = computed(() => fontOptions.value.filter(font => ['Maple Mono NF CN', 'Sarasa Mono SC', 'Noto Sans SC', 'system'].includes(font) || customFontChinese.value[font]))
function fontLabel(font: string) { return font === 'system' ? '系统默认' : font.replace('DingCustom-', '').replace(/-[a-f0-9]{12}$/, '') }
async function deleteFont(font: string) {
  try {
    await removeCustomFont(font)
    fontsForm.terminalFont = encodeFontPair(englishFont.value === font ? 'Geist Mono' : englishFont.value, chineseFont.value === font ? 'Noto Sans SC' : chineseFont.value)
    importMessage.value = '字体已删除'
  } catch { importMessage.value = '删除失败，请重试' }
}
const systemPlatform = navigator.platform || '未知系统'
const systemLanguage = navigator.language
const systemCores = navigator.hardwareConcurrency
const fontDownloadMessage = computed(() => {
  const states = Object.entries(remoteFontStatus).filter(([key]) => fontPair(fontsForm.terminalFont).some(f => key.startsWith(`${f}:`))).map(([, state]) => state)
  if (states.includes('error')) return '字体下载失败，暂用回退字体；重新选择此方案可重试。'
  if (states.includes('loading')) {
    const entries = Object.entries(remoteFontProgress).filter(([key]) => fontPair(fontsForm.terminalFont).some(f => key.startsWith(`${f}:`)))
    const done = entries.reduce((sum, [, p]) => sum + p.done, 0)
    const total = entries.reduce((sum, [, p]) => sum + p.total, 0)
    return `正在加载字体资源 ${done}/${total}，期间使用回退字体…`
  }
  return states.length ? '字体已就绪，已缓存的资源可离线复用。' : '首次使用在线下载字体，之后复用本地缓存。'
})
watch(() => [fontsForm.terminalFont, fontsForm.terminalFontSize] as const, ([name, size]) => {
  void loadTerminalFonts(name, size, 400).catch(() => { /* CSS fallback keeps the preview readable. */ })
}, {immediate: true})

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
  if (color === '#A3A3A3') void selectPreset('monochrome')
  else void setSeedColor(color)
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
        <div class="color-spectrum" aria-label="完整颜色选择">
          <div class="spectrum-heading"><strong>自定义颜色</strong><span>彩色 · 黑白 · 明暗</span></div>
          <label class="hue-control"><span>彩色色相</span><input type="range" min="0" max="359" :value="hsv.h" aria-label="自定义色相" @input="setHue(Number(($event.target as HTMLInputElement).value))" /><output>{{ Math.round(hsv.h) }}°</output></label>
          <label class="spectrum-control"><span>黑白灰阶</span><input class="grayscale-range" type="range" min="0" max="255" :value="Math.round(hsv.v * 255)" aria-label="黑白灰阶" @input="setSeedColor(hsvToHex(hsv.h, 0, Number(($event.target as HTMLInputElement).value) / 255))" /><output>{{ hsv.s === 0 ? Math.round(hsv.v * 100) + '%' : '灰阶' }}</output></label>
          <label class="spectrum-control"><span>饱和度</span><input :style="saturationGradient" type="range" min="0" max="100" :value="Math.round(hsv.s * 100)" aria-label="颜色饱和度" @input="setSeedColor(hsvToHex(hsv.h, Number(($event.target as HTMLInputElement).value) / 100, hsv.v))" /><output>{{ Math.round(hsv.s * 100) }}%</output></label>
          <label class="spectrum-control"><span>明度</span><input :style="brightnessGradient" type="range" min="0" max="100" :value="Math.round(hsv.v * 100)" aria-label="颜色明度" @input="setSeedColor(hsvToHex(hsv.h, hsv.s, Number(($event.target as HTMLInputElement).value) / 100))" /><output>{{ Math.round(hsv.v * 100) }}%</output></label>
          <p class="spectrum-help">灰阶从纯黑到纯白；提高饱和度可恢复彩色，调整明度可选择更亮或更暗的颜色。也可直接输入 HEX。</p>
        </div>
        <label class="native-color"><span>取色盘 · 任意主色</span><input type="color" :value="appearanceForm.primary" aria-label="自定义主色取色盘" @input="chooseColor(($event.target as HTMLInputElement).value)" /></label>
        <div class="color-meta">
          <div class="preset-row" aria-label="预设主题色">
            <span v-for="(preset, index) in PRESET_COLORS" :key="preset.name" class="preset-color-item">
              <button type="button" class="preset-dot" :class="{active: appearanceForm.primary.toUpperCase() === preset.color}" :style="{background: preset.color}" :aria-label="preset.name" :aria-describedby="`preset-color-name-${index}`" @click="chooseColor(preset.color)"></button>
              <span :id="`preset-color-name-${index}`" class="preset-color-name" role="tooltip">{{ preset.name }}</span>
            </span>
          </div>
          <label class="hex-field"><span>HEX</span><Input v-model="appearanceForm.primary" aria-label="主题颜色 HEX" @change="chooseColor(appearanceForm.primary)" /></label>
        </div>
        <div class="slider-list">
          <label><span><strong>主题氛围强度</strong><output>{{ appearanceForm.atmosphere ?? appearanceForm.colorIntensity ?? 75 }}%</output></span><input :value="appearanceForm.atmosphere ?? appearanceForm.colorIntensity ?? 75" @input="setAtmosphere(Number(($event.target as HTMLInputElement).value))" type="range" min="0" max="100" aria-label="主题氛围强度" /></label>
          <label><span><strong>区域层级差异</strong><output>{{ appearanceForm.surfaceSeparation ?? 65 }}%</output></span><input :value="appearanceForm.surfaceSeparation ?? 65" @input="setSeparation(Number(($event.target as HTMLInputElement).value))" type="range" min="0" max="100" aria-label="区域层级差异" /></label>
          <div class="material-block">
            <span class="material-label"><strong>界面材质</strong><small>{{ MATERIAL_OPTIONS.find(option => option.key === currentMaterial)?.desc }}</small></span>
            <div class="material-modes" role="radiogroup" aria-label="界面材质模式">
              <button v-for="option in MATERIAL_OPTIONS" :key="option.key" type="button" :class="{active: currentMaterial === option.key}" :aria-checked="currentMaterial === option.key" role="radio" @click="setMaterialMode(option.key)">{{ option.label }}</button>
            </div>
          </div>
          <label><span><strong>表面不透明度</strong><output>{{ appearanceForm.glassOpacity }}%</output></span><input :value="appearanceForm.glassOpacity" @input="appearanceForm.glassOpacity = Number(($event.target as HTMLInputElement).value)" :disabled="currentMaterial === 'solid'" type="range" min="60" max="100" aria-label="表面不透明度" /></label>
          <label><span><strong>浮层模糊</strong><output>{{ appearanceForm.glassBlur }}px</output></span><input v-model.number="appearanceForm.glassBlur" :disabled="currentMaterial === 'solid'" type="range" min="0" max="60" aria-label="浮层模糊" /></label>
          <p class="glass-help">氛围强度控制大面积背景偏离中性的程度；层级差异控制导航、工作区与卡片的区别。桌面模糊由系统原生材质提供，前端不会重复模拟全窗口模糊。</p>
          <p v-if="materialFallback" class="glass-help material-fallback" role="status">当前平台未提供系统背景模糊，已自动回退为实色表面。</p>
          <label><span><strong>终端跟随应用主题</strong></span><ToggleSwitch :model-value="appearanceForm.terminalBackgroundMode !== 'independent'" @update:model-value="appearanceForm.terminalBackgroundMode = $event ? 'follow' : 'independent'" /></label>
          <p class="glass-help">开启后终端背景、光标与选区跟随应用主题的明暗与种子色；ANSI 16 色和独立配色方案保持原样。关闭后完整使用终端方案自身的背景、光标与选区。</p>
          <div class="terminal-preset-field">
            <label for="terminal-preset"><strong>终端配色方案</strong></label>
            <SearchSelect id="terminal-preset" model-value="" placeholder="选择配色方案" :options="terminalPresetOptions" @update:model-value="chooseTerminal" />
          </div>
        </div>
      </section>
      <aside class="theme-preview">
        <div class="preview-head"><div><span class="theme-kicker">LIVE PREVIEW</span><h4>界面预览</h4></div><span><Icon :name="modeIcon" :size="13" /> {{ appearanceForm.baseTone === 'auto' ? '自动' : appearanceForm.baseTone === 'light' ? '浅色' : '深色' }}</span></div>
        <div class="preview-window">
          <div class="preview-sidebar"><i></i><b></b><b></b><b></b></div>
          <div class="preview-main"><div class="preview-toolbar"></div><div class="preview-card"><span></span><span></span><span></span></div><div class="preview-terminal" :style="{background: themeForm.background}"><i :style="{background: themeForm.blue}"></i><i :style="{background: themeForm.green}"></i><i :style="{background: themeForm.yellow}"></i></div></div>
        </div>
        <div class="preview-note"><Icon name="terminal" :size="15" /><span><strong>终端保持清晰</strong>各区域使用独立主题染色；终端始终使用可读实色层，独立终端配色优先。</span></div>
      </aside>
    </div>
    <section class="font-section" aria-labelledby="font-title">
      <div class="section-heading"><span class="theme-kicker">TYPOGRAPHY</span><h4 id="font-title">终端字体</h4><p>分别选择中英文字体，在右侧查看混排效果。</p></div>
      <div class="font-layout">
        <div class="font-editor">
          <div class="recommended-fonts"><div class="library-heading"><div><h5>默认推荐</h5><small>一键套用中英文字体组合</small></div></div><div class="recommendation-list"><button v-for="scheme in TERMINAL_FONT_SCHEMES" :key="scheme.value" type="button" :class="{active: fontsForm.terminalFont === scheme.value}" :aria-pressed="fontsForm.terminalFont === scheme.value" @click="fontsForm.terminalFont = scheme.value"><strong>{{ scheme.label }}</strong><small>{{ scheme.description }}</small></button></div></div>
          <div class="font-fields">
            <div class="font-select-field">
              <label for="terminal-english-font">英文与数字</label>
              <SearchSelect id="terminal-english-font" v-model="englishFont" :options="fontOptions.map(font => ({value: font, label: fontLabel(font), keywords: font}))" />
              <small>建议使用等宽字体，保持命令和表格对齐。</small>
            </div>
            <div class="font-select-field">
              <label for="terminal-chinese-font">中文字体</label>
              <SearchSelect id="terminal-chinese-font" v-model="chineseFont" :options="chineseFontOptions.map(font => ({value: font, label: fontLabel(font), keywords: font}))" />
              <small>仅显示支持中文的字体；自定义字体按常用汉字检测。</small>
            </div>
            <label>字号 <span class="size-control"><input v-model.number="fontsForm.terminalFontSize" type="number" min="10" max="32" aria-label="终端字号" /><span>px</span></span></label>
          </div>
          <p class="font-status" role="status">{{ fontDownloadMessage }}</p>
          <div class="font-library">
            <div class="library-heading"><div><h5>我的字体</h5><small>TTF · OTF · WOFF · WOFF2 · 不限文件大小</small></div><Button variant="ghost" size="sm" :disabled="importing" @click="fontInput?.click()">{{ importing ? '正在导入…' : '导入字体' }}</Button></div>
            <input ref="fontInput" hidden type="file" accept=".ttf,.otf,.woff,.woff2" @change="importFont" />
            <p v-if="!customFonts.length" class="empty-fonts">暂无自定义字体。导入后可在上方选择使用。</p>
            <div v-for="font in customFonts" :key="font" class="font-library-row"><span>{{ fontLabel(font) }}<small class="font-storage-size">{{ formatFontSize(customFontSizes[font] ?? 0) }}</small></span><button type="button" :aria-label="`删除 ${fontLabel(font)}`" @click="deleteFont(font)">删除</button></div>
            <p v-if="importMessage" role="status">{{ importMessage }}</p>
          </div>
        </div>
        <aside class="font-preview">
          <div class="preview-head"><div><span class="theme-kicker">LIVE PREVIEW</span><h4>终端预览</h4></div><span>{{ fontsForm.terminalFontSize }} px</span></div>
          <div class="mini-terminal" :style="{background: themeForm.background, color: themeForm.foreground}">
            <div class="terminal-titlebar"><span class="window-dots">● ● ●</span><span>ding-ssh · 本机</span></div>
            <div class="terminal-content" :style="{fontFamily: monoFontStack(fontsForm.terminalFont), fontSize: `${fontsForm.terminalFontSize}px`}">
              <div><span :style="{color: themeForm.green}">❯</span> system info</div>
              <div class="system-banner" :style="{color: themeForm.blue}">ding-ssh / 系统信息</div>
              <div>系统　 {{ systemPlatform }}</div><div>处理器 {{ systemCores || '未知' }} 逻辑核心</div><div>语言　 {{ systemLanguage }}</div>
              <div class="terminal-divider">────────────────────────</div>
              <div :style="{color: themeForm.green}">✓ 字体预览已就绪 / Ready</div><div>中文测试：你好，世界。</div><div>English: Aa Bb 0O 1Il 0123456789</div><div :style="{color: themeForm.yellow}">~ / % → [终端混排效果]</div>
              <div class="terminal-prompt"><span :style="{color: themeForm.green}">❯</span> <span class="terminal-cursor"> </span></div>
            </div>
          </div>
          <p class="preview-caption">当前设备信息 · 字体、字号和主题实时更新</p>
        </aside>
      </div>
    </section>
    <Dialog v-model="showImportError" title="字体导入失败" :width="420"><p class="import-error-message">{{ importError }}</p><Button variant="primary" size="sm" @click="showImportError = false">知道了</Button></Dialog>
    <footer class="theme-footer"><p>更改会实时应用到整个应用。</p><Button variant="ghost" size="sm" @click="resetAppearance">恢复默认</Button></footer>
  </section>
</template>

<style scoped>
.preset-color-item{position:relative;display:inline-flex}.preset-color-name{position:absolute;z-index:10;bottom:calc(100% + 9px);left:50%;transform:translate(-50%,3px);padding:5px 8px;border:1px solid var(--border);border-radius:var(--radius-small);background:var(--surface-2);color:var(--foreground);box-shadow:var(--shadow-pop);white-space:nowrap;font-size:11px;line-height:1.4;pointer-events:none;opacity:0;visibility:hidden;transition:opacity 100ms,transform 100ms,visibility 0s 100ms}.preset-color-item:hover .preset-color-name,.preset-color-item:focus-within .preset-color-name{opacity:1;visibility:visible;transform:translate(-50%,0);transition-delay:100ms}

.color-spectrum{margin-top:18px;padding-top:16px;border-top:1px solid var(--border-subtle)}.spectrum-heading{display:flex;justify-content:space-between;gap:12px;font-size:12px}.spectrum-heading span{color:var(--muted-foreground);font-size:11px}.color-spectrum .hue-control,.spectrum-control{display:flex;align-items:center;gap:12px;margin-top:16px;font-size:12px;color:var(--foreground-secondary)}.color-spectrum label>span{width:64px;flex-shrink:0}.color-spectrum input{flex:1;min-width:0;appearance:none;height:12px;border:1px solid var(--border-subtle);border-radius:var(--radius-pill);cursor:pointer}.color-spectrum output{width:36px;text-align:right;font-size:11px;color:var(--muted-foreground);font-variant-numeric:tabular-nums}.color-spectrum .grayscale-range{background:linear-gradient(90deg,#000,#fff)}.color-spectrum input::-webkit-slider-thumb{appearance:none;width:18px;height:18px;background:var(--foreground);border:3px solid var(--surface-2);border-radius:50%;box-shadow:0 0 0 1px var(--border-strong)}.color-spectrum input::-moz-range-thumb{width:12px;height:12px;background:var(--foreground);border:3px solid var(--surface-2);border-radius:50%}.spectrum-help{margin:14px 0 0;font-size:11px;line-height:1.6;color:var(--muted-foreground)}

.font-storage-size{display:block;margin-top:4px;font-size:11px;color:var(--muted-foreground)}.import-error-message{font-size:13px;line-height:1.7;margin:12px 0 20px;overflow-wrap:anywhere}

.recommended-fonts{padding-bottom:20px;margin-bottom:20px;border-bottom:1px solid var(--border-subtle)}.recommendation-list{display:grid;gap:8px;margin-top:12px}.recommendation-list button{display:grid;gap:5px;text-align:left;padding:10px 12px;border:1px solid var(--border-subtle);border-radius:var(--radius-control);background:var(--hover)}.recommendation-list button.active{border-color:var(--primary);background:var(--primary-weak)}.recommendation-list strong{font-size:12px;font-weight:500}.recommendation-list small{font-size:11px;color:var(--muted-foreground)}

.section-heading{margin:26px 0 16px}.section-heading h4{font-size:18px;margin:7px 0}.section-heading p,.preview-caption{font-size:12px;color:var(--muted-foreground);margin:6px 0}.font-layout{display:grid;grid-template-columns:minmax(0,1.15fr) minmax(0,.85fr);gap:16px;align-items:start}.font-editor,.font-preview{padding:22px;border:1px solid var(--border-subtle);border-radius:var(--radius-card);background:var(--surface-raised-bg);min-width:0}.font-fields{display:grid;gap:20px}.font-select-field,.font-fields label{display:grid;gap:8px;font-size:12px;font-weight:500}.size-control input{padding:9px 10px;background:var(--field-bg);color:var(--foreground);border:1px solid var(--border);border-radius:var(--radius-control);font:inherit}.font-fields small,.library-heading small{font-size:11px;color:var(--muted-foreground);font-weight:400}.size-control{display:flex;align-items:center;gap:8px}.size-control input{width:80px}.font-status{font-size:11px;line-height:1.6;color:var(--muted-foreground);margin:18px 0}.font-library{border-top:1px solid var(--border-subtle);padding-top:18px}.library-heading{display:flex;justify-content:space-between;gap:12px;align-items:center}.library-heading h5{font-size:12px;margin:0 0 5px}.empty-fonts,.font-library p{font-size:11px;color:var(--muted-foreground);line-height:1.6}.font-library-row{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:12px 0;border-bottom:1px solid var(--border-subtle);font-size:12px}.font-library-row span{overflow-wrap:anywhere;min-width:0}.font-library-row button{color:var(--status-error);font-size:11px;flex-shrink:0}.mini-terminal{border:1px solid var(--border-subtle);border-radius:var(--radius-window);overflow:hidden;margin-top:18px}.terminal-titlebar{display:flex;gap:14px;align-items:center;padding:10px 12px;border-bottom:1px solid var(--border-subtle);font-size:10px;background:var(--surface-raised-bg)}.window-dots{color:var(--muted-foreground);letter-spacing:2px}.terminal-content{padding:18px 14px;line-height:1.8;overflow-x:auto;white-space:nowrap;min-height:295px}.system-banner{margin:12px 0 5px;font-weight:700}.terminal-divider{opacity:.35;margin:7px 0}.terminal-prompt{margin-top:12px}.terminal-cursor{display:inline-block;background:currentColor;width:7px;height:1em;vertical-align:middle;opacity:.65}.preview-caption{margin-top:12px}@media(max-width:900px){.font-layout{grid-template-columns:1fr}}

.terminal-preset-field{display:grid;gap:8px}.terminal-font-controls{margin:20px 0;padding:22px;background:var(--surface-bg);border:1px solid var(--border-subtle);border-radius:var(--radius-card)}.terminal-font-controls h4{margin:0 0 18px;font-size:14px}.terminal-font-controls label{display:flex;align-items:center;justify-content:space-between;margin-bottom:12px;font-size:12px}.terminal-font-controls select,.terminal-font-controls input{background:var(--field-bg);color:var(--foreground);border:1px solid var(--border);padding:6px;border-radius:var(--radius-control)}.terminal-font-controls input{width:70px}.terminal-font-controls p{margin:8px 0;overflow-wrap:anywhere}.theme-page{max-width:1040px;padding-bottom:28px}.theme-header{display:flex;align-items:flex-end;justify-content:space-between;gap:24px;margin-bottom:24px}.theme-kicker{font:600 10px/1 var(--font-mono);letter-spacing:.16em;color:var(--muted-foreground)}.theme-header h3{margin:8px 0 5px;font-size:24px;line-height:1.1;letter-spacing:-.035em}.theme-header p{margin:0;color:var(--foreground-secondary);font-size:13px}.save-state{display:flex;align-items:center;gap:7px;color:var(--muted-foreground);font-size:11px}.save-state>span{width:6px;height:6px;border-radius:50%;background:var(--success)}.save-state>span.saving{background:var(--warning);animation:pulse 1s infinite}
.theme-layout{display:grid;grid-template-columns:minmax(420px,1.15fr) minmax(300px,.85fr);gap:16px}.theme-editor,.theme-preview{border:1px solid var(--border-subtle);border-radius:var(--radius-card);background:var(--surface-raised-bg);box-shadow:none}.theme-editor{padding:22px}.tone-switch{display:flex;justify-content:center;gap:5px;margin-bottom:18px}.tone-switch button{width:38px;height:34px;display:grid;place-items:center;border-radius:var(--radius-control);color:var(--muted-foreground);transition:background var(--dur),color var(--dur),transform var(--dur)}.tone-switch button:hover{color:var(--foreground);background:var(--hover)}.tone-switch button.active{color:var(--primary-hover);background:var(--primary-weak);box-shadow:inset 0 0 0 1px var(--primary-border)}
.color-field{position:relative;height:300px;border-radius:var(--radius-card);overflow:hidden;cursor:crosshair;touch-action:none;box-shadow:inset 0 0 0 1px rgb(0 0 0 / 7%)}.field-white,.field-black{position:absolute;inset:0;pointer-events:none}.field-white{background:linear-gradient(90deg,#fff,transparent)}.field-black{background:linear-gradient(0deg,#000000,transparent)}.color-marker{position:absolute;width:32px;height:32px;border:4px solid white;border-radius:50%;box-shadow:0 2px 10px rgb(0 0 0 / 35%);transform:translate(-50%,-50%);pointer-events:none}.color-marker span{display:block;width:100%;height:100%;border-radius:50%}
.hue-control,.native-color{display:flex;align-items:center;gap:16px;margin-top:16px;font-size:12px;color:var(--foreground-secondary)}.hue-control input{flex:1;appearance:none;height:12px;border-radius:var(--radius-pill);background:linear-gradient(90deg,#f00,#ff0,#0f0,#0ff,#00f,#f0f,#f00)}.native-color input{width:54px;height:32px;padding:2px;border:1px solid var(--border);border-radius:var(--radius-control);background:transparent}.color-meta{display:flex;align-items:center;justify-content:space-between;gap:16px;margin:18px 0 22px}.preset-row{display:flex;gap:9px;flex-wrap:wrap}.preset-dot{width:27px;height:27px;border-radius:50%;box-shadow:inset 0 0 0 1px rgb(0 0 0 / 8%);transition:transform var(--dur),box-shadow var(--dur)}.preset-dot:hover{transform:scale(1.08)}.preset-dot.active{box-shadow:0 0 0 2px var(--surface-2),0 0 0 4px var(--foreground)}.hex-field{display:flex;align-items:center;gap:7px;flex-shrink:0}.hex-field>span{font:600 9px var(--font-mono);color:var(--muted-foreground)}.hex-field :deep(input){width:88px;height:30px;font:11px var(--font-mono);text-transform:uppercase}
.glass-help{margin:0;color:var(--muted-foreground);font-size:12px}.slider-list .glass-toggle input{appearance:auto;width:18px;height:18px}.slider-list{display:grid;gap:22px}.slider-list label>span{display:flex;justify-content:space-between;align-items:center;margin-bottom:9px}.slider-list strong{font-size:12.5px;font-weight:500}.slider-list output{font:12px var(--font-mono);color:var(--muted-foreground)}.slider-list input{width:100%;height:5px;appearance:none;border-radius:var(--radius-pill);background:var(--hover-strong);accent-color:var(--primary)}.slider-list input::-webkit-slider-thumb{appearance:none;width:16px;height:16px;border:3px solid var(--surface-2);border-radius:50%;background:var(--primary);box-shadow:0 0 0 1px var(--primary-border),0 2px 6px rgb(0 0 0 / 20%)}
.theme-preview{align-self:start;padding:22px}.preview-head{display:flex;justify-content:space-between;align-items:flex-start;margin-bottom:18px}.preview-head h4{margin:7px 0 0;font-size:14px}.preview-head>span{display:flex;align-items:center;gap:5px;padding:5px 8px;border-radius:var(--radius-pill);background:var(--hover);color:var(--muted-foreground);font-size:10px}.preview-window{height:270px;display:grid;grid-template-columns:34% 1fr;overflow:hidden;border:1px solid var(--border-subtle);border-radius:var(--radius-card);background:var(--workspace-gradient,var(--workspace-bg));box-shadow:0 10px 24px rgb(0 0 0 / 10%)}.preview-sidebar{padding:14px 10px;background:var(--sidebar-primary-gradient,var(--sidebar-primary-bg))}.preview-sidebar i{display:block;width:55%;height:7px;margin-bottom:17px;border-radius:var(--radius-control);background:var(--primary);opacity:.65}.preview-sidebar b{display:block;height:27px;margin-bottom:7px;border-radius:var(--radius-control);background:var(--hover-strong)}.preview-main{padding:12px;background:var(--workspace-gradient,var(--workspace-bg))}.preview-toolbar{height:28px;margin-bottom:10px;border-radius:var(--radius-control);background:var(--toolbar-bg)}.preview-card{height:70px;padding:12px;margin-bottom:10px;border-radius:var(--radius-card);background:var(--surface-bg)}.preview-card span{display:block;width:75%;height:6px;margin-bottom:8px;border-radius:var(--radius-small);background:var(--border-strong);opacity:.55}.preview-card span:nth-child(2){width:52%}.preview-card span:nth-child(3){width:63%}.preview-terminal{height:125px;padding:16px;border-radius:var(--radius-card);background:var(--terminal-bg,#11151b)}.preview-terminal i{display:block;width:60%;height:5px;margin-bottom:9px;border-radius:var(--radius-small);background:#8fb0f2;opacity:.7}.preview-terminal i:nth-child(2){width:78%;background:#7ad8a3}.preview-terminal i:nth-child(3){width:42%;background:#d4a04a}.preview-note{display:flex;gap:10px;margin-top:16px;padding:13px;border-radius:var(--radius-card);background:var(--hover);color:var(--primary-hover)}.preview-note span{color:var(--muted-foreground);font-size:10.5px;line-height:1.45}.preview-note strong{display:block;margin-bottom:2px;color:var(--foreground-secondary);font-size:11px}.theme-footer{display:flex;align-items:center;justify-content:space-between;margin-top:14px;padding:0 4px}.theme-footer p{margin:0;color:var(--muted-foreground);font-size:11px}
@keyframes pulse{50%{opacity:.35}}@media(max-width:900px){.theme-layout{grid-template-columns:1fr}.theme-preview{width:100%}}@media(max-width:600px){.theme-header{align-items:flex-start;flex-direction:column}.theme-layout{display:block}.theme-preview{margin-top:14px}.color-field{height:240px}.color-meta{align-items:flex-start;flex-direction:column}}

.material-block{display:grid;gap:10px}.material-label{display:flex;justify-content:space-between;gap:12px;align-items:baseline}.material-label small{font-size:11px;color:var(--text-muted);text-align:right}.material-modes{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:6px}.material-modes button{height:32px;padding:0 8px;border:1px solid var(--border-subtle);border-radius:var(--radius-control);background:var(--field-bg);color:var(--text-secondary);font-size:12px;transition:background var(--dur),border-color var(--dur),color var(--dur)}.material-modes button:hover{background:var(--interactive-hover-bg);color:var(--text-primary)}.material-modes button.active{background:var(--interactive-active-bg);border-color:var(--accent-border);color:var(--text-primary)}.material-fallback{color:var(--status-warning)}.slider-list input:disabled{opacity:.45;cursor:not-allowed}
</style>
