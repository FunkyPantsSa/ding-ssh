import {computed, reactive, watch, type Ref} from 'vue'
import {sshService} from '../services/ssh'
import {defaultAppearance, defaultFonts, defaultTheme, useSettingsStore} from '../stores/settings'
import {defaultPreset, paletteToTheme, presetById, PRESETS} from '../theme/presets'
import {normalizeSeed} from '../theme/engine'
import {systemTone} from './useSystemTone'
import {TERMINAL_FONT_SCHEMES} from '../theme/terminal-fonts'
import type {Fonts, MaterialMode, Theme, UIAppearance} from '../types'
import {clamp} from '../theme/material'

export const UI_FONT_OPTIONS = ['Geist Sans', 'system'] as const
export const TERMINAL_FONT_OPTIONS = [...TERMINAL_FONT_SCHEMES.map(scheme => scheme.value), 'Geist Mono', 'JetBrains Mono', 'Fira Code', 'system'] as const
export const TONE_OPTIONS = [{key: 'auto', label: '跟随系统'}, {key: 'light', label: '浅色'}, {key: 'dark', label: '深色'}] as const
export const MODE_OPTIONS = [{key: 'preset', label: '预设主题'}, {key: 'custom', label: '自定义'}] as const
export const MATERIAL_OPTIONS = [
  {key: 'solid', label: '实色', desc: '不透明动态配色，最稳定'},
  {key: 'soft', label: '柔和毛玻璃', desc: '保留主题染色与区域层级'},
  {key: 'clear', label: '通透毛玻璃', desc: '更通透，文字仍由实色层保护'},
] as const
type AnsiColorKey = 'black' | 'red' | 'green' | 'yellow' | 'blue' | 'magenta' | 'cyan' | 'white' | 'brightBlack' | 'brightRed' | 'brightGreen' | 'brightYellow' | 'brightBlue' | 'brightMagenta' | 'brightCyan' | 'brightWhite'
export const ANSI_FIELDS: {key: AnsiColorKey; label: string}[] = [
  {key: 'black', label: 'Black'}, {key: 'red', label: 'Red'}, {key: 'green', label: 'Green'}, {key: 'yellow', label: 'Yellow'},
  {key: 'blue', label: 'Blue'}, {key: 'magenta', label: 'Magenta'}, {key: 'cyan', label: 'Cyan'}, {key: 'white', label: 'White'},
  {key: 'brightBlack', label: '亮黑'}, {key: 'brightRed', label: '亮红'}, {key: 'brightGreen', label: '亮绿'}, {key: 'brightYellow', label: '亮黄'},
  {key: 'brightBlue', label: '亮蓝'}, {key: 'brightMagenta', label: '亮紫'}, {key: 'brightCyan', label: '亮青'}, {key: 'brightWhite', label: '亮白'},
]

/** 外观设置的表单状态与持久化边界；页面只负责组合各 section。 */
export function useSettingsAppearance(settings: ReturnType<typeof useSettingsStore>, saving: Ref<boolean>) {
  const themeForm = reactive<Theme>(defaultTheme())
  const appearanceForm = reactive<UIAppearance>(defaultAppearance())
  const fontsForm = reactive<Fonts>(defaultFonts())
  let syncing = false
  const currentTone = computed(() => appearanceForm.baseTone === 'auto' ? systemTone.value : appearanceForm.baseTone)
  const activePreset = computed(() => presetById(appearanceForm.presetId) ?? defaultPreset())

  function syncForms() {
    syncing = true
    Object.assign(themeForm, settings.theme)
    Object.assign(appearanceForm, settings.appearance)
    Object.assign(fontsForm, settings.fonts)
    syncing = false
  }
  async function persistLook() {
    saving.value = true
    try { await settings.applyLook({...appearanceForm}, {...themeForm}, {...fontsForm}) } finally { saving.value = false }
  }
  watch([themeForm, appearanceForm, fontsForm], () => { if (!syncing) void persistLook() }, {deep: true})
  async function selectPreset(id: string) {
    syncing = true; appearanceForm.mode = 'preset'; appearanceForm.presetId = id
    const preset = presetById(id) ?? defaultPreset()
    appearanceForm.primary = preset.primary
    appearanceForm.secondary = preset.secondary
    syncing = false; await persistLook()
  }
  async function setTone(tone: UIAppearance['baseTone']) {
    syncing = true; appearanceForm.baseTone = tone
    syncing = false; await persistLook()
  }
  async function setSeedColor(color: string) {
    syncing = true
    const seed = normalizeSeed(color)
    appearanceForm.mode = 'custom'
    appearanceForm.primary = seed
    appearanceForm.secondary = seed
    syncing = false
    await persistLook()
  }
  async function setMode(mode: UIAppearance['mode']) {
    syncing = true; appearanceForm.mode = mode
    syncing = false; await persistLook()
  }
  async function setMaterialMode(mode: MaterialMode) {
    syncing = true
    appearanceForm.materialMode = mode
    // Keep the legacy boolean in sync for older components / scripts.
    appearanceForm.glassEnabled = mode !== 'solid'
    syncing = false
    await persistLook()
  }
  async function setAtmosphere(value: number) {
    syncing = true
    const next = Math.round(clamp(value, 0, 100))
    appearanceForm.atmosphere = next
    appearanceForm.colorIntensity = next
    syncing = false
    await persistLook()
  }
  async function setSeparation(value: number) {
    syncing = true
    appearanceForm.surfaceSeparation = Math.round(clamp(value, 0, 100))
    syncing = false
    await persistLook()
  }
  async function resetTheme() { syncing = true; Object.assign(themeForm, defaultTheme()); syncing = false; await persistLook() }
  async function resetAppearance() {
    syncing = true; Object.assign(appearanceForm, defaultAppearance()); Object.assign(fontsForm, defaultFonts())
    syncing = false; await persistLook()
  }
  async function pickBgImage() { const path = await sshService.selectImageFile(); if (path) themeForm.bgImage = path }
  return {themeForm, appearanceForm, fontsForm, currentTone, activePreset, syncForms, selectPreset, setTone, setSeedColor, setMode, setMaterialMode, setAtmosphere, setSeparation, resetTheme, resetAppearance, pickBgImage, PRESETS}
}
