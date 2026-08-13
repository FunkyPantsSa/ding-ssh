<script lang="ts" setup>
import {computed, onBeforeUnmount, onMounted, reactive, ref, watch} from 'vue'
import {onTunnelStatus, sshService} from '../services/ssh'
import {useServersStore} from '../stores/servers'
import type {TunnelInfo, TunnelStatusEvent} from '../types'

const servers = useServersStore()
const tunnels = ref<TunnelInfo[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const form = reactive({
  serverId: '',
  name: '',
  mode: 'local' as 'local' | 'remote' | 'dynamic',
  localPort: 13306,
  remoteHost: '127.0.0.1',
  remotePort: 3306,
})
const disposers: Array<() => void> = []

const selectedServer = computed(() => servers.servers.find((s) => s.id === form.serverId))

const hint = computed(() => {
  if (form.mode === 'dynamic') {
    return `SOCKS5 代理：127.0.0.1:${form.localPort || '?'}`
  }
  if (form.mode === 'remote') {
    return `远程转发：远端 ${form.remoteHost || '127.0.0.1'}:${form.remotePort || '?'} → 本机 127.0.0.1:${form.localPort || '?'}`
  }
  return `本地转发：127.0.0.1:${form.localPort || '?'} → ${form.remoteHost || '?'}:${form.remotePort || '?'}`
})

watch(
  () => form.serverId,
  (id, old) => {
    const s = servers.servers.find((x) => x.id === id)
    if (!s) return
    if (!form.name || form.name === servers.servers.find((x) => x.id === old)?.name) {
      form.name = s.name
    }
  },
)

watch(
  () => form.mode,
  (mode) => {
    if (mode === 'dynamic' && form.localPort === 13306) form.localPort = 1080
    if (mode === 'local' && form.localPort === 1080) form.localPort = 13306
  },
)

async function refresh() {
  loading.value = true
  try {
    tunnels.value = await sshService.listTunnels()
  } catch (e) {
    error.value = String(e)
  } finally {
    loading.value = false
  }
}

async function create() {
  error.value = ''
  if (!form.serverId) {
    error.value = '请选择服务器'
    return
  }
  const node = selectedServer.value
  if (!node) return
  const localPort = Number(form.localPort)
  const remotePort = Number(form.remotePort)
  if (!Number.isInteger(localPort) || localPort < 1 || localPort > 65535) {
    error.value = '本地端口无效（1-65535）'
    return
  }
  if (form.mode !== 'dynamic') {
    if (!Number.isInteger(remotePort) || remotePort < 1 || remotePort > 65535) {
      error.value = '远程端口无效（1-65535）'
      return
    }
  }
  busy.value = true
  try {
    await sshService.startTunnel(
      node,
      form.name.trim() || node.name,
      form.mode,
      localPort,
      form.remoteHost.trim() || '127.0.0.1',
      form.mode === 'dynamic' ? 0 : remotePort,
    )
    await refresh()
  } catch (e) {
    error.value = String(e)
  } finally {
    busy.value = false
  }
}

async function stop(id: string) {
  error.value = ''
  try {
    await sshService.stopTunnel(id)
    await refresh()
  } catch (e) {
    error.value = String(e)
  }
}

async function restart(id: string) {
  error.value = ''
  try {
    await sshService.restartTunnel(id)
    await refresh()
  } catch (e) {
    error.value = String(e)
  }
}

async function remove(id: string) {
  error.value = ''
  try {
    await sshService.removeTunnel(id)
    await refresh()
  } catch (e) {
    error.value = String(e)
  }
}

function onStatus(evt: TunnelStatusEvent) {
  const t = tunnels.value.find((x) => x.id === evt.id)
  if (!t) return
  t.status = evt.status
  if (evt.message) t.message = evt.message
}

const filteredTunnels = computed(() => tunnels.value.filter((t) => t.mode === form.mode))

function tunnelDesc(t: TunnelInfo): string {
  if (t.mode === 'dynamic') return `socks5://127.0.0.1:${t.localPort}\nvia ${t.serverName}`
  if (t.mode === 'remote') {
    return `remote:${t.remotePort} → ${t.remoteHost}:${t.localPort}\nvia ${t.serverName}`
  }
  return `127.0.0.1:${t.localPort} → ${t.remoteHost}:${t.remotePort}\nvia ${t.serverName}`
}

onMounted(async () => {
  if (!servers.servers.length) await servers.load()
  if (servers.servers.length && !form.serverId) form.serverId = servers.servers[0].id
  disposers.push(onTunnelStatus(onStatus))
  await refresh()
})

onBeforeUnmount(() => {
  disposers.forEach((d) => d())
})
</script>

<template>
  <div class="h-full flex flex-col min-h-0">
    <div class="page-pad">
      <div class="page-hero grid grid-cols-[1fr_auto] gap-6 items-end mb-6">
        <div>
          <h2>SSH 隧道</h2>
          <p>本地转发、远程转发与动态 SOCKS5。把内网服务安全映射到本机，或让远端走本地出口。</p>
        </div>
        <svg class="w-[200px] h-[100px]" viewBox="0 0 200 100" fill="none" aria-hidden="true">
          <path d="M20 50h40l10-20 10 40 10-20h40" stroke="#3ec4b4" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round"/>
          <circle cx="30" cy="50" r="8" fill="#121820" stroke="#e0925e" stroke-width="1.4"/>
          <circle cx="170" cy="50" r="8" fill="#121820" stroke="#3ec4b4" stroke-width="1.4"/>
        </svg>
      </div>

      <div class="inline-flex gap-1 mb-5 p-1 rounded-[10px]" style="background:rgba(0,0,0,0.28);box-shadow:inset 0 0 0 1px rgba(255,255,255,0.05)">
        <button
          v-for="m in (['local', 'remote', 'dynamic'] as const)"
          :key="m"
          class="h-[34px] px-4 rounded-[6px] text-xs font-medium"
          :class="form.mode === m ? 'text-[var(--mist-100)] bg-white/8' : 'text-mist'"
          @click="form.mode = m"
        >
          {{ m === 'local' ? '本地转发 -L' : m === 'remote' ? '远程转发 -R' : '动态 SOCKS5 -D' }}
        </button>
      </div>

      <div v-if="filteredTunnels.length" class="grid gap-4 mb-6" style="grid-template-columns: repeat(auto-fill, minmax(300px, 1fr))">
        <div v-for="t in filteredTunnels" :key="t.id" class="neo neo-hover p-5 flex flex-col gap-3">
          <div class="flex items-start justify-between gap-3">
            <div>
              <h3 class="text-sm font-semibold text-[var(--mist-100)]">{{ t.name }}</h3>
              <div class="font-mono text-[11px] text-mist leading-relaxed whitespace-pre-line mt-1">{{ tunnelDesc(t) }}</div>
            </div>
            <span class="badge" :class="t.status === 'running' ? 'live' : t.status === 'error' ? 'err' : 'stop'">
              {{ t.status === 'running' ? 'LIVE' : t.status === 'error' ? 'ERROR' : 'STOPPED' }}
            </span>
          </div>
          <p v-if="t.status === 'error' && t.message" class="text-[11px] text-danger break-all">{{ t.message }}</p>
          <div class="flex gap-1.5">
            <button v-if="t.status === 'running'" class="btn btn-ghost btn-sm" @click="stop(t.id)">停止</button>
            <button v-else class="btn btn-primary btn-sm" @click="restart(t.id)">启动</button>
            <button v-if="t.status === 'running'" class="btn btn-ghost btn-sm" @click="restart(t.id)">重启</button>
            <button class="btn btn-ghost btn-sm" @click="remove(t.id)">删除</button>
          </div>
        </div>
      </div>
      <div v-else-if="!loading" class="empty py-10">
        <div class="empty-inner">
          <h3>暂无此类隧道</h3>
          <p>使用下方表单创建一条转发规则，或切换到其他类型查看。</p>
        </div>
      </div>

      <div class="neo p-5 grid gap-4 items-end" style="grid-template-columns: repeat(4, 1fr)">
        <div class="field">
          <label>名称</label>
          <input v-model="form.name" class="input" placeholder="例如 Postgres 本地入口" />
        </div>
        <div class="field">
          <label>跳板服务器</label>
          <select v-model="form.serverId" class="select">
            <option v-for="s in servers.servers" :key="s.id" :value="s.id">{{ s.name }}</option>
          </select>
        </div>
        <div class="field">
          <label>{{ form.mode === 'remote' ? '本机目标端口' : '本地绑定' }}</label>
          <input v-model.number="form.localPort" type="number" min="1" max="65535" class="input font-mono" />
        </div>
        <div v-if="form.mode !== 'dynamic'" class="field">
          <label>{{ form.mode === 'remote' ? '远程监听' : '远端目标' }}</label>
          <input v-model="form.remoteHost" class="input font-mono" placeholder="127.0.0.1" />
        </div>
        <div v-if="form.mode !== 'dynamic'" class="field">
          <label>{{ form.mode === 'remote' ? '远程端口' : '远端端口' }}</label>
          <input v-model.number="form.remotePort" type="number" min="1" max="65535" class="input font-mono" />
        </div>
        <div class="col-span-full flex items-center justify-between gap-4 pt-2">
          <p v-if="error" class="text-xs text-danger break-all">{{ error }}</p>
          <p v-else class="text-[11px] text-mist">{{ hint }}</p>
          <div class="flex gap-2 ml-auto">
            <button class="btn btn-ghost" type="button" @click="refresh">刷新</button>
            <button class="btn btn-primary" :disabled="busy" @click="create">
              {{ busy ? '创建中…' : '创建隧道' }}
            </button>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>
