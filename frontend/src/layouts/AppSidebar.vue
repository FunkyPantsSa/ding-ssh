<script setup lang="ts">
import {computed, onBeforeUnmount, ref, watch} from 'vue'
import Icon from '@/components/Icon.vue'
import TabBar from '@/components/TabBar.vue'
import {Button} from '@/components/ui'
import {useServersStore} from '@/stores/servers'
import {useSessionsStore} from '@/stores/sessions'
import {useSettingsStore} from '@/stores/settings'
import {useUIStore} from '@/stores/ui'
import type {NavSectionKey} from '@/types'
import appLogo from '@/assets/images/ding-logo.png'

const props = defineProps<{collapsed: boolean; onlineCount: number}>()
const emit = defineEmits<{'update:collapsed': [value: boolean]}>()

const sessions = useSessionsStore()
const servers = useServersStore()
const settings = useSettingsStore()
const ui = useUIStore()

const sideTabs = computed(() => settings.tabBarPlacement === 'side' && sessions.tabs.length > 0)
// 左侧导航区段（导航 / 会话 / 标签页）按用户配置的顺序渲染；「标签页」仅在侧栏模式下存在
const navSections = computed<NavSectionKey[]>(() => {
  const order = settings.navSectionOrder.split(',').filter(Boolean) as NavSectionKey[]
  return order.filter((key) => key !== 'tabs' || sideTabs.value)
})

const preview = ref(false)
const sidebar = ref<HTMLElement | null>(null)
let openTimer: ReturnType<typeof setTimeout> | undefined
let closeTimer: ReturnType<typeof setTimeout> | undefined
let dragging = false
let pointerInside = false
let keyboardFocus = false
const visuallyCollapsed = computed(() => props.collapsed && !preview.value)
function cancelOpen() { clearTimeout(openTimer) }
function clearTimers() { clearTimeout(openTimer); clearTimeout(closeTimer) }
function scheduleOpen() {
  clearTimers()
  if (props.collapsed && !preview.value) openTimer = setTimeout(() => { preview.value = true }, 500)
}
function enterSidebar() { pointerInside = true; clearTimeout(closeTimer) }
function scheduleClose() {
  clearTimeout(openTimer); clearTimeout(closeTimer)
  if (!preview.value) return
  closeTimer = setTimeout(() => {
    if (pointerInside) return
    if (dragging || (keyboardFocus && sidebar.value?.contains(document.activeElement)) || document.querySelector('.menu-pop, [role="menu"], .ui-dialog-root')) { scheduleClose(); return }
    preview.value = false
  }, 250)
}
function leaveSidebar() { pointerInside = false; scheduleClose() }
function finishDrag() { dragging = false; if (!pointerInside) scheduleClose() }
function focusTrigger(event: FocusEvent) { keyboardFocus = (event.target as HTMLElement).matches(':focus-visible'); scheduleOpen() }
function leaveFocus(event: FocusEvent) {
  if (!sidebar.value?.contains(event.relatedTarget as Node | null) && !pointerInside) scheduleClose()
}
watch(() => props.collapsed, () => { clearTimers(); preview.value = false })
onBeforeUnmount(clearTimers)

function toggleCollapsed() {
  emit('update:collapsed', !props.collapsed)
}

function onBrandClick() {
  if (props.collapsed) emit('update:collapsed', false)
  else ui.showHome()
}
</script>

