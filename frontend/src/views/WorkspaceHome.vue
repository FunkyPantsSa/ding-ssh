<script setup lang="ts">
import {t as $t} from '../i18n'

import {t} from '../i18n'

import {computed, onMounted} from 'vue'
import {serverUsage} from '@/services/recent'
import {useUIStore} from '@/stores/ui'
import Icon from '@/components/Icon.vue'
import {Button} from '@/components/ui'
import {useServersStore} from '@/stores/servers'
import {useSessionsStore} from '@/stores/sessions'

const emit = defineEmits<{
  quickConnect: []
  manageServers: []
}>()

const ui = useUIStore()
const servers = useServersStore()
const sessions = useSessionsStore()

const knownServers = computed(() => new Map(servers.servers.map((server) => [server.id, server])))
const usage = computed(() => serverUsage.value.flatMap((entry) => {
  const server = knownServers.value.get(entry.id)
  return server ? [{...entry, server}] : []
}))
const featuredServers = computed(() => usage.value
  .filter((entry) => entry.count > 0)
  .sort((a, b) => b.count - a.count || b.connectedAt - a.connectedAt)
  .slice(0, 4))
const recentServers = computed(() => [...usage.value]
  .sort((a, b) => b.connectedAt - a.connectedAt)
  .slice(0, 4))
const groupCount = computed(() => new Set(servers.servers.map((server) => server.group).filter(Boolean)).size)
const onlineTabs = computed(() => sessions.tabs.filter(t => t.status === 'connected'))
const shortcuts = [
  {get label() { return t("命令面板") }, get description() { return t("搜索并执行应用命令") }, icon: 'command', open: () => ui.openCommandPalette()},
  {get label() { return t("新建服务器") }, get description() { return t("保存新的 SSH 连接") }, icon: 'plus', open: () => ui.requestNewServer()},
  {get label() { return t("SSH 隧道") }, get description() { return t("管理端口转发") }, icon: 'terminal', open: () => ui.showTunnel()},
  {get label() { return t("运行状态") }, get description() { return t("查看资源占用和渲染方式") }, icon: 'activity', open: () => ui.showSettings('about')},
  {get label() { return t("应用日志") }, get description() { return t("查看连接和诊断日志") }, icon: 'file', open: () => ui.showSettings('logs')},
  {get label() { return t("导入导出") }, get description() { return t("迁移服务器与配置") }, icon: 'package', open: () => ui.showSettings('migrate')},
]

onMounted(() => {
  if (!servers.loadedOnce && !servers.loading) void servers.load().catch(() => {})
})

function connect(server: (typeof servers.servers)[number]) {
  ui.showWorkspace()
  sessions.openTab(server)
}
</script>

