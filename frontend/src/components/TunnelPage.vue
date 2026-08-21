<script lang="ts" setup>
import {computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch} from 'vue'
import {onTunnelStatus, sshService} from '../services/ssh'
import {useServersStore} from '../stores/servers'
import {ClipboardSetText} from '../../wailsjs/runtime/runtime'
import type {TunnelInfo, TunnelStatusEvent} from '../types'
import Icon from './Icon.vue'

type TunnelMode = 'local' | 'remote' | 'dynamic'

const servers = useServersStore()
const tunnels = ref<TunnelInfo[]>([])
const loading = ref(false)
const busy = ref(false)
const error = ref('')
const showDialog = ref(false)
const editing = ref<TunnelInfo | null>(null)
const copiedId = ref('')
let copiedTimer: number | undefined
/** 载入已有隧道时暂停「按模式/服务器自动填默认值」的联动，避免覆盖原有配置。 */
let syncingForm = false
const form = reactive({
  serverId: '',
  name: '',
  mode: 'local' as TunnelMode,
  localPort: 13306,
  remoteHost: '127.0.0.1',
  remotePort: 3306,
})
const disposers: Array<() => void> = []

const selectedServer = computed(() => servers.servers.find((s) => s.id === form.serverId))

const modeDetails = {
  local: {
    title: '本地转发 -L',
    summary: '从本机访问远端网络中的服务',
    description: '在本机开放一个端口，经 SSH 服务器连接只有远端网络才能访问的目标。',
    example: '例如：数据库 10.0.0.8:3306 仅内网可见。创建后访问本机 127.0.0.1:13306，即可连接该数据库。',
  },
  remote: {
    title: '远程转发 -R',
    summary: '让远端访问本机上的服务',
    description: '在 SSH 服务器上开放一个端口，收到的连接会通过隧道转发到本机服务。',
    example: '例如：本机在 3000 端口运行开发站点。远端监听 127.0.0.1:13000 后，服务器访问该地址即可打开本机站点。',
  },
  dynamic: {
    title: '动态 SOCKS5 -D',
    summary: '让应用通过 SSH 服务器访问网络',
    description: '在本机创建 SOCKS5 代理，应用发出的不同目标请求都会经 SSH 服务器转发。',
    example: '例如：在浏览器中将 SOCKS5 代理设为 127.0.0.1:1080，浏览器流量就会从 SSH 服务器出口访问网络。',
  },
} as const

const currentModeDetail = computed(() => modeDetails[form.mode])

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
    if (syncingForm) return
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
    if (syncingForm) return
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

async function submit() {
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
  const name = form.name.trim() || node.name
  const remoteHost = form.remoteHost.trim() || '127.0.0.1'
  const targetPort = form.mode === 'dynamic' ? 0 : remotePort
  busy.value = true
  try {
    if (editing.value) {
      await sshService.updateTunnel(editing.value.id, node, name, form.mode, localPort, remoteHost, targetPort)
    } else {
      await sshService.startTunnel(node, name, form.mode, localPort, remoteHost, targetPort)
    }
    await refresh()
    showDialog.value = false
    editing.value = null
  } catch (e) {
    error.value = String(e)
    await refresh()
  } finally {
    busy.value = false
  }
}

/** 用指定隧道的配置填充表单；传 null 表示按当前模式重置为新建默认值。 */
function fillForm(t: TunnelInfo | null) {
  syncingForm = true
  if (t) {
    form.serverId = servers.servers.some((s) => s.id === t.serverId)
      ? t.serverId
      : servers.servers[0]?.id ?? ''
    form.name = t.name
    form.mode = t.mode as TunnelMode
    form.localPort = t.localPort
    form.remoteHost = t.remoteHost || '127.0.0.1'
    form.remotePort = t.remotePort || 3306
  } else {
    if (!form.serverId && servers.servers.length) form.serverId = servers.servers[0].id
    form.name = selectedServer.value?.name ?? ''
    form.localPort = form.mode === 'dynamic' ? 1080 : 13306
    form.remoteHost = '127.0.0.1'
    form.remotePort = 3306
  }
  void nextTick(() => {
    syncingForm = false
  })
}

function openCreate() {
  error.value = ''
  editing.value = null
  fillForm(null)
  showDialog.value = true
}

function openEdit(t: TunnelInfo) {
  error.value = ''
  editing.value = t
  fillForm(t)
  showDialog.value = true
}