<template>
  <aside ref="sidebar" class="nav" :class="{'nav-collapsed': visuallyCollapsed, 'nav-preview': collapsed && preview}" @pointerdown="keyboardFocus = false" @keydown.tab="keyboardFocus = true" @pointerenter="enterSidebar" @pointerleave="leaveSidebar" @focusout="leaveFocus" @dragstart="dragging = true" @dragend="finishDrag" @drop="finishDrag" @keydown.esc="preview = false" aria-label="主导航" data-ui="sidebar">
    <div class="nav-brand">
      <button
        class="nav-mark"
        @pointerenter="scheduleOpen"
        @pointerleave="cancelOpen"
        @focus="focusTrigger"
        :aria-expanded="!visuallyCollapsed"
        :title="collapsed ? '展开导航' : '返回工作区'"
        :aria-label="collapsed ? '展开导航' : '返回工作区'"
        @click="onBrandClick"
      >
        <img :src="appLogo" alt="" class="brand-logo nav-logo" />
      </button>
      <span class="nav-word">Ding<span>-ssh</span></span>
      <Button
        v-if="!visuallyCollapsed"
        variant="ghost"
        size="icon"
        class="ml-auto"
        :title="collapsed ? '固定展开导航' : '收起导航'"
        :aria-label="collapsed ? '固定展开导航' : '收起导航'"
        @click="toggleCollapsed"
      >
        <Icon name="chevrons-left" :size="14" />
      </Button>
    </div>

    <!-- 三个区段按「设置 → 左侧导航区段顺序」排列：导航 / 会话 / 标签页 -->
    <nav class="nav-body">
      <section v-for="sec in navSections" :key="sec" class="nav-sec">
        <template v-if="sec === 'nav'">
          <div class="nav-label">导航</div>
          <button
            class="nav-item"
            data-ui="nav-item-workspace"
            :class="ui.view === 'home' ? 'active' : ''"
            :aria-current="ui.view === 'home' ? 'page' : undefined"
            title="首页"
            @click="ui.showHome()"
          >
            <Icon name="terminal" :size="15" extra-class="nav-ico" />
            <span class="nav-text">首页</span>
            <span v-if="onlineCount" class="nav-count">{{ onlineCount }}</span>
          </button>
          <button
            class="nav-item"
            data-ui="nav-item-local"
            title="本地终端"
            @click="ui.showWorkspace(); sessions.openLocalTab()"
          >
            <Icon name="monitor" :size="15" extra-class="nav-ico" />
            <span class="nav-text">本地终端</span>
          </button>
          <button
            class="nav-item"
            data-ui="nav-item-servers"
            :class="ui.view === 'servers' ? 'active' : ''"
            :aria-current="ui.view === 'servers' ? 'page' : undefined"
            title="服务器管理"
            @click="ui.showServers()"
          >
            <Icon name="server" :size="15" extra-class="nav-ico" />
            <span class="nav-text">服务器</span>
            <span v-if="servers.servers.length" class="nav-count">{{ servers.servers.length }}</span>
          </button>
          <button
            class="nav-item"
            data-ui="nav-item-tunnel"
            :class="ui.view === 'tunnel' ? 'active' : ''"
            :aria-current="ui.view === 'tunnel' ? 'page' : undefined"
            title="SSH 隧道"
            @click="ui.showTunnel()"
          >
            <Icon name="tunnel" :size="15" extra-class="nav-ico" />
            <span class="nav-text">隧道</span>
          </button>
          <button
            class="nav-item"
            data-ui="nav-item-settings"
            :class="ui.view === 'settings' ? 'active' : ''"
            :aria-current="ui.view === 'settings' ? 'page' : undefined"
            title="设置"
            @click="ui.showSettings()"
          >
            <Icon name="settings" :size="15" extra-class="nav-ico" />
            <span class="nav-text">设置</span>
          </button>
        </template>

        <template v-else-if="sec === 'sessions'">
          <div class="nav-label">会话</div>
          <button
            class="nav-item"
            data-ui="nav-item-quick-connect"
            title="快速连接"
            :class="ui.terminalSidebarOpen && ui.view === 'workspace' ? 'active' : ''"
            :aria-expanded="ui.terminalSidebarOpen && ui.view === 'workspace'"
            @click="ui.toggleQuickConnect()"
          >
            <Icon name="panel-left" :size="15" extra-class="nav-ico" />
            <span class="nav-text">快速连接</span>
          </button>
        </template>

        <!-- 标签页并入左侧导航：纵向列表，与顶部标签栏互斥 -->
        <template v-else>
          <TabBar side />
        </template>
      </section>
    </nav>

    <div class="nav-foot">
      <button class="nav-item" data-ui="nav-item-command" title="命令面板 ⌘K" @click="ui.openCommandPalette()">
        <Icon name="command" :size="15" extra-class="nav-ico" />
        <span class="nav-text">命令面板</span>
        <span class="kbd nav-kbd">⌘K</span>
      </button>
    </div>
  </aside>
</template>

<style scoped>
.nav-preview{position:absolute;inset:0 auto 0 0;width:var(--nav-expanded-w,216px);max-width:calc(100% - 48px);z-index:60;box-shadow:var(--shadow-pop);animation:sidebar-preview-in 160ms var(--ease-out)}
@keyframes sidebar-preview-in{from{opacity:.7;transform:translateX(-6px)}to{opacity:1;transform:translateX(0)}}
@media(prefers-reduced-motion:reduce){.nav-preview{animation:none}}
</style>