<template>
  <section class="workspace-home" :aria-label="$t('工作区首页')">
    <div class="home-content">
      <header class="home-hero">
        <div>
          <span class="home-eyebrow"><span class="home-live-dot"></span> {{ $t('SSH 工作区') }}</span>
          <h2>{{ $t('准备连接到哪里？') }}</h2>
          <p>{{ $t('在一个安静、专注的工作区里管理服务器、终端与文件传输。') }}</p>
        </div>
        <div class="home-hero-actions">
          <Button variant="primary" @click="emit('quickConnect')">
            <Icon name="zap" :size="14" />{{ $t('快速连接') }}</Button>
          <Button variant="ghost" @click="ui.showWorkspace(); sessions.openLocalTab()">
            <Icon name="monitor" :size="14" />{{ $t('本地终端') }}</Button>
        </div>
      </header>

      <div class="home-grid">
        <div class="home-primary">
        <section class="home-panel home-servers">
          <div class="home-panel-head">
            <div>
              <span class="home-kicker">{{ $t('服务器') }}</span>
              <h3>{{ $t('常用服务器') }}</h3>
              <p class="home-panel-hint">{{ $t('按成功登录次数排序') }}</p>
            </div>
            <button class="home-link" type="button" @click="emit('manageServers')">{{ $t('管理全部') }}<Icon name="arrow-right" :size="13" />
            </button>
          </div>

          <div v-if="servers.loading && !servers.servers.length" class="home-server-list" :aria-label="$t('正在加载服务器')">
            <div v-for="i in 3" :key="i" class="home-server-row skeleton-row"></div>
          </div>
          <div v-else-if="featuredServers.length" class="home-server-list">
            <button
              v-for="{server, count} in featuredServers"
              :key="server.id"
              type="button"
              class="home-server-row"
              @click="connect(server)"
            >
              <span class="home-server-icon"><Icon name="server" :size="15" /></span>
              <span class="home-server-copy">
                <strong>{{ server.name }}</strong>
                <small>{{ server.user }}@{{ server.host }}:{{ server.port }}</small>
              </span>
              <span class="home-server-group">{{ count }} {{ $t('次') }}</span>
              <span v-if="server.group" class="home-server-group">{{ server.group }}</span>
              <Icon name="chevron-right" :size="14" extra-class="home-row-arrow" />
            </button>
          </div>
          <div v-else class="home-empty">
            <span class="home-empty-icon"><Icon name="plus" :size="18" /></span>
            <div v-if="servers.servers.length"><strong>{{ $t('还没有常用服务器') }}</strong><p>{{ $t('成功连接已保存的服务器后，这里会按登录次数更新。') }}</p></div>
            <div v-else><strong>{{ $t('添加第一台服务器') }}</strong><p>{{ $t('保存连接信息，下次一键进入终端。') }}</p></div>
            <Button v-if="servers.servers.length" variant="ghost" size="sm" @click="emit('quickConnect')">{{ $t('快速连接') }}</Button>
            <Button v-else variant="ghost" size="sm" @click="ui.requestNewServer()">{{ $t('新建服务器') }}</Button>
          </div>
        </section>

      <section class="home-panel home-tools" aria-labelledby="home-tools-title">
        <h3 id="home-tools-title">{{ $t('快捷入口') }}</h3>
        <div class="home-tool-grid">
          <button v-for="shortcut in shortcuts" :key="shortcut.label" type="button" class="home-server-row" @click="shortcut.open()">
            <span class="home-server-icon"><Icon :name="shortcut.icon" :size="16" /></span>
            <span class="home-server-copy"><strong>{{ shortcut.label }}</strong><small>{{ shortcut.description }}</small></span>
            <Icon name="arrow-right" :size="14" extra-class="home-row-arrow" />
          </button>
        </div>
      </section>

        </div>
        <aside class="home-side">
          <section class="home-panel home-status">
            <span class="home-kicker">{{ $t('概览') }}</span>
            <h3>{{ $t('首页概览') }}</h3>
            <div class="home-stats">
              <div><strong>{{ servers.servers.length }}</strong><span>{{ $t('服务器') }}</span></div>
              <div><strong>{{ groupCount }}</strong><span>{{ $t('分组') }}</span></div>
              <div><strong>{{ sessions.tabs.length }}</strong><span>{{ $t('会话') }}</span></div>
            </div>
          </section>

          <div class="home-panel home-activity">
          <section class="home-status">
            <h3>{{ $t('在线会话 ·') }} {{ onlineTabs.length }}</h3>
            <button v-for="tab in onlineTabs" :key="tab.clientId" class="home-server-row" @click="ui.showWorkspace(); sessions.activateTab(tab.clientId)"><span class="home-session-name" :title="tab.node?.name || t('本机终端')">{{ tab.node?.name || t('本机终端') }}</span> <Icon name="arrow-right" :size="14" /></button>
            <p v-if="!onlineTabs.length">{{ $t('暂无在线会话') }}</p>
          </section>
          <section class="home-status">
            <h3>{{ $t('最近连接') }}</h3>
            <button v-for="{server} in recentServers" :key="server.id" class="home-server-row" @click="connect(server)"><span class="home-session-name" :title="server.name">{{ server.name }}</span> <Icon name="arrow-right" :size="14" /></button>
            <p v-if="!recentServers.length">{{ $t('连接服务器后将在这里显示') }}</p>
          </section>
          </div>
          <button type="button" class="home-panel home-theme" @click="ui.showSettings('theme')">
            <span class="home-theme-icon"><Icon name="palette" :size="17" /></span>
            <span><strong>{{ $t('让工作区更像你') }}</strong><small>{{ $t('调整颜色、明暗与终端主题') }}</small></span>
            <Icon name="arrow-right" :size="14" extra-class="home-row-arrow" />
          </button>
        </aside>
      </div>


      <footer class="home-shortcuts" :aria-label="$t('快捷操作')">
        <div class="home-shortcuts-dock">
          <button type="button" class="home-shortcut-pill" :title="$t('打开命令面板 (⌘K)')" @click="ui.openCommandPalette()">
            <kbd>⌘ K</kbd>
            <span>{{ $t('命令面板') }}</span>
          </button>
          <span class="home-shortcut-sep"></span>
          <button type="button" class="home-shortcut-pill" :title="$t('打开本地终端 (⌘T)')" @click="ui.showWorkspace(); sessions.openLocalTab()">
            <kbd>⌘ T</kbd>
            <span>{{ $t('本地终端') }}</span>
          </button>
          <span class="home-shortcut-sep"></span>
          <button type="button" class="home-shortcut-pill" :title="$t('偏好设置 (⌘,)')" @click="ui.showSettings('general')">
            <kbd>⌘ ,</kbd>
            <span>{{ $t('偏好设置') }}</span>
          </button>
        </div>
      </footer>
    </div>
  </section>
