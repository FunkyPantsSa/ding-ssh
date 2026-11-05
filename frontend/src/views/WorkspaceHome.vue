<script setup lang="ts">
import {computed, onMounted, ref, watch} from 'vue'
import {useUIStore} from '@/stores/ui'
import Icon from '@/components/Icon.vue'
import {Button} from '@/components/ui'
import {useServersStore} from '@/stores/servers'
import {useSessionsStore} from '@/stores/sessions'

const emit = defineEmits<{
  quickConnect: []
  manageServers: []
  openSettings: []
}>()

const ui = useUIStore()
const servers = useServersStore()
const sessions = useSessionsStore()

const featuredServers = computed(() => servers.servers.slice(0, 4))
const groupCount = computed(() => new Set(servers.servers.map((server) => server.group).filter(Boolean)).size)

function readRecent(): string[] {
  try { const ids = JSON.parse(localStorage.getItem('ding-ssh:recent-servers') || '[]'); return Array.isArray(ids) ? ids.filter(id => typeof id === 'string') : [] } catch { return [] }
}
const recentIds = ref<string[]>(readRecent())
const recentServers = computed(() => recentIds.value.map(id => servers.servers.find(s => s.id === id)).filter(Boolean).slice(0, 4))
const onlineTabs = computed(() => sessions.tabs.filter(t => t.status === 'connected'))
watch(() => sessions.tabs.filter(t => t.status === 'connected' && t.node?.id).map(t => t.node!.id), ids => {
  recentIds.value = [...new Set([...ids, ...recentIds.value])].slice(0, 8)
  try { localStorage.setItem('ding-ssh:recent-servers', JSON.stringify(recentIds.value)) } catch { /* storage is optional */ }
}, {immediate: true})

onMounted(() => {
  if (!servers.loadedOnce && !servers.loading) void servers.load().catch(() => {})
})

function connect(server: (typeof servers.servers)[number]) {
  ui.showWorkspace()
  sessions.openTab(server)
}
</script>

<template>
  <section class="workspace-home" aria-label="工作区首页">
    <div class="home-orb home-orb-a" aria-hidden="true"></div>
    <div class="home-orb home-orb-b" aria-hidden="true"></div>

    <div class="home-content">
      <header class="home-hero">
        <div>
          <span class="home-eyebrow"><span class="home-live-dot"></span> SSH workspace</span>
          <h2>准备连接到哪里？</h2>
          <p>在一个安静、专注的工作区里管理服务器、终端与文件传输。</p>
        </div>
        <div class="home-hero-actions">
          <Button variant="primary" @click="emit('quickConnect')">
            <Icon name="zap" :size="14" />
            快速连接
          </Button>
          <Button variant="ghost" @click="ui.showWorkspace(); sessions.openLocalTab()">
            <Icon name="monitor" :size="14" />
            本地终端
          </Button>
        </div>
      </header>

      <div class="home-grid">
        <section class="home-panel home-servers">
          <div class="home-panel-head">
            <div>
              <span class="home-kicker">SERVERS</span>
              <h3>常用服务器</h3>
            </div>
            <button class="home-link" type="button" @click="emit('manageServers')">
              管理全部 <Icon name="arrow-right" :size="13" />
            </button>
          </div>

          <div v-if="servers.loading && !servers.servers.length" class="home-server-list" aria-label="正在加载服务器">
            <div v-for="i in 3" :key="i" class="home-server-row skeleton-row"></div>
          </div>
          <div v-else-if="featuredServers.length" class="home-server-list">
            <button
              v-for="server in featuredServers"
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
              <span v-if="server.group" class="home-server-group">{{ server.group }}</span>
              <Icon name="chevron-right" :size="14" extra-class="home-row-arrow" />
            </button>
          </div>
          <div v-else class="home-empty">
            <span class="home-empty-icon"><Icon name="plus" :size="18" /></span>
            <div><strong>添加第一台服务器</strong><p>保存连接信息，下次一键进入终端。</p></div>
            <Button variant="ghost" size="sm" @click="emit('manageServers')">新建服务器</Button>
          </div>
        </section>

        <aside class="home-side">
          <section class="home-panel home-status">
            <span class="home-kicker">OVERVIEW</span>
            <h3>首页概览</h3>
            <div class="home-stats">
              <div><strong>{{ servers.servers.length }}</strong><span>服务器</span></div>
              <div><strong>{{ groupCount }}</strong><span>分组</span></div>
              <div><strong>{{ sessions.tabs.length }}</strong><span>会话</span></div>
            </div>
          </section>

          <section class="home-panel home-status">
            <h3>在线会话 · {{ onlineTabs.length }}</h3>
            <button v-for="tab in onlineTabs" :key="tab.clientId" class="home-server-row" @click="ui.showWorkspace(); sessions.activateTab(tab.clientId)">{{ tab.node?.name || '本机终端' }} <Icon name="arrow-right" :size="14" /></button>
            <p v-if="!onlineTabs.length">暂无在线会话</p>
          </section>
          <section class="home-panel home-status">
            <h3>最近连接</h3>
            <button v-for="server in recentServers" :key="server!.id" class="home-server-row" @click="connect(server!)">{{ server!.name }} <Icon name="arrow-right" :size="14" /></button>
            <p v-if="!recentServers.length">连接服务器后将在这里显示</p>
          </section>
          <button type="button" class="home-panel home-theme" @click="emit('openSettings')">
            <span class="home-theme-icon"><Icon name="palette" :size="17" /></span>
            <span><strong>让工作区更像你</strong><small>调整颜色、明暗与终端主题</small></span>
            <Icon name="arrow-right" :size="14" extra-class="home-row-arrow" />
          </button>
        </aside>
      </div>

      <footer class="home-shortcuts">
        <span><kbd>⌘ K</kbd> 命令面板</span>
        <span><kbd>⌘ T</kbd> 本地终端</span>
        <span><kbd>⌘ ,</kbd> 偏好设置</span>
      </footer>
    </div>
  </section>