function closeDialog() {
  if (busy.value) return
  showDialog.value = false
  editing.value = null
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

/** 使用者真正需要填到客户端里的地址。 */
function tunnelAddress(t: TunnelInfo): string {
  if (t.mode === 'dynamic') return `socks5://127.0.0.1:${t.localPort}`
  if (t.mode === 'remote') return `${t.remoteHost || '127.0.0.1'}:${t.remotePort}`
  return `127.0.0.1:${t.localPort}`
}

function addressLabel(t: TunnelInfo): string {
  if (t.mode === 'dynamic') return '代理地址'
  if (t.mode === 'remote') return '远端访问地址'
  return '本机访问地址'
}

function tunnelPath(t: TunnelInfo): string {
  if (t.mode === 'dynamic') return `经 ${t.serverName} 出口访问网络`
  if (t.mode === 'remote') {
    return `${t.remoteHost}:${t.remotePort}（${t.serverName}）→ 本机 127.0.0.1:${t.localPort}`
  }
  return `本机 127.0.0.1:${t.localPort} → ${t.remoteHost}:${t.remotePort}（经 ${t.serverName}）`
}

async function copyAddress(t: TunnelInfo) {
  try {
    await ClipboardSetText(tunnelAddress(t))
    copiedId.value = t.id
    window.clearTimeout(copiedTimer)
    copiedTimer = window.setTimeout(() => {
      copiedId.value = ''
    }, 1600)
  } catch (e) {
    error.value = String(e)
  }
}

onMounted(async () => {
  if (!servers.servers.length) await servers.load()
  if (servers.servers.length && !form.serverId) form.serverId = servers.servers[0].id
  disposers.push(onTunnelStatus(onStatus))
  await refresh()
})

onBeforeUnmount(() => {
  disposers.forEach((d) => d())
  window.clearTimeout(copiedTimer)
})
</script>

<template>
  <div class="h-full flex flex-col min-h-0">
    <div class="page-pad">
      <div class="page-hero flex items-end justify-between gap-6 mb-6">
        <div>
          <h2>SSH 隧道</h2>
          <p>通过 SSH 安全转发端口，在本机、远端服务器和目标服务之间建立访问通道。</p>
        </div>
        <div class="flex gap-2 shrink-0">
          <button class="btn btn-ghost" type="button" @click="refresh">
            <Icon name="refresh" :size="14" />
            刷新
          </button>
          <button class="btn btn-primary" type="button" :disabled="!servers.servers.length" @click="openCreate">
            <Icon name="plus" :size="15" />
            新建隧道
          </button>
        </div>
      </div>

      <div class="grid grid-cols-3 gap-3 mb-6">
        <button
            v-for="m in (['local', 'remote', 'dynamic'] as const)"
            :key="m"
            class="relative neo-flat p-4 text-left transition-all"
            :class="form.mode === m
              ? 'ring-1 ring-[var(--signal-400)] bg-[rgba(42,168,154,0.10)] shadow-[0_0_20px_rgba(62,196,180,0.20)]'
              : 'hover:bg-white/3'"
            @click="form.mode = m"
          >
          <span
            v-if="form.mode === m"
            class="absolute top-2.5 right-2.5 w-4 h-4 rounded-full bg-[var(--signal-400)] grid place-items-center"
          >
            <Icon name="check" :size="10" extra-class="text-[#071210] font-bold" />
          </span>
          <div class="text-[13px] font-semibold" :class="form.mode === m ? 'text-[var(--signal-200)]' : 'text-[var(--mist-100)]'">
            {{ modeDetails[m].title }}
          </div>
          <div class="text-[12px] text-mist mt-1">{{ modeDetails[m].summary }}</div>
        </button>
      </div>

      <div class="neo-flat px-4 py-3 mb-5">
        <p class="text-[13px] text-[var(--mist-200)]">{{ currentModeDetail.description }}</p>
        <p class="text-[12px] text-mist mt-1.5 leading-relaxed">{{ currentModeDetail.example }}</p>
      </div>

      <div v-if="filteredTunnels.length" class="grid gap-4 mb-6" style="grid-template-columns: repeat(auto-fill, minmax(300px, 1fr))">
        <div v-for="t in filteredTunnels" :key="t.id" class="neo neo-hover p-5 flex flex-col gap-3">
          <div class="flex items-start justify-between gap-3">
            <h3 class="text-sm font-semibold text-[var(--mist-100)] min-w-0 truncate">{{ t.name }}</h3>
            <span class="badge" :class="t.status === 'running' ? 'live' : t.status === 'error' ? 'err' : 'stop'">
              {{ t.status === 'running' ? 'LIVE' : t.status === 'error' ? 'ERROR' : 'STOPPED' }}
            </span>
          </div>

          <button
            class="neo-flat w-full flex items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-white/5"
            :title="`复制 ${tunnelAddress(t)}`"
            @click="copyAddress(t)"
          >
            <span class="min-w-0 flex-1">
              <span class="block text-[11px] text-mist">{{ addressLabel(t) }}</span>
              <span class="block font-mono text-[13px] text-[var(--mist-100)] truncate">{{ tunnelAddress(t) }}</span>
            </span>
            <Icon :name="copiedId === t.id ? 'check' : 'copy'" :size="15" />
            <span class="text-[11px] shrink-0" :class="copiedId === t.id ? 'text-[var(--signal-300)]' : 'text-mist'">
              {{ copiedId === t.id ? '已复制' : '复制' }}
            </span>
          </button>

          <div class="text-[12px] text-mist leading-relaxed">{{ tunnelPath(t) }}</div>
          <p v-if="t.status === 'error' && t.message" class="text-[12px] text-danger break-all">{{ t.message }}</p>

          <div class="flex flex-wrap gap-1.5">
            <button v-if="t.status === 'running'" class="btn btn-ghost btn-sm" @click="stop(t.id)">停止</button>
            <button v-else class="btn btn-primary btn-sm" @click="restart(t.id)">启动</button>
            <button v-if="t.status === 'running'" class="btn btn-ghost btn-sm" @click="restart(t.id)">重启</button>
            <button class="btn btn-ghost btn-sm" @click="openEdit(t)">编辑</button>
            <button class="btn btn-ghost btn-sm" @click="remove(t.id)">删除</button>
          </div>
        </div>
      </div>
      <div v-else-if="!loading" class="empty py-10">
        <div class="empty-inner">
          <h3>暂无此类隧道</h3>
          <p>{{ servers.servers.length ? '点击右上角「新建隧道」创建一条转发规则，或切换类型查看。' : '请先新建 SSH 服务器，再创建隧道。' }}</p>
        </div>
      </div>
    </div>

    <Teleport to="body">
      <div v-if="showDialog" class="modal-root" @click.self="closeDialog">
        <div class="modal neo wide" style="width:min(600px,92vw);max-height:85vh;display:flex;flex-direction:column">
          <div class="flex items-start justify-between gap-4 px-6 pt-6 pb-4">
            <div>
              <h3>{{ editing ? '编辑隧道' : '新建隧道' }}</h3>
              <p class="mdesc !mb-0">
                {{ editing ? '保存后按新配置生效；运行中的隧道会自动重启。' : '选择转发方式，并配置监听端口与目标地址。' }}
              </p>
            </div>
            <button class="btn-icon" title="关闭" :disabled="busy" @click="closeDialog">
              <Icon name="close" :size="14" />
            </button>
          </div>

          <div class="flex-1 overflow-y-auto px-6 pb-2 flex flex-col gap-4">
            <div>
              <span class="text-slate-400">隧道类型</span>
              <div class="seg">
                <button :class="form.mode === 'local' ? 'active' : ''" @click="form.mode = 'local'">本地 -L</button>
                <button :class="form.mode === 'remote' ? 'active' : ''" @click="form.mode = 'remote'">远程 -R</button>
                <button :class="form.mode === 'dynamic' ? 'active' : ''" @click="form.mode = 'dynamic'">SOCKS5 -D</button>
              </div>
              <div class="neo-flat px-3 py-2.5 mt-2">
                <p class="text-[12px] text-[var(--mist-200)]">{{ currentModeDetail.description }}</p>
                <p class="text-[12px] text-mist mt-1">{{ currentModeDetail.example }}</p>
              </div>
            </div>

            <div class="grid grid-cols-2 gap-3">
              <div class="field">
                <label>名称</label>
                <input v-model="form.name" class="input" placeholder="例如：MySQL 本地入口" />
              </div>
              <div class="field">
                <label>SSH 服务器</label>
                <select v-model="form.serverId" class="select">
                  <option v-for="s in servers.servers" :key="s.id" :value="s.id">{{ s.name }}</option>
                </select>
              </div>
            </div>

            <div class="grid grid-cols-2 gap-3">
              <div class="field">
                <label>{{ form.mode === 'remote' ? '本机服务端口' : form.mode === 'dynamic' ? '本地代理端口' : '本地监听端口' }}</label>
                <input v-model.number="form.localPort" type="number" min="1" max="65535" class="input font-mono" />
              </div>
              <div v-if="form.mode !== 'dynamic'" class="field">
                <label>{{ form.mode === 'remote' ? '远端监听地址' : '目标主机' }}</label>
                <input v-model="form.remoteHost" class="input font-mono" placeholder="127.0.0.1" />
              </div>
            </div>

            <div v-if="form.mode !== 'dynamic'" class="field">
              <label>{{ form.mode === 'remote' ? '远端监听端口' : '目标端口' }}</label>
              <input v-model.number="form.remotePort" type="number" min="1" max="65535" class="input font-mono" />
            </div>

            <p v-if="error" class="text-xs text-danger break-all">{{ error }}</p>
            <p v-else class="text-[12px] text-mist">{{ hint }}</p>
          </div>

          <div class="flex justify-end gap-2 px-6 py-5">
            <button class="btn btn-ghost" type="button" :disabled="busy" @click="closeDialog">取消</button>
            <button class="btn btn-primary" :disabled="busy" @click="submit">
              {{ busy ? (editing ? '保存中…' : '创建中…') : editing ? '保存修改' : '创建隧道' }}
            </button>
          </div>
        </div>
      </div>
    </Teleport>
  </div>
</template>
