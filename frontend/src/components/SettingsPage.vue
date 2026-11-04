<script lang="ts" setup>
import {onMounted, ref} from 'vue'
import SettingsNavigation from './settings/SettingsNavigation.vue'
import HotkeysSection from './settings/HotkeysSection.vue'
import ConnectionInfoSection from './ConnectionInfoSection.vue'
import SettingsGeneralSection from './settings/SettingsGeneralSection.vue'
import SettingsThemeSection from './settings/SettingsThemeSection.vue'
import SettingsCredentialsSection from './settings/SettingsCredentialsSection.vue'
import SettingsSecuritySection from './settings/SettingsSecuritySection.vue'
import SettingsDebugSection from './settings/SettingsDebugSection.vue'
import SettingsLogsSection from './settings/SettingsLogsSection.vue'
import SettingsAuditSection from './settings/SettingsAuditSection.vue'
import SettingsMigrateSection from './settings/SettingsMigrateSection.vue'
import SettingsAboutSection from './settings/SettingsAboutSection.vue'
import {useSettingsStore} from '../stores/settings'

const settings = useSettingsStore()

// 一级菜单：通用 / 快捷键 / 连接说明 / 外观 / 凭证 / 安全 / 调试 / 日志 / AI 记录 / 导入导出 / 关于
const menuItems = [
  {key: 'general', label: '通用', icon: 'clock'},
  {key: 'hotkeys', label: '快捷键', icon: 'command'},
  {key: 'connectionInfo', label: '连接说明', icon: 'info'},
  {key: 'theme', label: '外观', icon: 'palette'},
  {key: 'credentials', label: '保存的凭证', icon: 'key'},
  {key: 'security', label: '安全', icon: 'lock'},
  {key: 'debug', label: '调试模式', icon: 'terminal'},
  {key: 'logs', label: '日志', icon: 'file'},
  {key: 'audit', label: 'AI 记录', icon: 'activity'},
  {key: 'migrate', label: '导入导出', icon: 'package'},
  {key: 'about', label: '关于', icon: 'info'},
] as const
type SettingsSection = 'general' | 'hotkeys' | 'connectionInfo' | 'theme' | 'credentials' | 'security' | 'debug' | 'logs' | 'audit' | 'migrate' | 'about'
const section = ref<SettingsSection>('general')

onMounted(() => {
  if (!settings.loaded) void settings.load()
})
</script>

<template>
  <div class="h-full flex min-h-0" data-ui="settings">
    <SettingsNavigation :items="menuItems" :model-value="section" @update:model-value="section = $event as SettingsSection" />

    <div class="flex-1 min-w-0 overflow-y-auto px-8 py-6" :data-ui="`settings-section-${section}`" :data-section="section">
      <SettingsGeneralSection v-if="section === 'general'" />
      <HotkeysSection v-else-if="section === 'hotkeys'" />
      <div v-else-if="section === 'connectionInfo'" class="max-w-3xl fade-rise">
        <ConnectionInfoSection />
      </div>
      <SettingsThemeSection v-else-if="section === 'theme'" />
      <SettingsCredentialsSection v-else-if="section === 'credentials'" />
      <SettingsSecuritySection v-else-if="section === 'security'" />
      <SettingsDebugSection v-else-if="section === 'debug'" />
      <SettingsLogsSection v-else-if="section === 'logs'" />
      <SettingsAuditSection v-else-if="section === 'audit'" />
      <SettingsMigrateSection v-else-if="section === 'migrate'" />
      <SettingsAboutSection v-else />
    </div>
  </div>
</template>