</template>

<style scoped>
.workspace-home{position:relative;flex:1;isolation:isolate;min-width:0;min-height:0;overflow-x:hidden;overflow-y:auto;container-type:inline-size;background:transparent;color:var(--foreground)}
.workspace-home::before{content:"";position:absolute;inset:0;pointer-events:none;background:linear-gradient(135deg,var(--theme-glass-tint,transparent),transparent 42%);opacity:.7}
.home-orb{position:absolute;z-index:-1;border-radius:999px;filter:blur(72px);pointer-events:none;opacity:.28}.home-orb-a{width:420px;height:420px;right:-140px;top:-180px;background:var(--primary)}.home-orb-b{width:280px;height:280px;left:18%;bottom:-180px;background:var(--secondary)}
.home-content{width:min(1040px,calc(100% - 64px));height:auto;min-height:100%;margin:0 auto;padding:clamp(24px,4.5vh,56px) 0 22px;display:flex;flex-direction:column}
.home-hero{display:flex;align-items:flex-end;justify-content:space-between;gap:32px;margin-bottom:34px}.home-eyebrow,.home-kicker{display:flex;align-items:center;gap:7px;color:var(--muted-foreground);font:600 10px/1 var(--font-mono);letter-spacing:.14em;text-transform:uppercase}.home-live-dot{width:6px;height:6px;border-radius:50%;background:var(--success);box-shadow:0 0 0 4px color-mix(in srgb,var(--success) 14%,transparent)}.home-hero h2{margin:15px 0 7px;font-size:clamp(29px,3vw,42px);line-height:1.08;letter-spacing:-.045em;font-weight:600}.home-hero p{margin:0;color:var(--foreground-secondary);font-size:14px}.home-hero-actions{display:flex;gap:8px;flex-shrink:0}
.home-grid{display:grid;grid-template-columns:minmax(0,1.65fr) minmax(260px,.75fr);gap:14px}.home-side{display:grid;gap:14px;align-content:start}.home-panel{border:1px solid var(--border-subtle);border-radius:var(--radius-card);background:var(--theme-bg-surface,var(--surface-2));box-shadow:0 12px 36px rgb(0 0 0 / 5%),inset 0 1px 0 rgb(255 255 255 / 4%);backdrop-filter:blur(var(--theme-glass-blur,24px));-webkit-backdrop-filter:blur(var(--theme-glass-blur,24px))}.home-servers{padding:24px}.home-panel-head{display:flex;align-items:flex-start;justify-content:space-between;margin-bottom:18px}.home-panel h3{margin:8px 0 0;font-size:15px;letter-spacing:-.01em}.home-link{display:flex;align-items:center;gap:6px;color:var(--muted-foreground);font-size:12px}.home-link:hover{color:var(--foreground)}
.home-server-list{display:grid;gap:7px}.home-server-row{width:100%;min-height:58px;display:flex;align-items:center;gap:12px;padding:9px 11px;border:1px solid transparent;border-radius:var(--radius-card);text-align:left;transition:background var(--dur),border-color var(--dur),transform var(--dur)}.home-server-row:hover{background:var(--hover-strong);border-color:var(--border-subtle);transform:translateY(-1px)}.home-server-icon,.home-theme-icon,.home-empty-icon{width:34px;height:34px;border-radius:var(--radius-control);display:grid;place-items:center;flex-shrink:0;background:var(--primary-weak);color:var(--primary-hover);box-shadow:inset 0 0 0 1px var(--primary-border)}.home-server-copy{min-width:0;flex:1}.home-server-copy strong{display:block;font-size:13px;font-weight:550}.home-server-copy small{display:block;margin-top:2px;color:var(--muted-foreground);font:11px/1.3 var(--font-mono);overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.home-server-group{max-width:92px;padding:3px 7px;border-radius:var(--radius-pill);background:var(--hover);color:var(--muted-foreground);font-size:10px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}.home-row-arrow{color:var(--muted-foreground)}.skeleton-row{background:linear-gradient(100deg,var(--hover),var(--hover-strong),var(--hover));background-size:200% 100%;animation:shimmer 1.4s infinite}.home-empty{min-height:210px;display:flex;flex-direction:column;align-items:center;justify-content:center;text-align:center;gap:12px;color:var(--muted-foreground)}.home-empty strong{display:block;color:var(--foreground);font-size:13px}.home-empty p{margin:4px 0 0;font-size:12px}
.home-status{padding:24px}.home-stats{display:grid;grid-template-columns:repeat(3,1fr);gap:7px;margin-top:24px}.home-stats div{padding:13px 8px;border-radius:var(--radius-card);background:var(--hover);text-align:center}.home-stats strong{display:block;font:600 20px/1 var(--font-mono);color:var(--foreground)}.home-stats span{display:block;margin-top:7px;color:var(--muted-foreground);font-size:10px}.home-theme{width:100%;display:grid;grid-template-columns:auto 1fr auto;align-items:center;gap:12px;padding:18px;text-align:left;transition:transform var(--dur),border-color var(--dur)}.home-theme:hover{transform:translateY(-1px);border-color:var(--primary-border)}.home-theme strong,.home-theme small{display:block}.home-theme strong{font-size:12.5px}.home-theme small{margin-top:3px;color:var(--muted-foreground);font-size:11px}
.home-shortcuts{display:flex;gap:22px;flex-shrink:0;padding-top:clamp(14px,2.5vh,28px);color:var(--muted-foreground);font-size:11px}.home-shortcuts kbd{margin-right:5px;padding:3px 6px;border:1px solid var(--border-subtle);border-radius:6px;background:var(--hover);font:10px var(--font-mono);color:var(--foreground-secondary)}
/* 内容自然撑高，短窗口和放大界面下由首页统一滚动。 */
.home-hero{margin-bottom:clamp(18px,3vh,30px);flex-shrink:0}
.home-grid{flex:1 0 auto;min-height:0}
.home-servers{min-height:0;display:flex;flex-direction:column}
.home-server-list{min-height:0}
@keyframes shimmer{to{background-position:-200% 0}}
@media(max-width:820px){.home-content{width:min(100% - 32px,680px);padding-top:32px}.home-hero{align-items:flex-start;flex-direction:column}.home-grid{grid-template-columns:1fr}.home-shortcuts{display:none}}
</style>

<style scoped>
@container(max-width:760px){.home-content{width:calc(100% - 32px);padding-top:24px}.home-hero{align-items:flex-start;flex-direction:column;gap:18px}.home-grid{grid-template-columns:minmax(0,1fr)}.home-shortcuts{flex-wrap:wrap;gap:12px}}
</style>