</template>

<style scoped>
.workspace-home{display:flex;flex-direction:column;position:relative;flex:1;min-width:0;min-height:0;overflow:auto;container-type:size;color:var(--foreground)}
.home-content{box-sizing:border-box;flex:0 0 auto;width:min(1080px,calc(100% - 40px));margin:auto;padding:32px 0}
.home-hero{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:16px;margin-bottom:20px}
.home-eyebrow,.home-kicker{display:flex;align-items:center;gap:7px;color:var(--muted-foreground);font:500 10px/1.3 var(--font-mono);letter-spacing:.1em}
.home-live-dot{width:6px;height:6px;border-radius:50%;background:var(--success)}
.home-hero h2{margin:9px 0 6px;font-size:clamp(23px,2.8cqw,32px);line-height:1.2;letter-spacing:-.03em;font-weight:600}
.home-hero p{margin:0;font-size:12px;line-height:1.5;color:var(--muted-foreground)}
.home-hero-actions{display:flex;flex-wrap:wrap;gap:6px}
.home-grid{display:grid;grid-template-columns:minmax(0,1fr) 280px;align-items:start;gap:16px}
.home-primary,.home-side{display:grid;align-content:start;gap:14px;min-width:0}
.home-panel{min-width:0;border:var(--glass-border);border-radius:var(--radius-card, 14px);background:var(--surface-bg);backdrop-filter:blur(var(--glass-shell-blur, 16px));-webkit-backdrop-filter:blur(var(--glass-shell-blur, 16px));box-shadow:0 8px 24px -4px rgba(0, 0, 0, 0.25), inset 0 1px 0 rgba(255, 255, 255, 0.08)}
.home-servers,.home-tools,.home-status{padding:16px}
.home-panel-head{display:flex;justify-content:space-between;gap:12px;align-items:start;margin-bottom:10px}
.home-panel h3{margin:4px 0 10px;font-size:13px;font-weight:600}
.home-panel-hint{margin:4px 0 0;font-size:11px;color:var(--muted-foreground)}
.home-link{display:flex;align-items:center;gap:5px;white-space:nowrap;font-size:12px;color:var(--muted-foreground);transition:color var(--dur)}
.home-link:hover{color:var(--foreground)}
.home-server-list{display:grid;gap:4px}
.home-server-row{display:flex;align-items:center;gap:10px;width:100%;min-width:0;min-height:44px;padding:8px 10px;text-align:left;border-radius:var(--radius-control, 10px);border:1px solid transparent;transition:all var(--dur) var(--ease)}
.home-server-row:hover{background:var(--interactive-hover-bg);border-color:var(--accent-border);transform:translateY(-0.5px)}
.home-server-icon,.home-theme-icon,.home-empty-icon{display:grid;place-items:center;flex-shrink:0;width:32px;height:32px;border-radius:var(--radius-control, 10px);background:var(--arc-active-tab-gradient, var(--primary-weak));color:var(--primary-hover);border:1px solid var(--arc-active-tab-border, var(--primary-border))}
.home-server-copy{flex:1;min-width:0}
.home-server-copy strong{display:block;font-size:12.5px;font-weight:500;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.home-server-copy small{display:block;margin-top:2px;font-size:11px;line-height:1.4;color:var(--muted-foreground);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}
.home-server-group{flex-shrink:0;max-width:70px;padding:2px 6px;font-size:10px;border-radius:var(--radius-pill);background:var(--interactive-hover-bg, rgba(255, 255, 255, 0.08));color:var(--muted-foreground);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}
.home-row-arrow,.home-status .home-server-row>svg{flex-shrink:0;color:var(--muted-foreground)}
.home-session-name{flex:1;min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12px}
.home-stats{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:8px;margin-top:12px}
.home-stats div{text-align:center;padding:10px 4px;border-radius:var(--radius-control, 10px);background:var(--interactive-hover-bg, rgba(255, 255, 255, 0.04));border:var(--glass-border-subtle)}
.home-stats strong{display:block;font:500 19px/1.2 var(--font-mono)}
.home-stats span{display:block;margin-top:4px;font-size:10px;color:var(--muted-foreground)}
.home-activity .home-status+.home-status{border-top:var(--glass-border-subtle)}
.home-status .home-server-row{min-height:34px;padding:5px 0}
.home-status p{margin:6px 0 0;font-size:12px;color:var(--muted-foreground)}
.home-theme{display:grid;grid-template-columns:auto minmax(0,1fr) auto;align-items:center;gap:10px;padding:14px;text-align:left;border:var(--glass-border);border-radius:var(--radius-card, 14px);background:var(--surface-bg);box-shadow:0 8px 24px -4px rgba(0, 0, 0, 0.25), inset 0 1px 0 rgba(255, 255, 255, 0.08);transition:all var(--dur) var(--ease)}
.home-theme:hover{background:var(--interactive-hover-bg);border-color:var(--accent-border);transform:translateY(-1px)}
.home-theme strong,.home-theme small{display:block;line-height:1.4}
.home-theme strong{font-size:12px;font-weight:500}
.home-theme small{margin-top:2px;font-size:11px;color:var(--muted-foreground)}
.home-tools h3{margin:0 0 8px}
.home-tool-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:6px}
.home-tools .home-server-row{min-height:52px}
.home-empty{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:8px;min-height:140px;text-align:center;color:var(--muted-foreground)}
.home-empty strong{font-size:13px;color:var(--foreground)}
.home-empty p{margin:4px 0;font-size:12px}
.home-shortcuts{display:flex;justify-content:center;padding-top:24px;padding-bottom:6px}
.home-shortcuts-dock{display:inline-flex;align-items:center;gap:3px;padding:4px 6px;border:var(--glass-border);border-radius:9999px;background:var(--surface-bg, rgba(255, 255, 255, 0.65));backdrop-filter:blur(var(--glass-shell-blur, 16px));-webkit-backdrop-filter:blur(var(--glass-shell-blur, 16px));box-shadow:0 4px 16px -2px rgba(0, 0, 0, 0.06), 0 1px 3px rgba(0, 0, 0, 0.04)}
.home-shortcut-pill{display:inline-flex;align-items:center;gap:6px;padding:4px 11px;border-radius:9999px;border:1px solid transparent;background:transparent;color:var(--text-secondary, var(--foreground));font-size:11.5px;font-weight:500;cursor:pointer;transition:all var(--dur-fast) var(--ease)}
.home-shortcut-pill:hover{background:var(--interactive-hover-bg);color:var(--foreground);transform:translateY(-1px)}
.home-shortcut-pill:active{transform:scale(0.97)}
.home-shortcut-pill kbd{display:inline-flex;align-items:center;justify-content:center;padding:2px 5px;min-width:18px;height:18px;border:var(--glass-border-subtle);border-radius:4px;background:var(--interactive-active-bg, rgba(255, 255, 255, 0.08));color:var(--text-muted);font:500 10px/1 var(--font-mono);box-shadow:0 1px 2px rgba(0, 0, 0, 0.04);transition:all var(--dur-fast) var(--ease)}
.home-shortcut-pill:hover kbd{color:var(--foreground);border-color:var(--accent-border);background:var(--interactive-hover-bg)}
.home-shortcut-sep{width:1px;height:12px;background:var(--border-subtle, rgba(0, 0, 0, 0.08))}
.skeleton-row{background:var(--hover)}
@container(max-width:720px){
 .home-content{width:calc(100% - 28px);padding-top:16px}
 .home-grid{grid-template-columns:minmax(0,1fr);gap:12px}
 .home-side{grid-row:1}
 .home-primary{grid-row:2}
 .home-side{grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}
 .home-theme{grid-column:1/-1}
 .home-shortcuts{display:none}
 .home-hero{margin-bottom:14px}
}
@container(max-width:460px){
 .home-side{grid-template-columns:minmax(0,1fr)}
 .home-tool-grid{grid-template-columns:minmax(0,1fr)}
 .home-server-group{display:none}
}
@container(max-height:600px){
 .home-content{padding-top:12px;padding-bottom:12px}
 .home-hero{margin-bottom:12px;gap:10px}
 .home-hero h2{font-size:24px;margin-top:6px}
 .home-servers,.home-tools,.home-status{padding:12px}
 .home-primary,.home-side{gap:10px}
 .home-server-row,.home-tools .home-server-row{min-height:40px;padding-top:5px;padding-bottom:5px}
 .home-server-icon{width:26px;height:26px}
 .home-shortcuts{padding-top:10px}
}
</style>
